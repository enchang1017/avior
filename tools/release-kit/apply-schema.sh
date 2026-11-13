#!/usr/bin/env bash
#
# Applies the whole declarative schema to a PostgreSQL database.
#
# This is how the kit materialises "desired": generate-migration.sh and
# verify-migrations.sh both build their desired database with it, so the thing that
# reads the DDL is PostgreSQL itself. It is also how a developer's local database and a
# CI database get their schema. It is not how production gets anything -- production
# receives migrations/ through Avior.
#
# Run from the database project's root, or with PROJECT_DIR pointing at it. The order
# of every folder comes from release-kit.conf.
#
# CONNECTION
#
# Ordinary libpq environment variables: PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD,
# PGSSLMODE. PGDATABASE defaults to release-kit.conf's DATABASE and PGUSER to postgres;
# the rest are forwarded only when set, so an unset PGHOST keeps psql on its local
# socket -- which is what makes the container mode below work with no password.
#
# HOW psql IS REACHED
#
#   PG_CONTAINER unset  -- psql from PATH if installed, otherwise LOCAL_PG_CONTAINER
#                          from release-kit.conf
#   PG_CONTAINER=""     -- force psql from PATH, and fail loudly if there is none
#   PG_CONTAINER=name   -- force `docker exec` into that container
#
# KNOWN LIMITATION
#
# The table files are plain CREATE TABLE, so this bootstraps a fresh database rather
# than re-syncing an existing one: against a database that already has the tables it
# aborts on the first "already exists". Do NOT paper over that with IF NOT EXISTS or
# DROP ... CASCADE -- re-syncing a live database is the staging lane's job, and it
# computes the ALTER rather than replaying CREATE.
set -euo pipefail

KIT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${PROJECT_DIR:-$PWD}"
# shellcheck source=kit-config.sh
. "$KIT_DIR/kit-config.sh"

# Guards against a file being added to a folder and silently never applied, which would
# leave every local database and every CI run quietly incomplete.
#
# Every folder is checked BEFORE anything is applied, and before a connection is even
# looked for: an unlisted file is a fact about the project, and it should be reported
# on a machine with no database as readily as on one with.
check_folder() {
    local folder="$1"; shift
    local path name known found
    for path in "$folder"/*.sql; do
        [ -e "$path" ] || continue
        name="$(basename "$path" .sql)"
        found=""
        for known in "$@"; do
            if [ "$known" = "$name" ]; then found=1; fi
        done
        if [ -z "$found" ]; then
            echo "ERROR: $folder/$name.sql is not listed for $folder in $KIT_CONFIG." >&2
            echo "       Add it in dependency order, then re-run." >&2
            exit 1
        fi
    done
}

check_folder "$SECURITY_DIR" ${SECURITY_ORDER[@]+"${SECURITY_ORDER[@]}"} \
    ${SECURITY_AFTER_TABLES_ORDER[@]+"${SECURITY_AFTER_TABLES_ORDER[@]}"}
check_folder "$TABLES_DIR" "${TABLES_ORDER[@]}"
check_folder "$DATA_DIR" ${DATA_ORDER[@]+"${DATA_ORDER[@]}"}
check_folder "$PROCEDURES_DIR" ${PROCEDURES_ORDER[@]+"${PROCEDURES_ORDER[@]}"}
check_folder "$PARTITIONS_DIR" ${PARTITIONS_ORDER[@]+"${PARTITIONS_ORDER[@]}"}

TARGET_DATABASE="${PGDATABASE:-$DATABASE}"
USERNAME="${PGUSER:-postgres}"

# Distinguishes "unset" (auto-detect) from "set to empty" (force PATH), which is why
# this is not the usual ${PG_CONTAINER:-...}.
if [ -z "${PG_CONTAINER+set}" ]; then
    if command -v psql >/dev/null 2>&1; then
        PG_CONTAINER=""
    elif [ -n "${LOCAL_PG_CONTAINER:-}" ]; then
        PG_CONTAINER="$LOCAL_PG_CONTAINER"
    else
        echo "ERROR: no psql on PATH and no LOCAL_PG_CONTAINER in $KIT_CONFIG." >&2
        echo "       Install a psql client, or name a running PostgreSQL container." >&2
        exit 1
    fi
fi

PSQL_ENV=("PGDATABASE=$TARGET_DATABASE" "PGUSER=$USERNAME")
for var in PGHOST PGPORT PGPASSWORD PGSSLMODE; do
    if [ -n "${!var:-}" ]; then
        PSQL_ENV+=("$var=${!var}")
    fi
done

if [ -n "$PG_CONTAINER" ]; then
    command -v docker >/dev/null 2>&1 || {
        echo "ERROR: PG_CONTAINER=$PG_CONTAINER but docker is not installed." >&2
        echo "       Set PG_CONTAINER= (empty) and install a psql client instead." >&2
        exit 1
    }
    TARGET="container $PG_CONTAINER, database $TARGET_DATABASE"
else
    command -v psql >/dev/null 2>&1 || {
        echo "ERROR: no psql on PATH, and PG_CONTAINER is empty so no container is used." >&2
        echo "       Install postgresql-client, or set PG_CONTAINER to a running container." >&2
        exit 1
    }
    TARGET="${PGHOST:-local socket}${PGPORT:+:$PGPORT}, database $TARGET_DATABASE"
fi

# Reads one .sql file on stdin. `-f -` rather than `-f "$path"` so the file is read on
# this side in both modes: the container has no copy of the project.
run_psql() {
    if [ -n "$PG_CONTAINER" ]; then
        local docker_env=() setting
        for setting in "${PSQL_ENV[@]}"; do docker_env+=(-e "$setting"); done
        docker exec -i "${docker_env[@]}" "$PG_CONTAINER" psql -q -v ON_ERROR_STOP=1 -f -
    else
        env "${PSQL_ENV[@]}" psql -q -v ON_ERROR_STOP=1 -f -
    fi
}

apply_folder() {
    local folder="$1"; shift
    local name
    for name in "$@"; do
        echo "==> $folder/$name.sql"
        run_psql < "$folder/$name.sql"
    done
}

echo "Applying schema to $TARGET."

# Security/ is applied in two passes. What creates something others grant to must run
# before them; a GRANT ... ON ALL TABLES is a snapshot of the tables that exist when it
# runs, so it must run after them. Partitions/ is last because each partition's own
# REVOKE only holds if it follows every GRANT that could reach it.
apply_folder "$SECURITY_DIR" ${SECURITY_ORDER[@]+"${SECURITY_ORDER[@]}"}
apply_folder "$TABLES_DIR" "${TABLES_ORDER[@]}"
apply_folder "$DATA_DIR" ${DATA_ORDER[@]+"${DATA_ORDER[@]}"}
apply_folder "$PROCEDURES_DIR" ${PROCEDURES_ORDER[@]+"${PROCEDURES_ORDER[@]}"}
apply_folder "$SECURITY_DIR" ${SECURITY_AFTER_TABLES_ORDER[@]+"${SECURITY_AFTER_TABLES_ORDER[@]}"}
apply_folder "$PARTITIONS_DIR" ${PARTITIONS_ORDER[@]+"${PARTITIONS_ORDER[@]}"}

echo "Schema applied to $TARGET."
