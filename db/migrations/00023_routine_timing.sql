-- +goose Up

-- FEAT-073. A routine's time as pg_stat_user_functions counts it, read by the
-- statement FEAT-071's snapshot already sends.

-- The last total and self milliseconds read per routine, the baseline of the
-- next interval's delta. Null for a relation, and for a routine until the
-- first snapshot that read its time.
ALTER TABLE observatory_object_usage
    ADD COLUMN total_ms double precision,
    ADD COLUMN self_ms  double precision;

-- One row per routine per snapshot interval in which its calls rose. A
-- routine not called writes nothing. 30 days.
CREATE TABLE observatory_routine_timing (
    database       text             NOT NULL,
    node           text             NOT NULL,
    kind           text             NOT NULL CHECK (kind IN ('function', 'procedure')),
    schema_name    text             NOT NULL,
    name           text             NOT NULL,
    signature      text             NOT NULL,
    interval_start timestamptz      NOT NULL,
    interval_end   timestamptz      NOT NULL,
    calls          bigint           NOT NULL CHECK (calls > 0),
    total_ms       double precision NOT NULL,
    self_ms        double precision NOT NULL,
    PRIMARY KEY (database, node, kind, schema_name, name, signature, interval_end)
);

CREATE INDEX observatory_routine_timing_end_idx ON observatory_routine_timing (interval_end);

-- +goose Down
DROP TABLE observatory_routine_timing;
ALTER TABLE observatory_object_usage DROP COLUMN self_ms, DROP COLUMN total_ms;
