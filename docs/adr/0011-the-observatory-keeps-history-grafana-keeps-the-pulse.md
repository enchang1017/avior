# ADR 0011 — The observatory keeps history; Grafana keeps the pulse

Date: 2026-09-27
Status: proposed
Specified by FEAT-071.

## Context

FEAT-071 asks Avior to show, per governed database, size and growth, slow and
long-running statements, operators' use of Avior, and when each table, index
and routine was last used. Two questions come before the feature:

1. Much of that looks like monitoring, and monitoring usually lives in
   Grafana. The estate's Grafana has no PostgreSQL dashboard today, so nothing is
   being replaced either way.
2. PostgreSQL keeps cumulative counters with no time in them
   (`pg_stat_user_functions` has `calls`, never a last-call time). Turning them
   into "last run on" dates needs a job that reads them on a schedule and keeps
   what it read. Something has to run that job.

## Decision

**Avior does governance and history. Grafana, with `postgres_exporter`, does
realtime and alerting.**

- Avior: its own audit summarised; last-used dates and the idle flag; size
  history by day; top statements and live activity **with statement text**,
  because text is gated per operator by Avior's permissions and FEAT-058
  database access, and Grafana has neither.
- Grafana: connections, transactions per second, cache hit ratio, replication
  lag, locks, disk, wraparound — seconds-resolution series and every alert.
  Avior raises no alert.

**The snapshot job runs inside the backend**, on a timer every replica runs,
behind a session-level `pg_try_advisory_lock` in aviordb, with an
`observatory_run` row so an interval runs once even when replicas' timers are
offset. Interval 15 minutes, configurable, never below 5.

## Consequences

- No second deployable. The job uses the instance configuration, the OpenBao
  secrets and the `pgtarget` pools the backend already holds.
- A backend with no replica running (a rollout gap) misses ticks; a "last run"
  is then late by the gap, never wrong in the other direction. The run table
  shows gaps.
- The job competes with request traffic for the pools. It reads catalogue and
  statistics views only, with a 3 s statement timeout, and FEAT-071 budgets one
  database at 2 s.
- A PostgreSQL dashboard in Grafana is separate work, outside this repository.

## Alternatives rejected

- **Kubernetes CronJob.** Clean isolation, but a second copy of the instance
  configuration, secrets and connection code, and a second image to release.
- **Everything in Grafana.** Grafana cannot join counters to the catalogue by
  date-versioned name, cannot keep "last increase" state, and cannot gate
  statement text by Avior's roles.
- **Everything in Avior, including alerts.** Rebuilds Prometheus badly.
- **Leader election through the Kubernetes API.** Needs RBAC for a lease
  object; the advisory lock needs nothing Avior does not already have.
