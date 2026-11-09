-- +goose Up

-- FEAT-063. An account for an agent that is nobody's person.
--
-- A kind rather than a separate table: a service account is granted roles,
-- holds tokens, and is named on audit rows exactly the way a person is, and
-- every one of those references points at app_user. A second table would need
-- a second copy of each.
--
-- created_by names the administrator who made it and nothing more. It is not an
-- owner: ownership is whoever holds account.manage, so nobody's departure stops
-- the agent. Not cascaded, for the reason account_change's actor is not.
ALTER TABLE app_user
    ADD COLUMN kind        text NOT NULL DEFAULT 'person',
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD COLUMN created_by  uuid REFERENCES app_user (id);

ALTER TABLE app_user ADD CONSTRAINT app_user_kind_known CHECK (kind IN ('person', 'service'));

-- ADR 0006, as amended. A service account is the one identity Avior makes
-- without the directory, and so it can never sign in: no password, no second
-- factor, never declared. Stated here as well as in the verifiers, so a
-- declaration that later names one fails at the row rather than handing it a
-- password.
ALTER TABLE app_user ADD CONSTRAINT app_user_service_cannot_sign_in CHECK (
    kind <> 'service' OR (
        lower(username) LIKE 'svc-%'
        AND password_hash = ''
        AND totp_secret = ''
        AND NOT declared
        AND length(btrim(description)) > 0
    )
);

-- The creation is recorded beside every grant, revoke and disable.
ALTER TABLE account_change DROP CONSTRAINT account_change_action_known;
ALTER TABLE account_change ADD CONSTRAINT account_change_action_known CHECK (
    action IN ('grant', 'revoke', 'enable', 'disable', 'provision', 'create')
);

ALTER TABLE account_change DROP CONSTRAINT account_change_role_present;
ALTER TABLE account_change ADD CONSTRAINT account_change_role_present CHECK (
    (action IN ('grant', 'revoke') AND role <> '')
    OR (action IN ('enable', 'disable', 'create') AND role = '')
    OR action = 'provision'
);

-- FEAT-063 AC-7 and AC-8. Which token carried a statement, and who the agent
-- says asked for it. Both nullable: a statement typed into the console carries
-- no token, and the note is free text nobody has to send.
--
-- requested_by is bounded here as well as in the contract, so a caller that
-- reached the store some other way cannot write a note the contract refuses.
ALTER TABLE audit_entry
    ADD COLUMN token_id     uuid REFERENCES api_token (id),
    ADD COLUMN requested_by text;

ALTER TABLE audit_entry ADD CONSTRAINT audit_entry_requested_by_bounded CHECK (
    requested_by IS NULL OR length(requested_by) <= 200
);

CREATE INDEX audit_entry_token_idx ON audit_entry (token_id, created_at DESC)
    WHERE token_id IS NOT NULL;

-- +goose Down
DROP INDEX audit_entry_token_idx;
ALTER TABLE audit_entry DROP CONSTRAINT audit_entry_requested_by_bounded;
ALTER TABLE audit_entry DROP COLUMN requested_by, DROP COLUMN token_id;

ALTER TABLE account_change DROP CONSTRAINT account_change_role_present;
ALTER TABLE account_change ADD CONSTRAINT account_change_role_present CHECK (
    (action IN ('grant', 'revoke') AND role <> '')
    OR (action IN ('enable', 'disable') AND role = '')
    OR action = 'provision'
);
ALTER TABLE account_change DROP CONSTRAINT account_change_action_known;
ALTER TABLE account_change ADD CONSTRAINT account_change_action_known CHECK (
    action IN ('grant', 'revoke', 'enable', 'disable', 'provision')
);

ALTER TABLE app_user DROP CONSTRAINT app_user_service_cannot_sign_in;
ALTER TABLE app_user DROP CONSTRAINT app_user_kind_known;
ALTER TABLE app_user DROP COLUMN created_by, DROP COLUMN description, DROP COLUMN kind;
