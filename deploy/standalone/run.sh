#!/bin/sh
set -eu

fail() { echo "avior: $*" >&2; exit 1; }

# Development setup must be selected explicitly, never inferred from the binary's default.
command="${1:-serve}"
if [ "$command" = init ]; then
    [ "${AVIOR_ENV:-}" = development ] || fail "init requires AVIOR_ENV=development"
    : "${AVIOR_BOOTSTRAP_DSN:?Set the PostgreSQL administrator DSN for this one-time command}"
    : "${AVIOR_STORE_PASSWORD:?Set the new store role password}"
    : "${AVIOR_TARGET_PASSWORD:?Set the new reader role password}"
    exec psql "$AVIOR_BOOTSTRAP_DSN" -X -f /standalone/init.sql
fi
[ "$command" = serve ] || fail "Expected serve or init"
[ -z "${AVIOR_BOOTSTRAP_DSN:-}" ] || fail "Remove AVIOR_BOOTSTRAP_DSN after init; serve does not use administrator credentials"

if [ "${AVIOR_ENV:-}" = development ]; then
    : "${AVIOR_STORE_DSN:?Set AVIOR_STORE_DSN to the separate avior database}"
    : "${AVIOR_TARGET_DSN:?Set AVIOR_TARGET_DSN to the database to browse}"
    store_password="${AVIOR_STORE_PASSWORD:-}"
    target_password="${AVIOR_TARGET_PASSWORD:-}"
    if [ -n "${AVIOR_STORE_PASSWORD_FILE:-}" ]; then
        [ -z "$store_password" ] || fail "Name AVIOR_STORE_PASSWORD or its file, not both"
        store_password="$(tr -d '\r\n' < "$AVIOR_STORE_PASSWORD_FILE")"
    fi
    if [ -n "${AVIOR_TARGET_PASSWORD_FILE:-}" ]; then
        [ -z "$target_password" ] || fail "Name AVIOR_TARGET_PASSWORD or its file, not both"
        target_password="$(tr -d '\r\n' < "$AVIOR_TARGET_PASSWORD_FILE")"
    fi
    # Check both DSNs before applying DDL so swapping the two cannot migrate
    # a governed database by accident.
    store_db="$(PGPASSWORD="$store_password" psql "$AVIOR_STORE_DSN" -XAtc 'SELECT current_database()')"
    case "$store_db" in avior|aviordb) ;; *) fail "The development store must be named avior or aviordb" ;; esac
    target_db="$(PGPASSWORD="$target_password" psql "$AVIOR_TARGET_DSN" -XAtc 'SELECT current_database()')"
    case "$target_db" in avior|aviordb|postgres|template0|template1) fail "AVIOR_TARGET_DSN must name a governed user database" ;; esac
    PGPASSWORD="$store_password" goose -dir /migrations postgres "$AVIOR_STORE_DSN" up
    if [ "${AVIOR_AUTH_MODE:-}" = trusted-local ]; then
        # This is deployment seeding, not a grant made by an operator.
        PGPASSWORD="$store_password" psql "$AVIOR_STORE_DSN" -X -v ON_ERROR_STOP=1 -v target_db="$target_db" <<'SQL'
INSERT INTO database_grant (id, role, database, reason)
VALUES (gen_random_uuid(), 'developer', :'target_db', 'Standalone development configuration')
ON CONFLICT (role, database) DO NOTHING;
SQL
        export AVIOR_LOCAL_ACCOUNTS_FILE="${AVIOR_LOCAL_ACCOUNTS_FILE:-/standalone/accounts.yaml}"
    fi
fi
exec /usr/local/bin/avior
