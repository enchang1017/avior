-- The governed fixture database. Applied to the same PostgreSQL server that
-- holds aviorDB, as a separate database, because that is the production shape:
-- one server per environment, one database per service, aviorDB alongside them.
--
-- Every object here exists to exercise a named acceptance criterion, and where
-- one does the comment says which. The cases are chosen for this engine's edges:
-- what is worth pinning is what a reader of the catalogue can get wrong.

\set ON_ERROR_STOP on

CREATE TABLE public.member (
    member_id       integer PRIMARY KEY,
    login_name      text        NOT NULL,
    currency_code   char(3)     NOT NULL,
    -- Denied to the developer role by the aviorDB seed. FEAT-002 AC-4 reads it
    -- through a view to prove the plan, not the text, decides which columns a
    -- statement touched.
    id_card_number  text        NOT NULL,
    register_time   timestamptz NOT NULL
);

CREATE TABLE public.bet (
    bet_id    integer PRIMARY KEY,
    member_id integer        NOT NULL REFERENCES public.member (member_id),
    stake     numeric(18, 4) NOT NULL,
    odds      numeric(9, 3)  NOT NULL,
    win_lost  numeric(18, 4),
    status    text           NOT NULL
);

-- A partial index carrying an included column. Both are reproduced in
-- FEAT-023's index script, and both are the parts a naive CREATE INDEX misses.
CREATE INDEX ix_bet_member_id ON public.bet (member_id ASC, stake DESC)
    INCLUDE (win_lost)
    WHERE status = 'Open';

-- A heap with no index of any kind. FEAT-023 AC-8: a table with no indexes has
-- to say so, because an empty answer and no answer read the same on screen.
-- Names that cannot be written bare.
--
-- The generator quotes an identifier only when leaving it off would read
-- something else, and the rule for when that is has three readers to satisfy:
-- PostgreSQL's folding, PostgreSQL's reserved words, and Avior's own parser.
-- Without a table like this the rule is checked against a list rather than
-- against a database, which is checking it against the thing that could be
-- wrong. FEAT-008 AC-4.
CREATE TABLE public.awkward (
    id     integer PRIMARY KEY,
    "order" integer NOT NULL,
    "MiXeD" text NOT NULL,
    "comment" text NOT NULL
);

INSERT INTO public.awkward (id, "order", "MiXeD", "comment")
VALUES (1, 7, 'Folded', 'a word the guard forbids');

CREATE TABLE public.audit_note (
    note       text,
    written_at timestamptz
);

-- Exposes id_card_number deliberately. Selecting it through the view resolves to
-- public.member.id_card_number — the denial's key — while the statement text
-- names neither the base table nor that key. That is the case the plan-is-
-- authoritative rule exists for, and it cannot be built from a view that hides
-- the column.
--
-- The pruning case is the other half: selecting only the harmless columns
-- through this same view is allowed, because the optimiser never resolves the
-- one it was not asked for. A view hides the base table's name, not its data.
CREATE VIEW public.member_view AS
    SELECT member_id, login_name, id_card_number, currency_code, register_time
    FROM public.member;

INSERT INTO public.member (member_id, login_name, currency_code, id_card_number, register_time) VALUES
    (1, 'alice', 'SGD', 'S1234567A', '2026-01-05 09:14:00+00'),
    (2, 'bob',   'MYR', 'M7654321B', '2026-02-11 22:03:00+00'),
    (3, 'chen',  'SGD', 'S7654321C', '2026-03-30 04:41:00+00');

INSERT INTO public.bet (bet_id, member_id, stake, odds, win_lost, status) VALUES
    (10, 1, 25.0000, 1.850, 21.2500, 'Settled'),
    (11, 2, 10.5000, 2.400, NULL,    'Open'),
    (12, 1,  5.0000, 3.100, -5.0000, 'Settled');

-- A composite type. It describes one row, not a set, and carries no nullability,
-- default, identity or key — there is nothing in the catalogue to read those
-- from. FEAT-023 reads it as a type definition.
CREATE TYPE public.bet_import AS (
    bet_id    integer,
    member_id integer,
    stake     numeric(18, 4),
    note      text
);

-- A domain. A user-defined type that is not a composite one, and one that does
-- render — so it proves AC-10 narrows to types with no script at all rather than
-- to everything that is not composite.
CREATE DOMAIN public.account_number AS varchar(32) NOT NULL;

-- An enum. A third kind of user-defined type, so the type reader cannot assume
-- every user type has attributes to list.
CREATE TYPE public.bet_status AS ENUM ('Open', 'Settled', 'Void');

-- A shell type. It is listed as a user-defined type and has no script to give,
-- because what defines it is C functions in a shared library. FEAT-023 AC-10:
-- it must be refused by name rather than answered with an empty definition.
--
-- Every other user type here renders, so without this object AC-10 would have no
-- case left. It is the narrowest thing that still exercises the rule.
CREATE TYPE public.opaque_handle;

CREATE FUNCTION public.fn_member_stake(p_member_id integer)
RETURNS numeric(18, 4)
LANGUAGE sql
STABLE
AS $$
    SELECT coalesce(sum(stake), 0) FROM public.bet WHERE member_id = p_member_id;
$$;

-- A set-returning function. prokind does not distinguish it from a scalar one,
-- so a reader filtering on what a routine returns would silently lose it.
CREATE FUNCTION public.tf_open_bets()
RETURNS TABLE (bet_id integer, member_id integer, stake numeric(18, 4))
LANGUAGE sql
STABLE
AS $$
    SELECT bet_id, member_id, stake FROM public.bet WHERE status = 'Open';
$$;

-- A procedure. PostgreSQL 11 and later separates these from functions by
-- prokind = 'p'; a procedure returns nothing and is invoked with CALL.
CREATE PROCEDURE public.sp_member_bets(p_member_id integer)
LANGUAGE sql
AS $$
    SELECT bet_id, stake, odds, win_lost FROM public.bet WHERE member_id = p_member_id;
$$;

-- An aggregate, prokind = 'a'. It exists so the function list has something it
-- must exclude: an aggregate has no body to read.
CREATE AGGREGATE public.total_stake(numeric) (
    SFUNC = numeric_add,
    STYPE = numeric,
    INITCOND = '0'
);

-- Granted to a named role, never to PUBLIC. FEAT-023 AC-5 excludes PUBLIC from
-- the permission list, so a grant to PUBLIC would prove nothing.
CREATE ROLE avior_reader;

-- Comments, because a table's description is one of the four things FEAT-054
-- shows and a fixture without any would leave that section untested against a
-- real catalogue.
COMMENT ON TABLE public.member IS 'One row per registered player.';
COMMENT ON COLUMN public.member.id_card_number IS
    'National identity number. Denied to developers; see column_denial.';
COMMENT ON COLUMN public.member.register_time IS 'When the account was opened.';

GRANT USAGE ON SCHEMA public TO avior_reader;
GRANT SELECT ON public.member_view TO avior_reader;
GRANT EXECUTE ON FUNCTION public.fn_member_stake(integer) TO avior_reader;
GRANT EXECUTE ON PROCEDURE public.sp_member_bets(integer) TO avior_reader;
GRANT USAGE ON TYPE public.bet_import TO avior_reader;

-- The role Avior connects to a governed database as. It reads and nothing else:
-- the guard refuses writes, and this is the second lock on the same door, at the
-- engine rather than in the parser.
CREATE ROLE avior_governed LOGIN PASSWORD 'avior_governed_local';
GRANT CONNECT ON DATABASE aviorfixture TO avior_governed;
GRANT USAGE ON SCHEMA public TO avior_governed;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO avior_governed;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO avior_governed;

-- The role that changes a governed database.
--
-- Separate from avior_governed on purpose, and the separation is the feature.
-- avior_governed holds SELECT and EXECUTE: it is what an operator's statement
-- runs under, and it cannot issue DDL however the statement is written. This
-- role can, and nothing but the release pipeline reaches it. FEAT-017.
CREATE ROLE avior_deployer LOGIN PASSWORD 'avior_deployer_local';
GRANT CONNECT ON DATABASE aviorfixture TO avior_deployer;
GRANT USAGE, CREATE ON SCHEMA public TO avior_deployer;

-- The role that owns what a governed database holds.
--
-- ALTER TABLE is not a privilege in PostgreSQL, it is a property of ownership,
-- so CREATE on the schema lets the deployer add tables and leaves it unable to
-- change any that already exist — which is the one thing a release usually
-- does. Granting ownership of every table to the login role would answer that,
-- and is the wrong answer: a service's tables belong to the service, not to
-- whoever deploys them.
--
-- So the objects are owned by a role that cannot log in, and the deployer is a
-- member of it. PostgreSQL's ownership checks follow role membership, so the
-- ALTER succeeds while the membership can be revoked without re-owning
-- anything. That is the shape a real governed database has to be set up in,
-- and this fixture would be lying if it used a superuser instead.
--
-- Named for the database it owns, which is the estate's convention and what
-- governed.OwnerRole derives: one owner per database, so revoking one
-- membership takes one database out of the deploy path. A name shared across
-- databases would have made that a cluster-wide decision.
CREATE ROLE aviordeploy_schema_owner NOLOGIN;
GRANT aviordeploy_schema_owner TO avior_deployer;

-- FEAT-059. The fourth credential, and the only one holding the two
-- cluster-level privileges nothing else here has.
--
-- CREATEDB and CREATEROLE, and deliberately not SUPERUSER: the feature creates
-- databases and group roles and must not be able to do anything else, and a
-- fixture that used the container's superuser would be unable to show the
-- difference. CREATEROLE also carries the right to grant the roles it creates,
-- which is what lets it put the deployer into a new database's owner role.
CREATE ROLE avior_provisioner LOGIN CREATEDB CREATEROLE PASSWORD 'avior_provisioner_local';
