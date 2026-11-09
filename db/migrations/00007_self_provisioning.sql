-- +goose Up
-- FEAT-007. An account the directory vouched for, made on first sign-in.
--
-- Recorded as its own action rather than as a grant. A grant names an
-- administrator who decided somebody should have production access; this names
-- nobody deciding, and the two must not read alike in the one record that says
-- who was let in. The actor is the operator themselves, because that is who the
-- directory authenticated and there is nobody else to name.
ALTER TABLE account_change DROP CONSTRAINT account_change_action_known;
ALTER TABLE account_change ADD CONSTRAINT account_change_action_known CHECK (
    action IN ('grant', 'revoke', 'enable', 'disable', 'provision')
);

-- A provision names a role when the deployment grants one on first sign-in and
-- names none when it does not, so it is the one action where either is right.
-- The others are unchanged: a grant without a role says nothing, and an enable
-- with one says something that did not happen.
ALTER TABLE account_change DROP CONSTRAINT account_change_role_present;
ALTER TABLE account_change ADD CONSTRAINT account_change_role_present CHECK (
    (action IN ('grant', 'revoke') AND role <> '')
    OR (action IN ('enable', 'disable') AND role = '')
    OR action = 'provision'
);

-- +goose Down
ALTER TABLE account_change DROP CONSTRAINT account_change_role_present;
ALTER TABLE account_change ADD CONSTRAINT account_change_role_present CHECK (
    (action IN ('grant', 'revoke')) = (role <> '')
);

ALTER TABLE account_change DROP CONSTRAINT account_change_action_known;
ALTER TABLE account_change ADD CONSTRAINT account_change_action_known CHECK (
    action IN ('grant', 'revoke', 'enable', 'disable')
);
