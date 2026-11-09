-- +goose Up
-- Reading a definition is a read of the system's shape, not of its data, so it
-- does not belong in audit_entry: that table carries a statement and a guard
-- decision, and a definition read has neither. The predecessor records nothing here at
-- all. See FEAT-023 AC-12.
CREATE TABLE definition_read (
    id          uuid PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES app_user (id),
    instance_id uuid        NOT NULL REFERENCES db_instance (id),
    database    text        NOT NULL,
    object_type text        NOT NULL,
    schema_name text        NOT NULL,
    object_name text        NOT NULL,
    -- False records an attempt to read something absent. A sweep for object
    -- names is only visible if the misses are kept.
    found       boolean     NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX definition_read_user_created_idx ON definition_read (user_id, created_at DESC);
CREATE INDEX definition_read_object_idx ON definition_read (database, object_type, object_name);

-- +goose Down
DROP TABLE definition_read;
