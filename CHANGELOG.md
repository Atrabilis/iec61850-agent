# Changelog

## Unreleased

- First version: `iec61850-read` reader (C, libiec61850 v1.5.3, read-only, never `CO`) and `iec61850-agent` (Go) with
  YAML configuration per device type, groups by functional constraint, `postgres_shadow` output and the
  `-interrogateOnly`, `-checkConfig` and `-version` options.
- Tested against a NOJA Power recloser (firmware 1.27) and a local PostgreSQL 17.9: one row per group (`mx`, `st`) with
  the IED name, nameplate and device timestamps in the payload.
