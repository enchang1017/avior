-- +goose Up
-- FEAT-061 AC-6. Three more things the provisioning ledger records.
--
-- login: a role that logs in was created. password: its password was replaced.
-- drop: a role was removed. The name is all that is written — never the
-- password, which exists in one HTTP response and nowhere else. For a drop,
-- `created` is true when the role was there and is gone; the column's name is
-- FEAT-059's and reads as "this call was the one that changed something".
ALTER TABLE provision_change
    DROP CONSTRAINT provision_change_kind_known,
    ADD CONSTRAINT provision_change_kind_known CHECK (
        kind IN ('database', 'role', 'membership', 'access', 'login', 'password', 'drop')
    );

-- +goose Down
DELETE FROM provision_change WHERE kind IN ('login', 'password', 'drop');
ALTER TABLE provision_change
    DROP CONSTRAINT provision_change_kind_known,
    ADD CONSTRAINT provision_change_kind_known CHECK (
        kind IN ('database', 'role', 'membership', 'access')
    );
