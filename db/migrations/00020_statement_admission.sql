-- +goose Up

-- FEAT-006. One row per statement submission an account's budget admitted.
--
-- Counted here rather than in a process's memory because production runs
-- several replicas: a budget per pod would be several budgets per account, and
-- a restart would hand out a fresh one. Only admissions are rows; a refused
-- submission is recorded in audit_entry and spends nothing.
--
-- Rows older than the window are deleted by the next admission for the same
-- account, so the table holds at most one budget's worth per active account.
CREATE TABLE statement_admission (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    admitted_at timestamptz NOT NULL
);

CREATE INDEX statement_admission_user_idx ON statement_admission (user_id, admitted_at);

-- +goose Down
DROP TABLE statement_admission;
