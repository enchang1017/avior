-- +goose Up
-- FEAT-056. Accounts a deployment declares in a file rather than a directory.
--
-- The flag is what keeps the two sets of accounts apart. A declared account is
-- verified against the hash below and never reaches the directory, and its name
-- cannot be taken by a directory account — which is the whole of the isolation,
-- the way ClickHouse's ordered user directories are.
ALTER TABLE app_user ADD COLUMN declared boolean NOT NULL DEFAULT false;

-- Names are reserved without regard to case, because a directory folds case and
-- would otherwise hand `Avior_Admin` to whoever the directory holds under that
-- spelling. The index makes the check a lookup rather than a scan and states
-- the rule where the data is.
CREATE UNIQUE INDEX app_user_declared_name_idx
    ON app_user (lower(username)) WHERE declared;

-- FEAT-056 AC-12. A change with no actor is the deployment applying its file.
--
-- Nullable rather than pointed at some standing "system" account: a row in
-- app_user that nobody can sign in to would still be an account, and it would
-- appear on the page as one. Absence says what happened without inventing a
-- principal. A grant that names an actor is somebody who decided; one that
-- names nobody is a file being applied.
ALTER TABLE account_change ALTER COLUMN actor_id DROP NOT NULL;

-- +goose Down
ALTER TABLE account_change ALTER COLUMN actor_id SET NOT NULL;
DROP INDEX app_user_declared_name_idx;
ALTER TABLE app_user DROP COLUMN declared;
