-- The database releases are applied to.
--
-- Not aviorfixture: the read tests assert against that one's exact shape, and a
-- migration landing in it would make those tests depend on the order the suite
-- happened to run in.
CREATE SCHEMA IF NOT EXISTS public;

CREATE TABLE public.ledger_probe (
    id         integer PRIMARY KEY,
    note       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_ledger_probe_created ON public.ledger_probe (created_at DESC);

INSERT INTO public.ledger_probe (id, note) VALUES (1, 'seeded');

-- The ledger a real target already carries. Avior creates it when it is absent,
-- but a target that has ever been deployed to has one, and the fixture should
-- look like the thing it stands in for.
CREATE TABLE public.schema_migrations (
    version    text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now(),
    checksum   text NOT NULL DEFAULT ''
);

GRANT CONNECT ON DATABASE aviordeploy TO avior_deployer;
GRANT USAGE, CREATE ON SCHEMA public TO avior_deployer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO avior_deployer;

-- Ownership, not privileges, is what ALTER TABLE needs. These tables stand in
-- for a service's own, so they are owned by a role the deployer belongs to
-- rather than by the deployer itself.
ALTER SCHEMA public OWNER TO aviordeploy_schema_owner;
ALTER TABLE public.ledger_probe OWNER TO aviordeploy_schema_owner;
ALTER TABLE public.schema_migrations OWNER TO aviordeploy_schema_owner;
ALTER INDEX ix_ledger_probe_created OWNER TO aviordeploy_schema_owner;

-- A table a migration creates is owned by whoever ran the migration, and the
-- query path is a different role that was granted SELECT on the tables that
-- existed when this ran. Without this, the first table a release adds is
-- invisible to every operator until somebody notices and grants it by hand.
GRANT CONNECT ON DATABASE aviordeploy TO avior_governed;
-- FEAT-058's spare: reachable by the credential, granted to no role, so the
-- gate being tested is Avior's rather than PostgreSQL's.
GRANT CONNECT ON DATABASE aviorspare TO avior_governed;
GRANT USAGE ON SCHEMA public TO avior_governed;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO avior_governed;
ALTER DEFAULT PRIVILEGES FOR ROLE avior_deployer IN SCHEMA public
    GRANT SELECT ON TABLES TO avior_governed;
ALTER DEFAULT PRIVILEGES FOR ROLE aviordeploy_schema_owner IN SCHEMA public
    GRANT SELECT ON TABLES TO avior_governed;
