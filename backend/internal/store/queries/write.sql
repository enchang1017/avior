-- FEAT-009. Write requests, their decisions, and where the capture went.

-- name: InsertWriteRequest :one
INSERT INTO write_request (
    id, database, statement, kind, target_table, reason,
    requested_by, estimated_rows, state, rollback_of
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', sqlc.narg(rollback_of))
RETURNING id, requested_at;

-- name: GetWriteRequest :one
SELECT r.id, r.database, r.statement, r.kind, r.target_table, r.reason,
       r.requested_by, requester.username AS requester_name, r.requested_at,
       r.estimated_rows, r.state,
       r.decided_by, decider.username AS decider_name, r.decided_at, r.decision_note,
       r.ran_at, r.affected_rows, r.duration_ms, r.error,
       r.capture_rows, r.reverse_statement,
       r.capture_removed_at, r.capture_removed_by, remover.username AS remover_name,
       r.rollback_of,
       (SELECT back.id FROM write_request back
        WHERE back.rollback_of = r.id AND back.state = 'applied') AS rolled_back_by
FROM write_request r
JOIN app_user requester ON requester.id = r.requested_by
LEFT JOIN app_user decider ON decider.id = r.decided_by
LEFT JOIN app_user remover ON remover.id = r.capture_removed_by
WHERE r.id = $1;

-- name: ListWriteRequests :many
-- An empty state and a zero requester both mean "every one", so one query
-- serves the approver's queue and a developer's own list. Two queries would be
-- two places for the ordering and the joins to drift.
SELECT r.id, r.database, r.statement, r.kind, r.target_table, r.reason,
       r.requested_by, requester.username AS requester_name, r.requested_at,
       r.estimated_rows, r.state,
       r.decided_by, decider.username AS decider_name, r.decided_at, r.decision_note,
       r.ran_at, r.affected_rows, r.duration_ms, r.error,
       r.capture_rows, r.reverse_statement,
       r.capture_removed_at, r.capture_removed_by, remover.username AS remover_name,
       r.rollback_of,
       (SELECT back.id FROM write_request back
        WHERE back.rollback_of = r.id AND back.state = 'applied') AS rolled_back_by
FROM write_request r
JOIN app_user requester ON requester.id = r.requested_by
LEFT JOIN app_user decider ON decider.id = r.decided_by
LEFT JOIN app_user remover ON remover.id = r.capture_removed_by
WHERE (sqlc.arg(state)::text = '' OR r.state = sqlc.arg(state)::text)
  AND (sqlc.arg(requested_by)::uuid IS NULL OR r.requested_by = sqlc.arg(requested_by)::uuid)
ORDER BY r.requested_at DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: ListWriteRequestChanges :many
SELECT c.action, c.note, c.changed_at, coalesce(u.username, '') AS actor
FROM write_request_change c
LEFT JOIN app_user u ON u.id = c.actor_id
WHERE c.request_id = $1
ORDER BY c.changed_at DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: InsertWriteRequestChange :exec
INSERT INTO write_request_change (id, request_id, actor_id, action, note)
VALUES ($1, $2, $3, $4, $5);

-- name: SetWriteRequestDecision :exec
UPDATE write_request
SET state = $2, decided_by = $3, decided_at = now(), decision_note = $4
WHERE id = $1;

-- name: SetWriteRequestWithdrawn :exec
UPDATE write_request SET state = 'withdrawn' WHERE id = $1;

-- name: SetWriteRequestOutcome :exec
-- The estimate is untouched. Keeping it beside the outcome is what makes an
-- estimate of ten and a result of nine hundred findable afterwards. AC-8.
UPDATE write_request
SET state = $2,
    ran_at = now(),
    affected_rows = $3,
    duration_ms = $4,
    error = $5,
    capture_rows = $6,
    reverse_statement = $7
WHERE id = $1;

-- name: ListWriteCaptures :many
-- Newest first.
--
-- Oldest first was the first answer, on the reasoning that what accumulated
-- longest ago is what somebody came to prune. It is not what they came to do
-- most of the time: the thing they just approved was at the bottom of a page of
-- history, with nothing to tell one row from another. The age filter serves the
-- pruning job, and the ordering serves the ordinary one.
SELECT r.id, r.database, r.statement, r.kind, r.target_table, r.reason,
       r.requested_by, requester.username AS requester_name, r.requested_at,
       r.estimated_rows, r.state,
       r.decided_by, decider.username AS decider_name, r.decided_at, r.decision_note,
       r.ran_at, r.affected_rows, r.duration_ms, r.error,
       r.capture_rows, r.reverse_statement,
       r.capture_removed_at, r.capture_removed_by, remover.username AS remover_name,
       r.rollback_of,
       (SELECT back.id FROM write_request back
        WHERE back.rollback_of = r.id AND back.state = 'applied') AS rolled_back_by
FROM write_request r
JOIN app_user requester ON requester.id = r.requested_by
LEFT JOIN app_user decider ON decider.id = r.decided_by
LEFT JOIN app_user remover ON remover.id = r.capture_removed_by
WHERE r.capture_rows IS NOT NULL AND r.capture_removed_at IS NULL
ORDER BY r.ran_at DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: SetWriteCaptureRemoved :exec
UPDATE write_request
SET capture_removed_at = now(), capture_removed_by = $2
WHERE id = $1;
