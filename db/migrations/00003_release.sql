-- +goose Up
-- FEAT-017. The ledger the database project's CI submits, and everything that
-- happens to an entry afterwards.
--
-- Nothing here is derived. Avior does not generate migrations and does not
-- compare schemas, so every column below is either something CI handed over or
-- something Avior did with it. ADR-0004 draws that line.
CREATE TABLE release (
    id                 uuid PRIMARY KEY,
    -- The target database on the governed server. Not an instance: ADR-0005 puts
    -- every governed database on one server per environment.
    database           text        NOT NULL,
    commit_sha         text        NOT NULL,
    merge_request_url  text,
    author             text        NOT NULL,
    -- Over the migration files, in order. With commit_sha it identifies a
    -- resubmission: CI re-runs, and one merge must not produce two entries an
    -- approver has to choose between.
    migration_checksum text        NOT NULL,
    lint_findings      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    verify_result      jsonb,
    staging_synced_at  timestamptz,
    -- What CI says the target should hold *before* this migration, computed over
    -- the database it built by replaying the history. The drift check compares
    -- it with the target's real digest.
    expected_digest    text        NOT NULL,
    state              text        NOT NULL,
    submitted_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT release_state_known CHECK (
        state IN ('pending', 'approved', 'rejected', 'applied', 'failed')
    ),
    CONSTRAINT release_commit_is_one_entry UNIQUE (database, commit_sha, migration_checksum)
);

CREATE INDEX release_state_submitted_idx ON release (state, submitted_at DESC);
CREATE INDEX release_database_idx ON release (database, submitted_at DESC);

-- The SQL as submitted, in the order it must run. Stored rather than referenced:
-- what an approver read has to be what runs, and a file fetched again later is
-- not necessarily the same file.
CREATE TABLE release_migration (
    release_id uuid    NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    ordinal    integer NOT NULL,
    filename   text    NOT NULL,
    sql        text    NOT NULL,
    PRIMARY KEY (release_id, ordinal)
);

-- Every check, not only the last. The one before an apply is the one that
-- mattered, and knowing a release listed clean an hour earlier is how somebody
-- reconstructs what happened.
CREATE TABLE release_drift_check (
    id              uuid PRIMARY KEY,
    release_id      uuid        NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    result          text        NOT NULL,
    expected_digest text,
    actual_digest   text,
    detail          text        NOT NULL DEFAULT '',
    checked_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT release_drift_result_known CHECK (
        result IN ('match', 'drifted', 'unavailable')
    )
);

CREATE INDEX release_drift_check_release_idx ON release_drift_check (release_id, checked_at DESC);

-- Who let this reach production. The question this feature exists to answer, so
-- the row is kept whatever happens to the release afterwards.
CREATE TABLE release_approval (
    id         uuid PRIMARY KEY,
    release_id uuid        NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES app_user (id),
    decision   text        NOT NULL,
    note       text        NOT NULL DEFAULT '',
    decided_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT release_decision_known CHECK (decision IN ('approve', 'reject'))
);

CREATE INDEX release_approval_release_idx ON release_approval (release_id, decided_at DESC);

-- An attempt, not a success. A failed apply leaves a row saying so, because a
-- migration that failed against production is more interesting than one that
-- did not.
CREATE TABLE release_application (
    id            uuid PRIMARY KEY,
    release_id    uuid        NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    applied_by    uuid        NOT NULL REFERENCES app_user (id),
    started_at    timestamptz NOT NULL DEFAULT now(),
    result        text        NOT NULL,
    duration_ms   integer,
    files_applied integer     NOT NULL DEFAULT 0,
    error         text        NOT NULL DEFAULT '',
    CONSTRAINT release_application_result_known CHECK (result IN ('succeeded', 'failed'))
);

CREATE INDEX release_application_release_idx ON release_application (release_id, started_at DESC);

-- A machine caller's credential, for the CI that submits a ledger entry.
--
-- Deliberately thin: one hashed token per issuer, no rotation, no expiry, no
-- scope beyond "may submit". FEAT-053 is the real answer and this should not
-- outlive it. It exists because AC-1 needs a caller that is not an operator —
-- a session cannot submit and a token cannot approve.
CREATE TABLE release_token (
    id          uuid PRIMARY KEY,
    issuer      text        NOT NULL UNIQUE,
    token_hash  bytea       NOT NULL UNIQUE,
    disabled    boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

-- +goose Down
DROP TABLE release_token;
DROP TABLE release_application;
DROP TABLE release_approval;
DROP TABLE release_drift_check;
DROP TABLE release_migration;
DROP TABLE release;
