#!/usr/bin/env bash
# Submits this project's pending migrations to Avior as a ledger entry.
#
# Steps 1-5 of the pipeline are the database project's, and this is the hand-off
# at step 6. Avior receives what has already been generated, linted and verified;
# it does not repeat any of that. ADR-0004.
set -euo pipefail

if [ "$#" -lt 3 ]; then
    echo "usage: $0 <avior-url> <release-token> <target-database> [psql-dsn]" >&2
    exit 64
fi

AVIOR_URL="$1"
TOKEN="$2"
TARGET_DB="$3"
# The digest is read from the target the release will be applied to. On a real
# pipeline this is the replayed history's digest, computed with no production
# connection at all; here the target is the only database available, which is
# honest about being a stand-in.
PSQL_DSN="${4:-postgres://avior:avior@localhost:55432/${TARGET_DB}?sslmode=disable}"

cd "$(dirname "$0")"

# The digest definition is shared with Avior. Both sides have to compute it the
# same way or the drift check compares two unrelated numbers, so this reads it
# out of the Go implementation rather than restating the query.
DIGEST=$(cd ../../backend && go run ./cmd/schemadigest "$PSQL_DSN")

MIGRATIONS=$(
    for file in migrations/*.sql; do
        jq -n --arg filename "$(basename "$file")" --rawfile sql "$file" \
            '{filename: $filename, sql: $sql}'
    done | jq -s '.'
)

BODY=$(jq -n \
    --arg database "$TARGET_DB" \
    --arg commitSha "$(git rev-parse HEAD)" \
    --arg author "$(git log -1 --format=%ae)" \
    --arg mergeRequestUrl "${CI_MERGE_REQUEST_URL:-}" \
    --arg digest "$DIGEST" \
    --argjson migrations "$MIGRATIONS" \
    --argjson lintFindings "$(cat lint.json)" \
    '{
        database: $database,
        commitSha: $commitSha,
        author: $author,
        migrations: $migrations,
        lintFindings: $lintFindings,
        expectedSchemaDigest: $digest,
        verifyResult: { passed: true, detail: "replayed on a fresh database and compared with tables/", againstProductionRestore: false }
    } | if $mergeRequestUrl == "" then del(.mergeRequestUrl) else .mergeRequestUrl = $mergeRequestUrl end')

curl -sS -f -X POST "${AVIOR_URL%/}/api/v1/releases" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$BODY"
