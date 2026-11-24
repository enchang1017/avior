-- +goose Up
-- FEAT-077. Bind an operator account to its verified OpenID Connect issuer and
-- subject so a reassigned Google Workspace email cannot inherit an existing
-- account and its roles.
ALTER TABLE app_user
    ADD COLUMN oidc_issuer  text NOT NULL DEFAULT '',
    ADD COLUMN oidc_subject text NOT NULL DEFAULT '';

-- Both columns are set together or both empty. Service accounts and declared
-- local accounts never carry an OIDC identity.
ALTER TABLE app_user ADD CONSTRAINT app_user_oidc_pair_complete CHECK (
    (oidc_issuer = '' AND oidc_subject = '')
    OR (
        oidc_issuer <> ''
        AND oidc_subject <> ''
        AND kind = 'person'
        AND NOT declared
    )
);

-- One verified (issuer, subject) maps to at most one Avior account.
CREATE UNIQUE INDEX app_user_oidc_identity_key
    ON app_user (oidc_issuer, oidc_subject)
    WHERE oidc_issuer <> '';

-- FEAT-077. Record first-time OIDC binding of an existing unbound account in
-- the append-only account_change ledger, capturing only the verified issuer and
-- subject (never authorization codes, tokens, secrets, or raw ID tokens).
ALTER TABLE account_change
    ADD COLUMN oidc_issuer  text NOT NULL DEFAULT '',
    ADD COLUMN oidc_subject text NOT NULL DEFAULT '';

ALTER TABLE account_change DROP CONSTRAINT account_change_action_known;
ALTER TABLE account_change ADD CONSTRAINT account_change_action_known CHECK (
    action IN ('grant', 'revoke', 'enable', 'disable', 'provision', 'create', 'bind_oidc')
);

ALTER TABLE account_change DROP CONSTRAINT account_change_role_present;
ALTER TABLE account_change ADD CONSTRAINT account_change_role_present CHECK (
    (action IN ('grant', 'revoke') AND role <> '')
    OR (action IN ('enable', 'disable', 'create', 'bind_oidc') AND role = '')
    OR action = 'provision'
);

ALTER TABLE account_change ADD CONSTRAINT account_change_oidc_present CHECK (
    (action = 'bind_oidc' AND oidc_issuer <> '' AND oidc_subject <> '')
    OR (action <> 'bind_oidc' AND oidc_issuer = '' AND oidc_subject = '')
);

-- +goose Down
ALTER TABLE account_change DROP CONSTRAINT account_change_oidc_present;

ALTER TABLE account_change DROP CONSTRAINT account_change_role_present;
ALTER TABLE account_change ADD CONSTRAINT account_change_role_present CHECK (
    (action IN ('grant', 'revoke') AND role <> '')
    OR (action IN ('enable', 'disable', 'create') AND role = '')
    OR action = 'provision'
);

ALTER TABLE account_change DROP CONSTRAINT account_change_action_known;
ALTER TABLE account_change ADD CONSTRAINT account_change_action_known CHECK (
    action IN ('grant', 'revoke', 'enable', 'disable', 'provision', 'create')
);

ALTER TABLE account_change
    DROP COLUMN oidc_subject,
    DROP COLUMN oidc_issuer;

DROP INDEX app_user_oidc_identity_key;
ALTER TABLE app_user DROP CONSTRAINT app_user_oidc_pair_complete;
ALTER TABLE app_user
    DROP COLUMN oidc_subject,
    DROP COLUMN oidc_issuer;
