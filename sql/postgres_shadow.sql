-- Target table for a `postgres_shadow` output: one row per plant, device, group and timestamp, with the
-- group's IEC 61850 data in a JSONB payload. Generic "shadow" layout: any device fits without
-- changing the table, and typed tables can be derived from it later. The agent does not create tables.
--
-- Indexes: the primary key is the ON CONFLICT target of the writer and also serves the usual reads
-- (one device and group over a time range: plant, device_name, slave_name, ts). No other index is
-- needed until a query filters on the payload.

CREATE SCHEMA IF NOT EXISTS edge;

CREATE TABLE IF NOT EXISTS edge.example_iec61850_shadow (
  plant       TEXT        NOT NULL,
  ts          TIMESTAMPTZ NOT NULL,
  device_name TEXT        NOT NULL,
  slave_name  TEXT        NOT NULL,  -- group name (e.g. mx, st)
  payload     JSONB       NOT NULL,
  series_key  TEXT        NOT NULL,  -- group name again (keeps a generic key shape)
  ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (plant, device_name, slave_name, ts, series_key)
);

-- Reading a measurement back (phase A current of MMXU1 in LD0):
--   SELECT ts, (payload #>> '{fields,LD0,MMXU1$MX,A,phsA,cVal,mag,f}')::double precision AS ia
--   FROM edge.example_iec61850_shadow
--   WHERE device_name = 'noja_recloser' AND series_key = 'mx'
--   ORDER BY ts DESC LIMIT 10;
-- Breaker position (IEC 61850 double point: "10" closed, "01" open):
--   SELECT ts, payload #>> '{fields,LD0,XCBR1$ST,Pos,stVal}' FROM edge.example_iec61850_shadow
--   WHERE series_key = 'st' ORDER BY ts DESC LIMIT 10;
