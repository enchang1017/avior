#!/usr/bin/env bash
#
# Proves the two lanes describe the same database. ADR-0004's replay gate.
#
# A database project reaches a database two ways, deliberately:
#
#   staging      the declarative files are applied directly; the end state is right and
#                nothing is recorded
#   production   migrations/ is applied in order through Avior and recorded; the path is
#                right
#
# Two descriptions of one thing drift. This makes the drift impossible to merge:
#
#   A: an empty database, then apply-schema.sh
#   B: an empty database, then every migration in order
#   assert digest(A) == digest(B), and the same for what the digest cannot see
#
# The digest is Avior's own schemadigest, run from the checkout this kit lives in. Two
# implementations of a hash are two things to keep in step, and the drift check Avior
# runs before a production deployment is meaningless the moment they disagree.
#
# Usage, from the database project's root:
#
#   <avior>/tools/release-kit/verify-migrations.sh
set -euo pipefail

KIT_DIR="$(cd "$(dirname "$0")" && pwd)"
AVIOR_REPO="${AVIOR_REPO:-$KIT_DIR/../..}"
cd "${PROJECT_DIR:-$PWD}"
# shellcheck source=kit-config.sh
. "$KIT_DIR/kit-config.sh"
# shellcheck source=throwaway-pg.sh
. "$KIT_DIR/throwaway-pg.sh"

if [ ! -d "$AVIOR_REPO/backend/cmd/schemadigest" ]; then
    echo "ERROR: no schemadigest at $AVIOR_REPO/backend/cmd/schemadigest." >&2
    echo "       Set AVIOR_REPO to a checkout of the avior repository." >&2
    exit 1
fi

pg_start
pg_create_roles
pg_psql -d postgres -c "CREATE DATABASE lane_a" >/dev/null
pg_psql -d postgres -c "CREATE DATABASE lane_b" >/dev/null

# PGHOST and PGPORT are deliberately not set. apply-schema.sh forwards them into the
# container, where an address reachable from here is not reachable from there.
echo "==> lane A: apply-schema.sh"
PGUSER=postgres PGDATABASE=lane_a PG_CONTAINER="$PG_NAME" "$KIT_DIR/apply-schema.sh" >/dev/null

echo "==> lane B: $MIGRATIONS_DIR/ in order"
for file in "$MIGRATIONS_DIR"/*.sql; do
    [ -e "$file" ] || continue
    echo "    $file"
    pg_psql -d lane_b -f - < "$file" >/dev/null
done

echo "==> comparing"
digest() { (cd "$AVIOR_REPO/backend" && go run ./cmd/schemadigest "$(pg_host_dsn "$1")"); }

A="$(digest lane_a)"
B="$(digest lane_b)"

if [ "$A" != "$B" ]; then
    echo >&2
    echo "FAIL  the two lanes describe different databases." >&2
    echo "      apply-schema.sh  $A" >&2
    echo "      $MIGRATIONS_DIR/      $B" >&2
    echo >&2
    echo "      Something was added to one and not the other. A new function or column" >&2
    echo "      needs a migration as well as its declarative file: run generate-migration.sh." >&2
    exit 1
fi

# Avior's digest is deliberately blind to privileges -- it hashes columns, constraints,
# indexes, routine bodies and types, and a GRANT is none of those. So a GRANT present in
# one lane and missing from the other passes the check above in silence, and is
# compared here instead.
#
# relacl is NULL until the first GRANT or REVOKE touches an object, so NULL falls back
# to acldefault() for that object's kind rather than being skipped: two lanes that both
# grant nothing compare equal because they are, not because both read NULL. A sequence
# has its own default (rwU), distinct from a table's.
echo "==> comparing privileges"
privileges_query=$(cat <<'SQL'
WITH table_grants AS (
    SELECT
        n.nspname || '.' || c.relname || ' ' ||
        (CASE c.relkind
            WHEN 'S' THEN 'SEQUENCE'
            WHEN 'v' THEN 'VIEW'
            WHEN 'm' THEN 'MATERIALIZED VIEW'
            ELSE 'TABLE'
        END) || ' GRANT ' ||
        (CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE acl.grantee::regrole::text END) ||
        ' ' || acl.privilege_type AS line
    FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    CROSS JOIN LATERAL aclexplode(coalesce(
        c.relacl,
        CASE WHEN c.relkind = 'S' THEN acldefault('s', c.relowner)
             ELSE acldefault('r', c.relowner)
        END
    )) AS acl
    WHERE c.relkind IN ('r', 'p', 'S', 'v', 'm')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND c.relname <> 'schema_migrations'
),
schema_grants AS (
    SELECT
        n.nspname || ' SCHEMA GRANT ' ||
        (CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE acl.grantee::regrole::text END) ||
        ' ' || acl.privilege_type AS line
    FROM pg_catalog.pg_namespace n
    CROSS JOIN LATERAL aclexplode(coalesce(n.nspacl, acldefault('n', n.nspowner))) AS acl
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
),
function_grants AS (
    SELECT
        n.nspname || '.' || p.proname || '(' ||
        pg_catalog.pg_get_function_identity_arguments(p.oid) || ')' ||
        ' FUNCTION GRANT ' ||
        (CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE acl.grantee::regrole::text END) ||
        ' ' || acl.privilege_type AS line
    FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    CROSS JOIN LATERAL aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS acl
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
),
function_security AS (
    SELECT
        n.nspname || '.' || p.proname || '(' ||
        pg_catalog.pg_get_function_identity_arguments(p.oid) || ')' ||
        ' SECURITY DEFINER=' || p.prosecdef::text ||
        ' OWNER=' || p.proowner::regrole::text AS line
    FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
)
SELECT line FROM (
    SELECT line FROM table_grants
    UNION ALL SELECT line FROM schema_grants
    UNION ALL SELECT line FROM function_grants
    UNION ALL SELECT line FROM function_security
) all_lines
ORDER BY line;
SQL
)

privileges() { pg_psql -d "$1" -tAq -c "$privileges_query"; }

PRIV_A="$(privileges lane_a)"
PRIV_B="$(privileges lane_b)"

if [ "$PRIV_A" != "$PRIV_B" ]; then
    # `|| true` on diff itself, not on the block that prints it: diff exits 1 whenever it
    # finds a difference, which is every time this branch runs, and under pipefail that
    # would end the script before the explanation below is printed.
    diff_output="$(diff <(echo "$PRIV_A") <(echo "$PRIV_B") || true)"

    echo >&2
    echo "FAIL  the two lanes grant different privileges." >&2
    echo "      < apply-schema.sh   > $MIGRATIONS_DIR/" >&2
    echo >&2
    echo "$diff_output" | sed 's/^/      /' >&2
    echo >&2
    echo "      A GRANT or REVOKE is missing from one lane. A $SECURITY_DIR/ file reaches" >&2
    echo "      $MIGRATIONS_DIR/ only once, the first time generate-migration.sh copies it, so a" >&2
    echo "      table added after that needs its grants in a $SECURITY_DIR/ file of its own." >&2
    exit 1
fi

# What the digest cannot see beyond privileges: a partition's bound, storage parameters,
# and an enum's values. CREATE TABLE IF NOT EXISTS ... PARTITION OF swallows a wrong
# bound as a NOTICE when the partition exists, so comparison is the only check a bound
# gets.
#
# An enum value's position is row_number() over enumsortorder rather than enumsortorder
# itself: ALTER TYPE ... ADD VALUE BEFORE leaves a fraction there, so a migrated type and
# a freshly created one can differ in the number while agreeing in the order, which is
# the property that matters.
echo "==> comparing partitions, storage parameters and enum values"
catalog_query=$(cat <<'SQL'
SELECT line FROM (
    SELECT 'PARTITIONED ' || n.nspname || '.' || c.relname || ' ' || pg_catalog.pg_get_partkeydef(c.oid) AS line
    FROM pg_catalog.pg_partitioned_table pt
    JOIN pg_catalog.pg_class c ON c.oid = pt.partrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind = 'p'
    UNION ALL
    SELECT 'PARTITION ' || pn.nspname || '.' || p.relname || ' ' || c.relname || ' ' ||
           pg_catalog.pg_get_expr(c.relpartbound, c.oid)
    FROM pg_catalog.pg_inherits i
    JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
    JOIN pg_catalog.pg_class p ON p.oid = i.inhparent
    JOIN pg_catalog.pg_namespace pn ON pn.oid = p.relnamespace
    WHERE c.relispartition AND c.relkind IN ('r', 'p')
    UNION ALL
    SELECT 'RELOPTIONS ' || n.nspname || '.' || c.relname || ' ' || array_to_string(c.reloptions, ',')
    FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE c.reloptions IS NOT NULL
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
    UNION ALL
    SELECT 'ENUM ' || n.nspname || '.' || t.typname || ' ' ||
           row_number() OVER (PARTITION BY t.oid ORDER BY e.enumsortorder) || ' ' || e.enumlabel
    FROM pg_catalog.pg_enum e
    JOIN pg_catalog.pg_type t ON t.oid = e.enumtypid
    JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
) all_lines
ORDER BY line;
SQL
)

catalog() { pg_psql -d "$1" -tAq -c "$catalog_query"; }

CAT_A="$(catalog lane_a)"
CAT_B="$(catalog lane_b)"

if [ "$CAT_A" != "$CAT_B" ]; then
    diff_output="$(diff <(echo "$CAT_A") <(echo "$CAT_B") || true)"
    echo >&2
    echo "FAIL  the two lanes differ in what the digest cannot see." >&2
    echo "      < apply-schema.sh   > $MIGRATIONS_DIR/" >&2
    echo >&2
    echo "$diff_output" | sed 's/^/      /' >&2
    echo >&2
    echo "      A partition, its bound or its storage parameters, or an enum's values, differ" >&2
    echo "      between the lanes. A new $PARTITIONS_DIR/ file needs its migration (generate-migration.sh" >&2
    echo "      copies it); an enum value is ALTER TYPE ... ADD VALUE, in a migration of its own." >&2
    exit 1
fi
echo "    $(printf '%s\n' "$CAT_A" | grep -c '^PARTITION ' || true) partitions, $(printf '%s\n' "$CAT_A" | grep -c '^ENUM ' || true) enum values, $(printf '%s\n' "$CAT_A" | grep -c '^RELOPTIONS ' || true) relations with storage parameters -- equal"

echo
echo "PASS  both lanes build the same database ($A) and grant the same privileges, partitions and enum values"
