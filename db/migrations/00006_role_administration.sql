-- +goose Up
-- FEAT-004. What an administrator did to somebody's access, and when.
--
-- Append-only for the reason the release ledger is: a record of who was given
-- reach into production is worth nothing if it can be revised afterwards. The
-- triggers from 00004 are reused rather than restated.
CREATE TABLE account_change (
    id         uuid PRIMARY KEY,
    -- Who made the change. Not cascaded from app_user: an administrator who
    -- leaves does not take the record of what they granted with them.
    actor_id   uuid        NOT NULL REFERENCES app_user (id),
    subject_id uuid        NOT NULL REFERENCES app_user (id),
    -- grant, revoke, enable, disable.
    action     text        NOT NULL,
    -- The role for a grant or a revoke, empty for a status change.
    role       text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT account_change_action_known CHECK (
        action IN ('grant', 'revoke', 'enable', 'disable')
    ),
    CONSTRAINT account_change_role_present CHECK (
        (action IN ('grant', 'revoke')) = (role <> '')
    )
);

CREATE INDEX account_change_subject_idx ON account_change (subject_id, changed_at DESC);

CREATE TRIGGER account_change_is_append_only
    BEFORE UPDATE OR DELETE ON account_change
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

-- +goose Down
DROP TRIGGER account_change_is_append_only ON account_change;
DROP TABLE account_change;
