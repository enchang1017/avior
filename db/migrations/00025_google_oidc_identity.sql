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

-- +goose Down
DROP INDEX app_user_oidc_identity_key;
ALTER TABLE app_user DROP CONSTRAINT app_user_oidc_pair_complete;
ALTER TABLE app_user
    DROP COLUMN oidc_subject,
    DROP COLUMN oidc_issuer;
