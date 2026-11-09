-- +goose Up
-- Local development only. The password is "avior" and the TOTP secret is the
-- fixed base32 value below, so a developer can generate a code without
-- enrolling a device. Neither ever reaches a real environment: config.Load
-- refuses to start in production with the development session secret.
INSERT INTO app_user (id, username, password_hash, totp_secret)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'developer',
    '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu',
    'JBSWY3DPEHPK3PXP'
)
ON CONFLICT (lower(username)) DO NOTHING;

INSERT INTO app_user_role (user_id, role)
VALUES ('00000000-0000-0000-0000-000000000001', 'developer')
ON CONFLICT DO NOTHING;

-- A second operator, because several rules differ by role and a fixture with
-- one role can only ever demonstrate half of them. The rail shows the unbuilt
-- entries to an administrator and to nobody else, and an administrator holds
-- audit.read.all where a developer holds audit.read.own.
INSERT INTO app_user (id, username, password_hash, totp_secret)
VALUES (
    '00000000-0000-0000-0000-000000000002',
    'administrator',
    '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu',
    'JBSWY3DPEHPK3PXP'
)
ON CONFLICT (lower(username)) DO NOTHING;

INSERT INTO app_user_role (user_id, role)
VALUES ('00000000-0000-0000-0000-000000000002', 'administrator')
ON CONFLICT DO NOTHING;

-- Two more, because several rules differ by role and the rail differs by
-- permission. A fixture holding one operator of each kind is what makes those
-- differences checkable at all.
INSERT INTO app_user (id, username, password_hash, totp_secret)
VALUES
    (
        '00000000-0000-0000-0000-000000000003',
        'dba',
        '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu',
        'JBSWY3DPEHPK3PXP'
    ),
    (
        '00000000-0000-0000-0000-000000000004',
        'approver',
        '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu',
        'JBSWY3DPEHPK3PXP'
    ),
    -- FEAT-009. A separate person from the release approver, which is the whole
    -- point of the role: letting a schema change through and letting somebody
    -- edit rows are different judgements. Holds developer as well, because a
    -- write request names a database and FEAT-058 refuses one the approver's
    -- roles do not reach.
    (
        '00000000-0000-0000-0000-000000000005',
        'writeapprover',
        '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu',
        'JBSWY3DPEHPK3PXP'
    )
ON CONFLICT (lower(username)) DO NOTHING;

INSERT INTO app_user_role (user_id, role)
VALUES
    ('00000000-0000-0000-0000-000000000003', 'dba'),
    ('00000000-0000-0000-0000-000000000004', 'release_approver'),
    ('00000000-0000-0000-0000-000000000005', 'update_approver'),
    ('00000000-0000-0000-0000-000000000005', 'dba')
ON CONFLICT DO NOTHING;

-- A machine credential, so the release path can be walked by hand without
-- somebody minting one first. This is the SHA-256 of
-- rel_local_development_release_token, which is local-only for the same reason
-- the password above is.
INSERT INTO release_token (id, issuer, token_hash)
VALUES (
    '00000000-0000-0000-0000-0000000000f1',
    'local-development',
    decode('3c2d619f8b8997e586c11b383b7a7d4313eb6ca34f8a1dd6d68cec73b7055a31', 'hex')
)
ON CONFLICT (issuer) DO UPDATE SET token_hash = EXCLUDED.token_hash;

INSERT INTO db_instance (id, name, kind, host, port)
VALUES (
    '00000000-0000-0000-0000-0000000000a1',
    'local-postgres',
    'postgres',
    'postgres',
    5432
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name, kind = EXCLUDED.kind,
    host = EXCLUDED.host, port = EXCLUDED.port;

-- FEAT-058. Which roles reach which database. Deny by default, so without these
-- the local stack shows an empty tree and every query is refused — which is the
-- point of the table, and the reason a deployment configures it before it is
-- used.
--
-- The deploy database is granted to the DBA and not to the developer, so the
-- local stack demonstrates the difference rather than only describing it.
INSERT INTO database_grant (id, role, database, reason)
VALUES
    ('00000000-0000-0000-0000-0000000000b1', 'developer', 'aviorfixture', 'local development'),
    ('00000000-0000-0000-0000-0000000000b2', 'dba', 'aviorfixture', 'local development'),
    ('00000000-0000-0000-0000-0000000000b3', 'release_approver', 'aviorfixture', 'local development'),
    ('00000000-0000-0000-0000-0000000000b4', 'dba', 'aviordeploy', 'local development'),
    ('00000000-0000-0000-0000-0000000000b5', 'developer', 'aviordeploy', 'the acceptance suite queries it'),
    ('00000000-0000-0000-0000-0000000000b6', 'update_approver', 'aviorfixture', 'local development')
ON CONFLICT (role, database) DO NOTHING;

-- FEAT-055. One restricted column, so the guard visibly refuses something out
-- of the box — and one unlock, so the page has both states to show.
--
-- Restricted means refused to every role, including dba and administrator. The
-- unlock is what makes it readable, and it names who allowed it.
INSERT INTO column_restriction (id, database, object_name, column_name, reason)
VALUES (
    '00000000-0000-0000-0000-0000000000b1',
    'aviorfixture', 'public.member', 'id_card_number',
    'A national identity number. Restricted by default; ask if you need it.'
)
ON CONFLICT (database, object_name, column_name) DO NOTHING;

-- +goose Down
DELETE FROM column_restriction WHERE id = '00000000-0000-0000-0000-0000000000b1';
DELETE FROM database_grant WHERE reason IN ('local development', 'the acceptance suite queries it');
DELETE FROM db_instance WHERE id = '00000000-0000-0000-0000-0000000000a1';
DELETE FROM app_user WHERE id = '00000000-0000-0000-0000-000000000001';
