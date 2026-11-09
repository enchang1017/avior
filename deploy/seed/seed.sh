#!/usr/bin/env bash
# Brings the local stack to a known state: the store schema applied, one
# operator account, one governed instance, one denied column, and the governed
# fixture database beside aviorDB on the same PostgreSQL server.
#
# Deterministic on purpose. An acceptance test that depends on random data is
# worse than no test, because it fails for reasons nobody can reproduce.
set -euo pipefail

echo "seed: applying store migrations"
goose -dir ./migrations postgres "$AVIOR_STORE_DSN" up

# FEAT-071. pg_stat_statements lives in Avior's own database and nowhere else;
# the server loads it through shared_preload_libraries (docker-compose.yml).
echo "seed: installing pg_stat_statements in the store"
psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements'

echo "seed: loading store fixtures"
goose -no-versioning -dir ./fixtures postgres "$AVIOR_STORE_DSN" up

# Dropped and rebuilt rather than applied idempotently. It is a test fixture,
# and a half-applied one is the kind of state that makes a failure impossible to
# reproduce. FORCE closes the backend's own connection to it, which is otherwise
# enough to make DROP DATABASE fail on a re-seed.
echo "seed: rebuilding the governed fixture database"
psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 <<'SQL'
-- Every governed database, found rather than listed.
--
-- The list used to be spelled out here, with a comment saying that a database
-- added above needs its owner added below. That was maintainable while the only
-- databases were the ones this file creates. FEAT-059 made Avior able to
-- provision a governed database at run time, so the set is no longer knowable
-- when this is written -- and the first one somebody created locally made the
-- seed refuse to run: DROP ROLE avior_governed failed because a database this
-- file had never heard of held privileges for it, which reads as a broken seed
-- rather than as a stale cluster.
--
-- Everything except Avior's own store and PostgreSQL's own is fixture, and
-- rebuilding the fixture is what this file is for.
--
-- FEAT-058's aviorspare is among them: a governed database granted to nobody,
-- so the refusal can be exercised without taking a grant away from a database
-- another case depends on. Grants are per role and therefore global, so a test
-- that revoked one would change the configuration every other test runs under.
SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', datname)
FROM pg_database
WHERE datname NOT IN ('avior', 'postgres', 'template0', 'template1')
\gexec

DROP ROLE IF EXISTS avior_reader;
DROP ROLE IF EXISTS avior_governed;
DROP ROLE IF EXISTS avior_deployer;
-- One owner role per governed database, named for it, which is what
-- governed.OwnerRole derives. Dropped by name for the same reason they are
-- created by name: a role left behind makes the fixture refuse to rebuild, and
-- "already exists" on the second run of a seed reads as a broken seed rather
-- than a stale cluster. A database added above needs its owner added here.
SELECT format('DROP ROLE IF EXISTS %I', rolname)
FROM pg_roles
WHERE rolname LIKE '%\_schema\_owner'
\gexec

-- FEAT-061 made the set of roles unknowable a second time: the provisioner
-- creates logins and group roles on request, and it is the grantor of every
-- membership between them, so it cannot be dropped while any of them stands.
-- What it administers is exactly what it created, so that is what goes.
SELECT format('DROP ROLE IF EXISTS %I', r.rolname)
FROM pg_auth_members m
JOIN pg_roles r ON r.oid = m.roleid
WHERE m.member = (SELECT oid FROM pg_roles WHERE rolname = 'avior_provisioner')
  AND m.admin_option
\gexec
DROP ROLE IF EXISTS avior_schema_owner;
DROP ROLE IF EXISTS avior_provisioner;
CREATE DATABASE aviorfixture;
CREATE DATABASE aviordeploy;
CREATE DATABASE aviorspare;
SQL

echo "seed: loading governed fixtures"
psql "$AVIOR_FIXTURE_DSN" -v ON_ERROR_STOP=1 -f ./postgres-fixture.sql

# FEAT-009. The writer is a cluster role, created here the way bootstrap.sql
# creates it in a real deployment: once, and left alone after that.
echo "seed: creating the writer role"
psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'CREATE ROLE avior_writer LOGIN PASSWORD ''avior_writer_local'''
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'avior_writer')
\gexec
SQL

# FEAT-076. The capture table and the writer's grants, from the file generated
# out of the list Avior itself runs. The fixture has no owner role: the seed's
# own role creates its tables, so it is the one the default privileges are for.
echo "seed: adding write support to the governed fixture"
psql "$AVIOR_FIXTURE_DSN" -v ON_ERROR_STOP=1 -v owner=avior -f ./governed/write-support.sql

echo "seed: loading the deployment target"
psql "$AVIOR_DEPLOY_SEED_DSN" -v ON_ERROR_STOP=1 -f ./postgres-deploy-target.sql

# FEAT-070. A database with enough lineage to show the page: local development
# only. The acceptance overlay leaves AVIOR_LINEAGE_DEMO_DSN empty, because the
# suite builds the shapes it reads and asserts on what the databases hold.
if [ -n "${AVIOR_LINEAGE_DEMO_DSN:-}" ]; then
  echo "seed: loading the lineage demo"
  psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 -c 'CREATE DATABASE aviorlineage'
  psql "$AVIOR_LINEAGE_DEMO_DSN" -v ON_ERROR_STOP=1 -f ./postgres-lineage-demo.sql
  psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO database_grant (id, role, database, reason)
VALUES
    ('00000000-0000-0000-0000-0000000000c1', 'developer', 'aviorlineage', 'the lineage demo'),
    ('00000000-0000-0000-0000-0000000000c2', 'dba', 'aviorlineage', 'the lineage demo'),
    ('00000000-0000-0000-0000-0000000000c3', 'administrator', 'aviorlineage', 'the lineage demo')
ON CONFLICT (role, database) DO NOTHING;
SQL
fi

# FEAT-071. Sixty days of observatory history and thirty days of
# submissions, so every tab and the overview have something to show. Needs the lineage demo database; the
# acceptance overlay leaves both variables empty, because the suite owns the
# rows it asserts on.
if [ -n "${AVIOR_OBSERVATORY_DEMO:-}" ] && [ -n "${AVIOR_LINEAGE_DEMO_DSN:-}" ]; then
  echo "seed: loading the observatory demo"
  psql "$AVIOR_LINEAGE_DEMO_DSN" -v ON_ERROR_STOP=1 -f ./postgres-observatory-demo.sql
  psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 -f ./store-observatory-demo.sql
  psql "$AVIOR_LINEAGE_DEMO_DSN" -v ON_ERROR_STOP=1 -At -f ./observatory-demo-objects.sql \
    | psql "$AVIOR_STORE_DSN" -v ON_ERROR_STOP=1 -q
  # FEAT-072. Operator statements through the governed role, last, so the
  # Performance tab ranks something after a statistics reset.
  echo "seed: running the observatory demo workload"
  psql "$AVIOR_OBSERVATORY_WORKLOAD_DSN" -v ON_ERROR_STOP=1 -q -o /dev/null -f ./observatory-demo-workload.sql
fi

echo "seed: done"
