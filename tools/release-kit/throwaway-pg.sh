#!/usr/bin/env bash
#
# A PostgreSQL container that lives for the length of one script.
#
# Sourced by verify-migrations.sh, generate-migration.sh and prepare-release.sh, which
# all need the same thing: an empty server, on a network a second container can reach,
# torn down whatever happens.
#
# It runs on its own docker network rather than only on a published port, because atlas
# runs in a container too and reaches it by name. It publishes a port as well, because
# Avior's schemadigest runs on this machine.
#
#   pg_start              starts it and waits
#   pg_create_roles       creates release-kit.conf's APP_ROLES as NOLOGIN
#   pg_psql ARGS...       psql inside it, reading stdin
#   pg_dsn DATABASE       a DSN another container on the network can use
#   pg_host_dsn DATABASE  a DSN a program on this machine can use
#   PG_NAME               the container name, for apply-schema.sh's PG_CONTAINER
#   PG_NETWORK            the network name, for docker run --network

PG_IMAGE="${PG_IMAGE:-postgres:18-alpine}"
PG_NAME="${PG_NAME:-release-kit-pg}"
PG_NETWORK="${PG_NETWORK:-release-kit-net}"
PG_PASSWORD="throwaway"

pg_cleanup() {
    docker rm -f "$PG_NAME" >/dev/null 2>&1 || true
    docker network rm "$PG_NETWORK" >/dev/null 2>&1 || true
}

pg_start() {
    # Before as well as after: a previous run killed part-way through leaves a
    # container holding the name, and the failure that produces reads as the image
    # being broken.
    pg_cleanup
    trap pg_cleanup EXIT

    docker network create "$PG_NETWORK" >/dev/null
    echo "==> starting $PG_IMAGE"
    docker run -d --name "$PG_NAME" --network "$PG_NETWORK" \
        -e POSTGRES_PASSWORD="$PG_PASSWORD" -p 0:5432 "$PG_IMAGE" >/dev/null
    PG_HOST_PORT="$(docker port "$PG_NAME" 5432/tcp | head -1 | sed 's/.*://')"

    # Two waits, because the image starts twice. It runs a temporary server to execute
    # initdb, then shuts it down and starts the real one -- and both pg_isready and a
    # plain SELECT answer during the first.
    local _
    for _ in $(seq 60); do
        if docker logs "$PG_NAME" 2>&1 | grep -q "PostgreSQL init process complete"; then
            break
        fi
        sleep 1
    done
    for _ in $(seq 60); do
        if docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_NAME" \
            psql -tAq -U postgres -d postgres -c "SELECT 1" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    echo "ERROR: postgres never became ready" >&2
    return 1
}

pg_psql() {
    docker exec -i -e PGPASSWORD="$PG_PASSWORD" "$PG_NAME" \
        psql -q -v ON_ERROR_STOP=1 -U postgres "$@"
}

pg_dsn() {
    echo "postgres://postgres:${PG_PASSWORD}@${PG_NAME}:5432/$1?sslmode=disable&search_path=public"
}

pg_host_dsn() {
    echo "postgres://postgres:${PG_PASSWORD}@localhost:${PG_HOST_PORT}/$1?sslmode=disable"
}

# Roles are cluster-level, so neither lane creates them in production; a migration
# carries only GRANT statements. ADR-0004.
pg_create_roles() {
    local role
    for role in ${APP_ROLES[@]+"${APP_ROLES[@]}"}; do
        pg_psql -d postgres -c "CREATE ROLE \"$role\" NOLOGIN" >/dev/null
    done
}
