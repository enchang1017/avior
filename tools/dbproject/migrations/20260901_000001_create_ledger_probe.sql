-- The baseline. Replaying this directory into an empty database has to produce
-- what tables/ declares, so the history has to start somewhere, and a project
-- that begins with an ALTER describes a table nobody created.
CREATE TABLE public.ledger_probe (
    id         integer PRIMARY KEY,
    note       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_ledger_probe_created ON public.ledger_probe (created_at DESC);
