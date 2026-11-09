-- What a PostgreSQL server needs before Avior can be pointed at it.
--
-- Run this ONCE per environment, by hand, as a superuser, against the
-- maintenance database:
--
--   psql "postgres://postgres@<host>:5432/postgres" -f deploy/store/bootstrap.sql
--
-- It prompts for a password rather than taking one as a -v argument. A value
-- passed on the command line reaches the process table, the shell history and,
-- when this is run through `kubectl run`, the pod's container log -- which is
-- how a deployer credential in this estate has already been disclosed once.
--
-- IT ASKS ONLY FOR WHAT IS MISSING. Each role is checked first, and one that is
-- already there is left exactly as it is -- no prompt, no ALTER, no rotation.
-- So re-running this file on a live server is how a role added to it later
-- arrives, and re-typing the four passwords the secret store already holds is
-- not part of that.
--
-- The consequence is that this file does NOT rotate anything. Rotating is a
-- different act from bootstrapping and now looks like one:
--
--   ALTER ROLE avior_governed WITH PASSWORD '...';   -- then update the store
--
-- which is deliberate. Rotation as a side effect of re-running a setup script
-- is how a password in the secret store stops matching the server without
-- anybody deciding to change it.
--
-- CI does not run this, and that is deliberate. The credentials it creates own
-- Avior's store and read every governed database; bringing a server under Avior
-- is somebody deciding to, not something that happens because a pipeline ran.

\set ON_ERROR_STOP on

-- Every role below follows the same shape: ask the server whether it is there,
-- and prompt only if it is not. \prompt is a client-side command and cannot be
-- guarded by SQL, so the guard is psql's own \if -- which does skip meta
-- commands in a branch it does not take, and is what makes "only what is
-- missing" possible in one file rather than two.

-- The store owner. It owns aviordb and is the only credential in the estate
-- that can change the shape of anything in it: the migration Job connects as
-- this role, and the gateway deliberately does not.
--
-- No SUPERUSER and no CREATEDB. It owns one database, which is created below by
-- the superuser running this file, so it never needs to make another.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'avior') AS missing_avior \gset
\if :missing_avior
\prompt 'password for avior (owns aviordb): ' password
SELECT format('CREATE ROLE avior LOGIN PASSWORD %L', :'password')
\gexec
\unset password
\else
\echo 'avior is already here, left untouched'
\endif

-- The credential an operator's read runs as. FEAT-002.
--
-- It holds SELECT and EXECUTE on the governed databases and nothing else -- no
-- DDL, no writes. Those grants are not here: this server holds no governed
-- database yet, and a grant naming one that does not exist cannot be written.
-- See the block at the end of this file for what to run per database as they
-- arrive.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'avior_governed') AS missing_avior_governed \gset
\if :missing_avior_governed
\prompt 'password for avior_governed (reads governed databases): ' password
SELECT format('CREATE ROLE avior_governed LOGIN PASSWORD %L', :'password')
\gexec
\unset password
\else
\echo 'avior_governed is already here, left untouched'
\endif

-- The credential an approved write runs as. FEAT-009.
--
-- Created here with no grants at all, which is deliberate and is not the same
-- as not creating it. Preparing a database for writes on the Provisioning page
-- (FEAT-076) is what gives this role reach, one governed database at a time,
-- and a freshly bootstrapped server has no database brought under the write
-- path -- so at that point it can log in and read nothing.
--
-- It exists anyway because the SecretProviderClass every environment shares
-- names AVIOR_WRITER_PASSWORD, and the CSI driver fails the whole mount when a
-- key it was told to read is absent: without this role and its password the pod
-- does not start with writes disabled, it does not start at all. The other way
-- round -- a password in the store naming a role that does not exist -- is worse
-- again, because nothing reports it until somebody tries to use it.
--
-- This is the only place the role is created. Avior prepares databases for it
-- and refuses to while it is missing, but never creates it or sets its
-- password: the password goes to OpenBao from here, and a second writer of it
-- would be a second place for it to stop matching.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'avior_writer') AS missing_avior_writer \gset
\if :missing_avior_writer
\prompt 'password for avior_writer (runs approved writes): ' password
SELECT format('CREATE ROLE avior_writer LOGIN PASSWORD %L', :'password')
\gexec
\unset password
\else
\echo 'avior_writer is already here, left untouched'
\endif

-- The credential a release is applied as. FEAT-017.
--
-- No attributes at all, and that is the whole design. It owns nothing and is
-- granted nothing directly: it reaches a governed database's objects by being a
-- member of that database's owner role, which FEAT-059 grants when it creates
-- the database. So one credential applies every database's releases while each
-- database can be taken back out of the release path by revoking one
-- membership, rather than by rotating a credential every other database shares.
--
-- It is not SUPERUSER and not CREATEDB for the same reason: a migration that
-- needed either is a migration doing something a release should not.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'avior_deployer') AS missing_avior_deployer \gset
\if :missing_avior_deployer
\prompt 'password for avior_deployer (applies releases): ' password
SELECT format('CREATE ROLE avior_deployer LOGIN PASSWORD %L', :'password')
\gexec
\unset password
\else
\echo 'avior_deployer is already here, left untouched'
\endif

-- The credential that creates governed databases and their roles. FEAT-059.
--
-- CREATEDB and CREATEROLE, and deliberately NOT SUPERUSER. Those two are what
-- the feature does and the whole of what it may do; a superuser here would be a
-- credential in the gateway's process able to read and change every database on
-- the server, to satisfy a page whose entire vocabulary is "create a database"
-- and "create a role".
--
-- The difference is not theoretical. PostgreSQL 16 stopped giving a CREATEROLE
-- role SET over the roles it creates, so this credential is refused when it
-- names an owner role it has just created and has to take SET explicitly --
-- a refusal a superuser would never have met, and which therefore reached
-- production-shaped code only because this role is not one.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'avior_provisioner') AS missing_avior_provisioner \gset
\if :missing_avior_provisioner
\prompt 'password for avior_provisioner (creates databases and roles): ' password
SELECT format('CREATE ROLE avior_provisioner LOGIN CREATEDB CREATEROLE PASSWORD %L', :'password')
\gexec
\unset password
\else
\echo 'avior_provisioner is already here, left untouched'
\endif

-- Avior's own store.
--
-- CREATE DATABASE cannot run inside a transaction block, which is why this is
-- \gexec rather than a DO block like the grants below.
SELECT 'CREATE DATABASE aviordb OWNER avior'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'aviordb')
\gexec

-- aviordb is never a governed database, and this is what makes that true rather
-- than a convention.
--
-- PostgreSQL grants CONNECT on a new database to PUBLIC, so without this every
-- role on the server -- avior_governed included -- could open Avior's own store
-- and read the audit trail, the accounts and the session table. The gateway
-- would still refuse to route a query there, but the credential would not.
REVOKE CONNECT ON DATABASE aviordb FROM PUBLIC;
GRANT CONNECT ON DATABASE aviordb TO avior;

-- FEAT-071. The observatory reads the server-wide statistics as the store
-- role: pg_stat_activity's text, pg_stat_replication, pg_stat_statements for
-- every database. pg_read_all_stats is what lets it see other roles' rows.
GRANT pg_read_all_stats TO avior;

-- pg_stat_statements lives in aviordb and in no governed database. The server
-- must also carry it in shared_preload_libraries (a restart), with
-- pg_stat_statements.track_utility = off and track_functions = 'pl'; see
-- FEAT-071's production setup.
\connect aviordb
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
\connect postgres

-- What this file did, so the person running it can see it rather than trust it.
\echo ''
\echo '=== roles ==='
SELECT rolname, rolcanlogin, rolsuper, rolcreatedb
FROM pg_roles
WHERE rolname IN ('avior', 'avior_governed', 'avior_writer', 'avior_deployer',
                  'avior_provisioner')
ORDER BY rolname;

\echo '=== databases ==='
SELECT datname, pg_get_userbyid(datdba) AS owner, datacl
FROM pg_database WHERE datname = 'aviordb';

\echo ''
\echo 'Next: apply the migrations (the avior-migrate Job, or goose run as avior).'
\echo 'A governed database is brought under Avior one at a time -- see the'
\echo 'comment at the foot of this file.'

-- ---------------------------------------------------------------------------
-- Bringing a governed database under Avior
-- ---------------------------------------------------------------------------
--
-- Not part of this file, because it is a different decision made at a different
-- time: this file provisions the server, and each of these is somebody saying
-- that operators may read one more database. Run against that database, as a
-- role that owns its schema:
--
--   GRANT CONNECT ON DATABASE <db> TO avior_governed;
--   GRANT USAGE ON SCHEMA public TO avior_governed;
--   GRANT SELECT ON ALL TABLES IN SCHEMA public TO avior_governed;
--   GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO avior_governed;
--   ALTER DEFAULT PRIVILEGES IN SCHEMA public
--       GRANT SELECT ON TABLES TO avior_governed;
--
-- The ALTER DEFAULT PRIVILEGES is what stops a table added later from being
-- invisible until somebody notices. It applies only to tables created by the
-- role that runs it, so it is run as whichever role owns that schema.
--
-- Writes are a further step again: preparing a database for writes (FEAT-076)
-- creates the backup schema a write captures into and grants avior_writer.
