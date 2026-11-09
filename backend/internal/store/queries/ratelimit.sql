-- FEAT-006. The statement budget's ledger.

-- name: LockAccountBudget :one
-- Held for the rest of the admission, so two submissions racing for an
-- account's last slot are serialised rather than both counting one free slot.
-- NO KEY UPDATE, not UPDATE: it must not block the key-share lock an
-- audit_entry insert takes on the same account.
SELECT id FROM app_user WHERE id = $1 FOR NO KEY UPDATE;

-- name: ForgetAdmissionsBefore :exec
DELETE FROM statement_admission WHERE user_id = $1 AND admitted_at <= $2;

-- name: ListAdmissions :many
SELECT admitted_at FROM statement_admission WHERE user_id = $1 ORDER BY admitted_at;

-- name: RecordAdmission :exec
INSERT INTO statement_admission (user_id, admitted_at) VALUES ($1, $2);
