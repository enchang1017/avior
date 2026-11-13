#!/usr/bin/env bash
#
# Builds the submission Avior is handed. Step 6 of ADR-0004's pipeline, the half that
# needs a database.
#
# Avior is not asked to work out what is pending. It is told, and told what the target
# is expected to look like before the change:
#
#   pending  = migrations/ minus what the target already ran
#   digest   = a throwaway database with exactly what the target already ran
#
# The digest is the point. Avior recomputes it against the real target before applying
# and refuses when the two disagree, which is how a hand edit to production is caught --
# so this number is derived from the history and never read off production, or the
# check would be comparing production with itself.
#
# Usage, from the database project's root:
#
#   <avior>/tools/release-kit/prepare-release.sh applied.json submission.json
#
# applied.json is what GET /api/v1/databases/<database>/migrations answered.
# MERGE_REQUEST_URL is sent when set; on GitLab it defaults to the commit's page.
set -euo pipefail

KIT_DIR="$(cd "$(dirname "$0")" && pwd)"
AVIOR_REPO="${AVIOR_REPO:-$KIT_DIR/../..}"

APPLIED="$(cd "$(dirname "${1:?usage: prepare-release.sh <applied.json> <submission.json>}")" && pwd)/$(basename "$1")"
OUT_ARG="${2:?usage: prepare-release.sh <applied.json> <submission.json>}"
OUT="$(cd "$(dirname "$OUT_ARG")" && pwd)/$(basename "$OUT_ARG")"

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

# Anything but `recorded` stops this.
#
# `unaccounted` is a file the target ran that Avior never submitted, `mismatched` is one
# filename with two bodies, and `missing` is a file an applied release claims ran that
# the target does not corroborate. Each one means the target is not the history, and a
# baseline replayed from the history would describe a database that does not exist.
anomalies="$(jq -r '[.migrations[] | select(.status != "recorded")] | length' "$APPLIED")"
if [ "$anomalies" != "0" ]; then
    echo "ERROR: $DATABASE holds $anomalies migration(s) that are not simply recorded:" >&2
    jq -r '.migrations[] | select(.status != "recorded") | "       \(.status)  \(.filename)"' "$APPLIED" >&2
    echo "       Avior's Applied page explains each one. Nothing can be prepared" >&2
    echo "       against a target whose history is not its own." >&2
    exit 1
fi

recorded="$(jq -r '[.migrations[].filename] | sort | .[]' "$APPLIED")"
pending=""
for file in "$MIGRATIONS_DIR"/*.sql; do
    [ -e "$file" ] || continue
    name="$(basename "$file")"
    if ! printf '%s\n' "$recorded" | grep -qxF "$name"; then
        pending="$pending$file"$'\n'
    fi
done
pending="$(printf '%s' "$pending" | sed '/^$/d')"

if [ -z "$pending" ]; then
    echo "$DATABASE already holds every migration in this project. Nothing to submit."
    exit 0
fi

echo "==> $DATABASE holds $(printf '%s\n' "$recorded" | sed '/^$/d' | wc -l | tr -d ' ') migration(s); $(printf '%s\n' "$pending" | wc -l | tr -d ' ') pending"

# The baseline, built from what the target already ran and nothing else. An empty list
# leaves an empty database, and schemadigest answers the empty-schema sentinel for one,
# so a first release is the same code path with nothing to replay.
pg_start
pg_create_roles
pg_psql -d postgres -c "CREATE DATABASE baseline" >/dev/null
for name in $recorded; do
    [ -n "$name" ] || continue
    if [ ! -f "$MIGRATIONS_DIR/$name" ]; then
        echo "ERROR: $DATABASE ran $name and this project has no such file." >&2
        exit 1
    fi
    echo "    replaying $name"
    pg_psql -d baseline -f - < "$MIGRATIONS_DIR/$name" >/dev/null
done

digest="$(cd "$AVIOR_REPO/backend" && go run ./cmd/schemadigest "$(pg_host_dsn baseline)")"
echo "==> baseline digest $digest"

commit="$(git rev-parse HEAD)"
merge_request_url="${MERGE_REQUEST_URL:-${CI_PROJECT_URL:+$CI_PROJECT_URL/-/commit/$commit}}"

# Through a file, never through an argument. A release can be a few hundred kilobytes of
# SQL and execve caps a single argument at far less, so --argjson with the migrations
# inline works until the day it dies with "Argument list too long".
#
# Added to the trap pg_start set rather than replacing it: a bare `trap ... EXIT` here
# would silently drop the one that tears down the throwaway container.
migrations_file="$(mktemp)"
trap 'pg_cleanup; rm -f "$migrations_file"' EXIT
for file in $pending; do
    jq -n --arg filename "$(basename "$file")" --rawfile sql "$file" \
       '{filename: $filename, sql: $sql}'
done | jq -s '.' > "$migrations_file"

jq -n \
  --arg database "$DATABASE" \
  --arg commitSha "$commit" \
  --arg author "$(git log -1 --format='%ae')" \
  --arg mr "$merge_request_url" \
  --arg digest "$digest" \
  --slurpfile migrations "$migrations_file" \
  '{database: $database, commitSha: $commitSha, author: $author,
    mergeRequestUrl: $mr, expectedSchemaDigest: $digest, migrations: $migrations[0]}
   | if .mergeRequestUrl == "" then del(.mergeRequestUrl) else . end' > "$OUT"

echo "==> wrote $OUT ($(wc -c < "$OUT" | tr -d ' ') bytes)"
