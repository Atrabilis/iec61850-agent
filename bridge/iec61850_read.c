/*
 * iec61850-read: read-only snapshot of an IEC 61850 server, printed as one JSON document on stdout.
 *
 * Copyright (C) 2026 The iec61850-agent authors. Licensed under the GNU General Public License v3 (see LICENSE).
 * Links libiec61850 (MZ Automation GmbH, GPLv3), pinned in third_party/libiec61850.
 *
 * Only MMS read services are used: GetNameList (domains and variables), GetVariableAccessAttributes
 * (type definitions, to name structure members) and Read. The CO functional constraint (controls) is
 * never read or written, whatever the -f list says. One association per run; one read per
 * "logical node + FC" item.
 *
 * Output:
 *   {"meta": {"host", "port", "taken_at", "ied", "fc", "nameplate", "reads", "failures"},
 *    "domains": {"<LD instance>": {"<LN>$<FC>": <value tree>}}}
 * Domain keys drop the IED name prefix (kept in meta.ied), so keys stay stable if the IED is renamed.
 *
 * Exit status: 0 ok; 1 usage or connection error; 3 some reads failed (output is still complete JSON).
 */
#include <inttypes.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "mms_client_connection.h"

static void usage(const char *prog)
{
    fprintf(stderr,
            "usage: %s -h <host> [-p <port>] [-t <timeout_ms>] [-d <delay_ms>] [-f <FC,FC,...>]\n"
            "  -p  MMS port (default 102)\n"
            "  -t  request timeout in ms (default 10000)\n"
            "  -d  pause between reads in ms, to go easy on the device (default 20)\n"
            "  -f  functional constraints to read, e.g. ST,MX (default: all but CO; CO is never read)\n",
            prog);
}

static void json_string(FILE *out, const char *s)
{
    fputc('"', out);
    for (; s && *s; s++) {
        unsigned char c = (unsigned char)*s;
        if (c == '"' || c == '\\') {
            fputc('\\', out);
            fputc(c, out);
        } else if (c < 0x20) {
            fprintf(out, "\\u%04x", c);
        } else {
            fputc(c, out);
        }
    }
    fputc('"', out);
}

/* True if fc (e.g. "MX") appears in the comma-separated list. */
static int fc_selected(const char *list, const char *fc)
{
    size_t n = strlen(fc);
    for (const char *p = list; *p;) {
        const char *end = strchr(p, ',');
        size_t len = end ? (size_t)(end - p) : strlen(p);
        if (len == n && strncmp(p, fc, n) == 0)
            return 1;
        if (!end)
            break;
        p = end + 1;
    }
    return 0;
}

/* IEC 61850 domain name = IED name + LD instance ("MyIED" + "LD0"). With several domains the IED
 * name is their common prefix; with a single domain it cannot be told apart, so nothing is stripped. */
static size_t ied_prefix_len(LinkedList domains)
{
    const char *first = NULL;
    size_t len = 0;
    int count = 0;
    for (LinkedList d = LinkedList_getNext(domains); d; d = LinkedList_getNext(d)) {
        const char *name = (const char *)LinkedList_getData(d);
        count++;
        if (!first) {
            first = name;
            len = strlen(name);
            continue;
        }
        size_t i = 0;
        while (i < len && name[i] == first[i])
            i++;
        len = i;
    }
    return count > 1 ? len : 0;
}

/* Emit one MMS value, using its type specification for member names. */
static void emit_value(FILE *out, MmsValue *v, MmsVariableSpecification *spec)
{
    char buf[512];
    if (v == NULL) {
        fputs("null", out);
        return;
    }
    switch (MmsValue_getType(v)) {
    case MMS_STRUCTURE: {
        int n = MmsVariableSpecification_getSize(spec);
        fputc('{', out);
        for (int i = 0; i < n; i++) {
            MmsVariableSpecification *child = MmsVariableSpecification_getChildSpecificationByIndex(spec, i);
            if (i)
                fputc(',', out);
            json_string(out, MmsVariableSpecification_getName(child));
            fputc(':', out);
            emit_value(out, MmsValue_getElement(v, i), child);
        }
        fputc('}', out);
        break;
    }
    case MMS_ARRAY: {
        int n = MmsValue_getArraySize(v);
        MmsVariableSpecification *el = MmsVariableSpecification_getArrayElementSpecification(spec);
        fputc('[', out);
        for (int i = 0; i < n; i++) {
            if (i)
                fputc(',', out);
            emit_value(out, MmsValue_getElement(v, i), el);
        }
        fputc(']', out);
        break;
    }
    case MMS_BOOLEAN:
        fputs(MmsValue_getBoolean(v) ? "true" : "false", out);
        break;
    case MMS_INTEGER:
        fprintf(out, "%" PRId64, MmsValue_toInt64(v));
        break;
    case MMS_UNSIGNED:
        fprintf(out, "%" PRIu32, MmsValue_toUint32(v));
        break;
    case MMS_FLOAT: {
        double d = MmsValue_toDouble(v);
        if (isfinite(d))
            fprintf(out, "%.9g", d);
        else
            fputs("null", out); /* JSON has no NaN/Inf */
        break;
    }
    case MMS_UTC_TIME: {
        uint64_t ms = MmsValue_getUtcTimeInMs(v);
        time_t secs = (time_t)(ms / 1000);
        struct tm tmv;
        gmtime_r(&secs, &tmv);
        strftime(buf, sizeof buf, "%Y-%m-%dT%H:%M:%S", &tmv);
        fprintf(out, "\"%s.%03" PRIu64 "Z\"", buf, ms % 1000);
        break;
    }
    default:
        /* Bit strings (quality, double-point positions), strings, octet strings, binary time. */
        MmsValue_printToBuffer(v, buf, sizeof buf);
        json_string(out, buf);
    }
}

/* Read one item and print it as `"key":value`. Returns 0 on success. */
static int read_item(MmsConnection con, const char *domain, const char *item, const char *key)
{
    MmsError err = MMS_ERROR_NONE;
    MmsVariableSpecification *spec = MmsConnection_getVariableAccessAttributes(con, &err, domain, item);
    MmsValue *val = spec ? MmsConnection_readVariable(con, &err, domain, item) : NULL;
    json_string(stdout, key);
    putchar(':');
    int ok = spec && val;
    if (ok)
        emit_value(stdout, val, spec);
    else
        printf("{\"_error\":%d}", (int)err);
    if (val)
        MmsValue_delete(val);
    if (spec)
        MmsVariableSpecification_destroy(spec);
    return ok ? 0 : -1;
}

int main(int argc, char **argv)
{
    const char *host = NULL;
    const char *fc_list = NULL;
    int port = 102, timeout_ms = 10000, delay_ms = 20, opt;

    while ((opt = getopt(argc, argv, "h:p:t:d:f:")) != -1) {
        switch (opt) {
        case 'h': host = optarg; break;
        case 'p': port = atoi(optarg); break;
        case 't': timeout_ms = atoi(optarg); break;
        case 'd': delay_ms = atoi(optarg); break;
        case 'f': fc_list = optarg; break;
        default: usage(argv[0]); return 1;
        }
    }
    if (!host || port <= 0 || timeout_ms <= 0 || delay_ms < 0) {
        usage(argv[0]);
        return 1;
    }
    struct timespec pause = {delay_ms / 1000, (delay_ms % 1000) * 1000000L};

    MmsError err = MMS_ERROR_NONE;
    MmsConnection con = MmsConnection_create();
    MmsConnection_setRequestTimeout(con, (uint32_t)timeout_ms);
    if (!MmsConnection_connect(con, &err, host, port)) {
        fprintf(stderr, "iec61850-read: connect to %s:%d failed (MMS error %d)\n", host, port, (int)err);
        MmsConnection_destroy(con);
        return 1;
    }

    time_t now = time(NULL);
    char stamp[32];
    strftime(stamp, sizeof stamp, "%Y-%m-%dT%H:%M:%SZ", gmtime(&now));

    LinkedList domains = MmsConnection_getDomainNames(con, &err);
    if (domains == NULL) {
        fprintf(stderr, "iec61850-read: could not list domains (MMS error %d)\n", (int)err);
        MmsConnection_destroy(con);
        return 1;
    }
    size_t prefix = ied_prefix_len(domains);
    char ied[128] = "";
    if (LinkedList_getNext(domains))
        snprintf(ied, sizeof ied, "%.*s", (int)prefix, (const char *)LinkedList_getData(LinkedList_getNext(domains)));

    int reads = 0, failures = 0;
    printf("{\"domains\":{");
    int firstDomain = 1;
    for (LinkedList d = LinkedList_getNext(domains); d; d = LinkedList_getNext(d)) {
        const char *domain = (const char *)LinkedList_getData(d);
        if (!firstDomain)
            putchar(',');
        firstDomain = 0;
        json_string(stdout, domain + prefix);
        printf(":{");

        LinkedList vars = MmsConnection_getDomainVariableNames(con, &err, domain);
        int firstVar = 1;
        for (LinkedList it = vars ? LinkedList_getNext(vars) : NULL; it; it = LinkedList_getNext(it)) {
            const char *item = (const char *)LinkedList_getData(it);
            /* Only top-level "LN$FC" items: one read covers a whole logical node for that FC. */
            const char *sep = strchr(item, '$');
            if (sep == NULL || strchr(sep + 1, '$') != NULL)
                continue;
            if (strcmp(sep + 1, "CO") == 0)
                continue; /* controls: never touched */
            if (fc_list && !fc_selected(fc_list, sep + 1))
                continue;
            if (!firstVar)
                putchar(',');
            firstVar = 0;
            reads++;
            if (read_item(con, domain, item, item) != 0)
                failures++;
            nanosleep(&pause, NULL);
        }
        if (vars)
            LinkedList_destroy(vars);
        putchar('}');
    }
    printf("},\"meta\":{\"host\":");
    json_string(stdout, host);
    printf(",\"port\":%d,\"taken_at\":\"%s\",\"ied\":", port, stamp);
    json_string(stdout, ied);
    printf(",\"fc\":");
    json_string(stdout, fc_list ? fc_list : "all-but-CO");
    /* Nameplate of the physical device (firmware, serial, model), to interpret the data later. */
    for (LinkedList d = LinkedList_getNext(domains); d; d = LinkedList_getNext(d)) {
        const char *domain = (const char *)LinkedList_getData(d);
        MmsVariableSpecification *spec = MmsConnection_getVariableAccessAttributes(con, &err, domain, "LPHD1$DC$PhyNam");
        if (!spec)
            continue;
        MmsValue *val = MmsConnection_readVariable(con, &err, domain, "LPHD1$DC$PhyNam");
        if (val) {
            printf(",\"nameplate\":");
            emit_value(stdout, val, spec);
            MmsValue_delete(val);
        }
        MmsVariableSpecification_destroy(spec);
        break;
    }
    printf(",\"reads\":%d,\"failures\":%d}}\n", reads, failures);

    LinkedList_destroy(domains);
    MmsConnection_destroy(con);
    if (failures)
        fprintf(stderr, "iec61850-read: %d of %d reads failed\n", failures, reads);
    return failures ? 3 : 0;
}
