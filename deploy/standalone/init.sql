-- Fixed names intentionally match the store exclusion in every query path.
-- Re-running this command preserves existing roles and their passwords.
\set ON_ERROR_STOP on
\getenv store_password AVIOR_STORE_PASSWORD
\getenv reader_password AVIOR_TARGET_PASSWORD
DO $$
BEGIN
 IF EXISTS (SELECT FROM pg_database d JOIN pg_roles r ON r.oid = d.datdba
            WHERE d.datname = 'avior' AND r.rolname <> 'avior_local_store') THEN
  RAISE EXCEPTION 'Existing avior database has another owner. Configure its existing credentials instead of running init.';
 END IF;
 IF EXISTS (SELECT FROM pg_roles WHERE rolname IN ('avior_local_store', 'avior_local_reader')
            AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolbypassrls)) THEN
  RAISE EXCEPTION 'Existing local role has elevated privileges; use dedicated unprivileged roles.';
 END IF;
END $$;
SELECT format('CREATE ROLE avior_local_store LOGIN PASSWORD %L', :'store_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'avior_local_store') \gexec
SELECT format('CREATE ROLE avior_local_reader LOGIN PASSWORD %L', :'reader_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'avior_local_reader') \gexec
SELECT 'CREATE DATABASE avior OWNER avior_local_store'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'avior') \gexec
REVOKE CONNECT ON DATABASE avior FROM PUBLIC;
-- pg_read_all_data grants USAGE/SELECT across user schemas, including new tables.
-- It does not grant CONNECT to the store after the PUBLIC grant is removed.
GRANT pg_read_all_data TO avior_local_reader;
ALTER ROLE avior_local_reader SET default_transaction_read_only = on;
