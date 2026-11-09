-- +goose Up

-- FEAT-071 phases 2 and 3. The history PostgreSQL does not keep: its
-- statistics counters carry no time, so the snapshot job reads them on a
-- schedule and these tables hold what it read. Retention is in FEAT-071 and
-- the purge at the end of each run enforces it.

-- One row per snapshot run, whichever replica won the advisory lock.
CREATE TABLE observatory_run (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    started_at       timestamptz NOT NULL,
    finished_at      timestamptz NOT NULL,
    databases_read   text[]      NOT NULL,
    databases_failed text[]      NOT NULL,
    -- Set when a connected standby is not one the observatory reads, and no
    -- object is flagged while it is.
    flags_withheld   text
);

CREATE INDEX observatory_run_finished_idx ON observatory_run (finished_at);

-- Per database and node: when tracking began and the epoch of its statistics,
-- which is how a reset is detected rather than trusted away.
CREATE TABLE observatory_database_state (
    database               text        NOT NULL,
    node                   text        NOT NULL,
    first_snapshot_at      timestamptz NOT NULL,
    routines_tracked_since timestamptz,
    stats_reset            timestamptz,
    postmaster_start       timestamptz,
    last_reset_at          timestamptz,
    last_snapshot_at       timestamptz NOT NULL,
    PRIMARY KEY (database, node)
);

-- The last counter read per object per node and the first snapshot at which
-- it had risen. Kept while the object exists and 90 days after it disappears.
CREATE TABLE observatory_object_usage (
    database           text        NOT NULL,
    node               text        NOT NULL,
    kind               text        NOT NULL CHECK (kind IN ('table', 'index', 'function', 'procedure')),
    schema_name        text        NOT NULL,
    name               text        NOT NULL,
    -- A routine's identity arguments; empty for a relation.
    signature          text        NOT NULL,
    -- The table an index belongs to.
    parent             text,
    counter            bigint      NOT NULL,
    last_increase_at   timestamptz,
    -- The newest scan time the server reported, kept past a reset.
    last_read_at       timestamptz,
    tracked            boolean     NOT NULL,
    not_tracked_reason text,
    constraint_backed  boolean     NOT NULL,
    first_seen_at      timestamptz NOT NULL,
    last_seen_at       timestamptz NOT NULL,
    disappeared_at     timestamptz,
    PRIMARY KEY (database, node, kind, schema_name, name, signature)
);

CREATE INDEX observatory_object_usage_gone_idx ON observatory_object_usage (disappeared_at)
    WHERE disappeared_at IS NOT NULL;

-- Use counted per object per UTC day. 90 days.
CREATE TABLE observatory_usage_daily (
    database    text   NOT NULL,
    node        text   NOT NULL,
    kind        text   NOT NULL,
    schema_name text   NOT NULL,
    name        text   NOT NULL,
    signature   text   NOT NULL,
    day         date   NOT NULL,
    delta       bigint NOT NULL,
    PRIMARY KEY (database, node, kind, schema_name, name, signature, day)
);

CREATE INDEX observatory_usage_daily_day_idx ON observatory_usage_daily (day);

-- Sizes, once per object per UTC day: the day's last snapshot wins. 400 days,
-- so a year-on-year comparison exists.
CREATE TABLE observatory_db_size_daily (
    database   text   NOT NULL,
    day        date   NOT NULL,
    size_bytes bigint NOT NULL,
    PRIMARY KEY (database, day)
);

CREATE INDEX observatory_db_size_daily_day_idx ON observatory_db_size_daily (day);

CREATE TABLE observatory_table_size_daily (
    database    text   NOT NULL,
    schema_name text   NOT NULL,
    name        text   NOT NULL,
    day         date   NOT NULL,
    size_bytes  bigint NOT NULL,
    PRIMARY KEY (database, schema_name, name, day)
);

CREATE INDEX observatory_table_size_daily_db_day_idx ON observatory_table_size_daily (database, day);
CREATE INDEX observatory_table_size_daily_day_idx ON observatory_table_size_daily (day);

-- The costliest statements per hour, with their text. 14 days: the text is
-- the sensitive part, and two weeks answers "what was slow last week".
CREATE TABLE observatory_statement_hourly (
    hour           timestamptz      NOT NULL,
    query_id       text             NOT NULL,
    database       text             NOT NULL,
    role           text             NOT NULL,
    calls          bigint           NOT NULL,
    total_ms       double precision NOT NULL,
    rows           bigint           NOT NULL,
    calls_delta    bigint           NOT NULL,
    total_ms_delta double precision NOT NULL,
    text           text             NOT NULL,
    PRIMARY KEY (hour, query_id, database, role)
);

CREATE INDEX observatory_statement_hourly_query_idx ON observatory_statement_hourly (query_id, database, role, hour DESC);

-- Every reset the snapshot saw. 90 days.
CREATE TABLE observatory_reset (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    database    text        NOT NULL,
    node        text        NOT NULL,
    detected_at timestamptz NOT NULL,
    reason      text        NOT NULL
);

CREATE INDEX observatory_reset_detected_idx ON observatory_reset (detected_at);
CREATE INDEX observatory_reset_database_idx ON observatory_reset (database, detected_at DESC);

-- Who read statement text. Not an audit_entry, because nothing was submitted
-- to a governed database; kept as long as audit_entry is.
CREATE TABLE observatory_text_view (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES app_user (id),
    database_filter text,
    tab             text        NOT NULL CHECK (tab IN ('statements', 'activity')),
    texts           integer     NOT NULL CHECK (texts > 0),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX observatory_text_view_user_idx ON observatory_text_view (user_id, created_at DESC);

-- +goose Down
DROP TABLE observatory_text_view;
DROP TABLE observatory_reset;
DROP TABLE observatory_statement_hourly;
DROP TABLE observatory_table_size_daily;
DROP TABLE observatory_db_size_daily;
DROP TABLE observatory_usage_daily;
DROP TABLE observatory_object_usage;
DROP TABLE observatory_database_state;
DROP TABLE observatory_run;
