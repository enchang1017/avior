-- +goose Up
-- FEAT-076 AC-5. Two more things the provisioning ledger records.
--
-- connect: a role was granted CONNECT on a database, named <role>@<database>
-- because the act has two subjects and the name is the only column for them.
-- writes: a database was brought under the write path, named for the database.
ALTER TABLE provision_change
    DROP CONSTRAINT provision_change_kind_known,
    ADD CONSTRAINT provision_change_kind_known CHECK (
        kind IN ('database', 'role', 'membership', 'access', 'login', 'password', 'drop',
                 'connect', 'writes')
    );

-- +goose Down
DELETE FROM provision_change WHERE kind IN ('connect', 'writes');
ALTER TABLE provision_change
    DROP CONSTRAINT provision_change_kind_known,
    ADD CONSTRAINT provision_change_kind_known CHECK (
        kind IN ('database', 'role', 'membership', 'access', 'login', 'password', 'drop')
    );
