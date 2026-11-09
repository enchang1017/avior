-- +goose Up
-- FEAT-007 AC-6. One operator is one account, whatever case they typed.
--
-- The directory accepts JANE.DOE61 and jane.doe61 for the same person, and this table
-- keys an operator on the spelling it was given — so the same person signing in
-- twice, typing it differently, made two accounts with two sets of roles.
--
-- Until now that was prevented by asking the directory for its own spelling of
-- the name after every bind: a whole-subtree search under the domain root, run
-- on every sign-in, to read back a string. The property is worth keeping and
-- that was an expensive way to keep it. It belongs here, where the uniqueness
-- it protects is already declared.
--
-- This fails if the table already holds two rows differing only in case. That
-- is the right failure: which of them keeps its roles is a decision, not
-- something a migration should make by dropping one.
ALTER TABLE app_user DROP CONSTRAINT app_user_username_key;

CREATE UNIQUE INDEX app_user_username_lower_key ON app_user (lower(username));

-- +goose Down
DROP INDEX app_user_username_lower_key;

ALTER TABLE app_user ADD CONSTRAINT app_user_username_key UNIQUE (username);
