-- name: InsertRelease :one
-- The unique constraint on (database, commit_sha, migration_checksum) is what
-- makes a resubmission idempotent rather than a check-then-insert race: CI
-- re-runs, and one merge must not produce two entries an approver has to choose
-- between. DO NOTHING returns no row, so the caller reads the existing entry.
INSERT INTO release (
    id, database, commit_sha, merge_request_url, author,
    migration_checksum, lint_findings, verify_result,
    staging_synced_at, expected_digest, state
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending')
ON CONFLICT (database, commit_sha, migration_checksum) DO NOTHING
RETURNING id, database, commit_sha, merge_request_url, author,
          migration_checksum, state, submitted_at;

-- name: FindRelease :one
SELECT id, database, commit_sha, merge_request_url, author,
       migration_checksum, state, submitted_at
FROM release
WHERE database = $1 AND commit_sha = $2 AND migration_checksum = $3;

-- name: InsertReleaseMigration :exec
-- relations is NULL when the extractor could not account for the whole file.
-- That is not the same as an empty array, and FEAT-039 AC-8 turns on the
-- difference: an unreadable file appears under every table filter.
INSERT INTO release_migration (release_id, ordinal, filename, sql, relations)
VALUES ($1, $2, $3, $4, $5);

-- name: ListReleases :many
SELECT r.id, r.database, r.commit_sha, r.merge_request_url, r.author,
       r.migration_checksum, r.state, r.submitted_at,
       (SELECT count(*) FROM release_migration m WHERE m.release_id = r.id) AS migration_count
FROM release r
WHERE (sqlc.narg('state')::text IS NULL OR r.state = sqlc.narg('state')::text)
  AND (sqlc.narg('database')::text IS NULL OR r.database = sqlc.narg('database')::text)
ORDER BY r.submitted_at DESC, r.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: GetRelease :one
SELECT id, database, commit_sha, merge_request_url, author,
       migration_checksum, lint_findings, verify_result,
       staging_synced_at, expected_digest, resume_digest, state, submitted_at,
       (SELECT count(*) FROM release_migration m WHERE m.release_id = release.id) AS migration_count
FROM release
WHERE id = $1;

-- name: ListReleaseMigrations :many
-- The relations column comes back too: FEAT-017 AC-21 reads the target's size
-- for each of them, and re-extracting at read time would follow a change in the
-- extractor rather than a change in the file.
SELECT ordinal, filename, sql, relations
FROM release_migration
WHERE release_id = $1
ORDER BY ordinal;

-- name: ListAppliedMigrationSources :many
-- Every migration Avior holds for one database, with the relations it names.
-- The caller reconciles these against the target's own schema_migrations; this
-- side alone cannot say what actually ran.
--
-- Failed releases are included because the files before the failure did run and
-- a target row has to be explainable. Which of them may be reported as missing
-- is decided in the domain, not here.
--
-- No table filter: filtering here would drop the ledger row explaining a target
-- row, and the reconciliation would then call an ordinary migration
-- unaccounted. The filter belongs after the two sides have been matched.
SELECT m.filename, m.sql, m.relations, m.ordinal,
       r.id AS release_id, r.database, r.commit_sha, r.author, r.merge_request_url,
       r.migration_checksum, r.state, r.submitted_at
FROM release_migration m
JOIN release r ON r.id = m.release_id
WHERE r.database = $1
  AND r.state IN ('applied', 'failed')
ORDER BY r.submitted_at DESC, r.id DESC, m.ordinal;

-- name: LatestDriftCheck :one
SELECT result, expected_digest, actual_digest, detail, checked_at
FROM release_drift_check
WHERE release_id = $1
ORDER BY checked_at DESC, id DESC
LIMIT 1;

-- name: InsertDriftCheck :exec
INSERT INTO release_drift_check (id, release_id, result, expected_digest, actual_digest, detail)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListReleaseApprovals :many
SELECT u.username, a.decision, a.note, a.decided_at
FROM release_approval a
JOIN app_user u ON u.id = a.user_id
WHERE a.release_id = $1
ORDER BY a.decided_at;

-- name: InsertReleaseApproval :exec
INSERT INTO release_approval (id, release_id, user_id, decision, note)
VALUES ($1, $2, $3, $4, $5);

-- name: ListReleaseApplications :many
SELECT u.username, a.started_at, a.result, a.duration_ms, a.files_applied, a.error, a.invalid_indexes
FROM release_application a
JOIN app_user u ON u.id = a.applied_by
WHERE a.release_id = $1
ORDER BY a.started_at;

-- name: InsertReleaseApplication :exec
INSERT INTO release_application (
    id, release_id, applied_by, started_at, result, duration_ms, files_applied, error,
    invalid_indexes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: SetReleaseState :exec
UPDATE release SET state = $2 WHERE id = $1;

-- name: FindReleaseToken :one
-- Compared on the hash, never the token. last_used_at is not updated here: a
-- read that writes turns every submission into a write against the store, and
-- the submission itself already records who and when.
SELECT id, issuer
FROM release_token
WHERE token_hash = $1 AND NOT disabled;

-- name: RecordResumeDigest :exec
-- FEAT-017 AC-22. Where a partially-applied release left the target, so a retry
-- compares against what this entry itself caused rather than being refused as
-- drift by its own change.
UPDATE release SET resume_digest = $2 WHERE id = $1;
