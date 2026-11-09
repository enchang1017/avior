-- +goose Up
CREATE TABLE app_user (
    id            uuid PRIMARY KEY,
    username      text        NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    totp_secret   text        NOT NULL,
    disabled      boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app_user_role (
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    role    text NOT NULL,
    PRIMARY KEY (user_id, role)
);

CREATE TABLE user_session (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX user_session_expires_at_idx ON user_session (expires_at);

CREATE TABLE db_instance (
    id       uuid PRIMARY KEY,
    name     text    NOT NULL UNIQUE,
    kind     text    NOT NULL,
    host     text    NOT NULL,
    port     integer NOT NULL,
    disabled boolean NOT NULL DEFAULT false
);

-- Denials rather than grants. A new column is unreadable only if someone says
-- so, but the alternative -- listing every readable column -- silently exposes
-- every column added after the list was written.
CREATE TABLE column_denial (
    id          uuid PRIMARY KEY,
    role        text NOT NULL,
    database    text NOT NULL,
    object_name text NOT NULL,
    column_name text NOT NULL,
    reason      text NOT NULL DEFAULT '',
    UNIQUE (role, database, object_name, column_name)
);

CREATE TABLE table_denial (
    id          uuid PRIMARY KEY,
    role        text NOT NULL,
    database    text NOT NULL,
    object_name text NOT NULL,
    reason      text NOT NULL DEFAULT '',
    UNIQUE (role, database, object_name)
);

-- Append only. The row is written before the statement reaches the target
-- server, so an audit trail exists even when execution fails or times out.
CREATE TABLE audit_entry (
    id          uuid PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES app_user (id),
    instance_id uuid        NOT NULL REFERENCES db_instance (id),
    database    text        NOT NULL,
    statement   text        NOT NULL,
    decision    text        NOT NULL,
    reasons     jsonb       NOT NULL DEFAULT '[]'::jsonb,
    row_count   integer,
    elapsed_ms  integer,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_entry_user_created_idx ON audit_entry (user_id, created_at DESC);
CREATE INDEX audit_entry_created_idx ON audit_entry (created_at DESC);

-- +goose Down
DROP TABLE audit_entry;
DROP TABLE table_denial;
DROP TABLE column_denial;
DROP TABLE db_instance;
DROP TABLE user_session;
DROP TABLE app_user_role;
DROP TABLE app_user;
