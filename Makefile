# Builds both executables into bin/:
#   iec61850-read   C reader (links libiec61850, GPLv3), the only part that talks IEC 61850
#   iec61850-agent  Go agent (no cgo): reads the YAML, runs the reader, writes PostgreSQL
#
# libiec61850 is a git submodule pinned to a release tag; run `git submodule update --init` first.
# Cross builds of the reader: make reader CC=aarch64-linux-gnu-gcc LIBTARGET=LINUX-ARM  (see libiec61850 make/)

LIBIEC  := third_party/libiec61850
CC      ?= gcc
CFLAGS  ?= -O2 -Wall -Wextra
GOARCH  ?= $(shell go env GOARCH)
LIBTARGET ?=

LIB_A   := $(LIBIEC)/build/libiec61850.a
INCLUDES := -I$(LIBIEC)/config -I$(LIBIEC)/hal/inc -I$(LIBIEC)/src/common/inc -I$(LIBIEC)/src/mms/inc \
            -I$(LIBIEC)/src/mms/inc_private -I$(LIBIEC)/src/mms/asn1 -I$(LIBIEC)/src/iec61850/inc -I$(LIBIEC)/src/logging

.PHONY: all reader reader-static agent test clean

all: reader agent

$(LIB_A):
	$(MAKE) -C $(LIBIEC) lib $(if $(LIBTARGET),TARGET=$(LIBTARGET)) CC=$(CC)

reader: $(LIB_A) bridge/iec61850_read.c
	@mkdir -p bin
	$(CC) $(CFLAGS) -o bin/iec61850-read bridge/iec61850_read.c $(INCLUDES) $(LIB_A) -lpthread -lm

# Static reader (musl, Alpine container): runs on any Linux host regardless of its glibc
# (a glibc build from Ubuntu 24.04 does not start on Ubuntu 20.04). PLATFORM=linux/arm64 needs qemu.
ALPINE   ?= alpine:3.20.3
PLATFORM ?= linux/amd64
reader-static:
	@mkdir -p bin
	docker run --rm --platform $(PLATFORM) -v "$(CURDIR)":/src:ro -v "$(CURDIR)/bin":/out $(ALPINE) sh -c '\
	  apk add -q build-base linux-headers >/dev/null && cp -r /src /tmp/b && cd /tmp/b && \
	  make -C $(LIBIEC) clean >/dev/null 2>&1; make -C $(LIBIEC) lib >/dev/null && \
	  cc -O2 -Wall -Wextra -static -o /out/iec61850-read-$$(uname -m) bridge/iec61850_read.c $(INCLUDES) $(LIB_A) -lpthread -lm && \
	  chown $(shell id -u):$(shell id -g) /out/iec61850-read-$$(uname -m)'

agent:
	@mkdir -p bin
	CGO_ENABLED=0 GOARCH=$(GOARCH) go build -o bin/iec61850-agent ./cmd/iec61850-agent

test:
	go vet ./...
	go test ./...

clean:
	rm -rf bin
	$(MAKE) -C $(LIBIEC) clean
