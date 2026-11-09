-- Declarative. This file is the desired shape, and the generator's input.
--
-- Steps 1-5 of ADR-0004's pipeline live in a database project, not in Avior.
-- This directory stands in for one so the release path has something real to
-- receive: a hand-written ledger would only prove Avior accepts the shape this
-- repository writes.
CREATE TABLE public.ledger_probe (
    id         integer PRIMARY KEY,
    note       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Added by 20260903_000001. A declarative file describes the shape the
    -- database should end up in, so a column a migration adds belongs here from
    -- the moment that migration is written — otherwise the two descriptions
    -- disagree, which is the drift the replay gate exists to refuse.
    settled_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_ledger_probe_created ON public.ledger_probe (created_at DESC);
