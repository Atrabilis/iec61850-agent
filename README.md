# iec61850-agent

General-purpose IEC 61850 (MMS) telemetry collector. It reads protection and control devices (for example NOJA Power
reclosers) **read-only** and writes one JSONB row per data group to a PostgreSQL table (a "shadow" table: it keeps the
raw data, from which typed tables can be derived later). It is configured with one YAML file per device type, takes
credentials from environment variables and does one run per invocation, meant for a systemd timer or cron.

## How it is built

Two separate executables that only talk through command-line arguments and standard output:

| Executable | Language | What it does |
|---|---|---|
| `iec61850-read` | C, linked with [libiec61850](https://github.com/mz-automation/libiec61850) v1.5.3 (submodule in `third_party/`) | Opens **one** MMS association with the device, walks its model and prints JSON with the name and value of every data attribute, with its quality (`q`) and timestamp (`t`). It only uses read services (list domains and variables, get type definitions, read). **It never touches controls (`CO`)**, even if asked to |
| `iec61850-agent` | Go, no cgo | Reads the YAML, runs the reader for each device, splits the data into groups and writes the rows to PostgreSQL |

The reader takes every field name from the type definitions the device itself provides, so the output is
deterministic. Logical device keys drop the IED name (`LD0`, not `MyIEDLD0`) so they do not change if the IED is
renamed; the IED name is kept separately in `ied`.

## Configuration

See [`configs/example.yml`](configs/example.yml). The essentials:

```yaml
plant: example_plant
timestamp_resolution: 1m          # row timestamp = run start rounded down to this step
reader:
  timeout_ms: 10000               # per MMS request
  delay_ms: 20                    # pause between reads, to go easy on the device
  run_timeout: 2m                 # limit for reading one whole device
devices:
  - device:
      name: "noja_recloser"
      host: "192.0.2.10"
      port: 102
      groups:                      # each group is one row per run (series_key = name)
        - name: "mx"
          fc: ["MX"]               # measurements
        - name: "st"
          fc: ["ST"]               # states and events
storage:
  outputs:
    - name: "local_shadow"
      type: "postgres_shadow"
      postgres_shadow:
        host_env: "PG_HOST"       # names of environment variables, not values
        port_env: "PG_PORT"
        user_env: "PG_USER"
        password_env: "PG_PASSWORD"
        database_env: "PG_DATABASE"
        schema: "edge"
        table: "noja_recloser_shadow"
```

Each group filters by functional constraint (`fc`, required) and, optionally, by logical device (`logical_devices`)
and logical node (`logical_nodes`). `CO` is rejected: the agent only reads. For the target table see
[`sql/postgres_shadow.sql`](sql/postgres_shadow.sql); the agent does not create tables.

Most used functional constraints: `MX` measurements; `ST` states (positions, alarms, protection start and operate,
counters, `Mod`/`Beh`/`Loc`); `DC` descriptions and nameplates; `CF` data configuration; `SP` setpoints.

## Usage

```bash
iec61850-agent -configPath /etc/iec61850-agent/noja.yml            # one run
iec61850-agent -configPath noja.yml -interrogateOnly                 # read and print the rows, write nothing
iec61850-agent -configPath noja.yml -checkConfig                     # validate the YAML
iec61850-read -h 192.0.2.10 -f ST,MX                                  # the reader alone, to inspect a device
```

Other options: `-envPath` (dotenv file with the credentials; default `/etc/iec61850-agent/iec61850-agent.env`) and
`-version`. The agent looks for `iec61850-read` in `reader.path`, then next to its own binary, then in `PATH`. It exits
with status 1 if any device or output failed.

## Building

```bash
git submodule update --init
make test             # go vet + go test
make agent            # bin/iec61850-agent (Go, static, no cgo)
make reader           # bin/iec61850-read with the system compiler (for development)
make reader-static    # bin/iec61850-read-<arch>, static with musl in an Alpine container
```

To distribute the reader, use **`make reader-static`**: a binary linked against the glibc of a newer system does not
start on an older one (for example, built on Ubuntu 24.04 it does not run on 20.04).

## Measured performance

NOJA Power recloser with firmware 1.27, from a server on the same LAN: states and measurements (89 reads, ~1470
values) in **3.7 s** with the 20 ms pause; the whole model in 9.8 s. Each row takes about 1.3 KB (`mx`) and 3.4 KB
(`st`) in PostgreSQL, compressed.

## License

**GNU GPL v3** (see [`LICENSE`](LICENSE)), because the reader links libiec61850, which is distributed under GPLv3 (or
a commercial license from MZ Automation GmbH). The libiec61850 source matching each version is pinned as a submodule
in `third_party/libiec61850`.
