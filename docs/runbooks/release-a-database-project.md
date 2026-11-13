# Runbook — releasing a database project through Avior

For the team that owns a database project and the DBA who connects it to Avior.
It covers setting the project up once, the change a developer makes every day,
the release itself, and what to do when something refuses. The tools are in
`tools/release-kit`; why the pipeline has this shape is
`docs/adr/0004-postgres-deployment.md`; the features behind it are FEAT-017
(releases), FEAT-039 (applied history) and FEAT-059 (preconditions).

Bringing the database itself under Avior — the server roles, provisioning, the
application's login — is `new-governed-database.md`, and it comes first. This
runbook starts where its step 4, "the first release", begins.

Throughout, `<db>` is the governed database (for example `shop`), `<avior>` is a
checkout of this repository, and `<project>` is the database project's
repository.

## How the pieces fit

```
 <project> repository                         Avior
 ───────────────────────────────              ─────────────────────────────────
 Tables/  Stored Procedures/  ...             6  pending set + expected digest
 release-kit.conf                                received from CI
        │                                     7  drift check: the target's live
        │ 1 generate-migration.sh                schema against that digest
        ▼                                     8  a release_approver approves
 migrations/  (+ atlas.sum)                   9  applied in order, as
        │                                        <db>_schema_owner
        │ 2 lint-migrations.sh                10 recorded: who, when, how long
        │ 3 verify-migrations.sh                         ▲
        │ 4 merge request, reviewed as SQL               │
        │ 5 merge                                        │
        ├──────────► staging (declarative sync,          │
        │            nothing recorded)                   │
        │                                                │
        └─ manual release job ── prepare-release.sh ─────┘
                                 POST /api/v1/releases
```

Two lanes reach a database, deliberately:

| | Staging | Production |
| --- | --- | --- |
| Mechanism | the declarative files are applied directly; the `ALTER` is computed | `migrations/` is applied in order, through Avior |
| Recorded | nothing | every file, the approver, the duration, the outcome |
| Guarantees | the end state | the path |

`verify-migrations.sh` is what makes two lanes safe: on every change it builds
the database both ways and refuses the change if they differ.

Steps 1–5 run in the project and its CI. Avior generates nothing and lints
nothing; it is handed the result, checks the target has not moved, and applies
what a person approved.

## 0. What the project needs

**Who:** the project team, once.

- Docker, bash, perl, git, jq and Go on every machine that runs the kit, CI
  runners included.
- The database already provisioned, with the application's group role created
  (`new-governed-database.md` steps 1–3). A release against a database Avior
  does not know is refused as `release.database_unknown`.

## 1. Lay the project out

**Who:** the project team, once.

```
<project>/
  release-kit.conf        copied from <avior>/tools/release-kit/release-kit.conf.example
  Security/               grants; and any conditional CREATE ROLE
  Tables/                 one CREATE TABLE per file; enums a column uses, too
  Data/                   rows for local, CI and staging; never production
  Stored Procedures/      one function per file, the version in its name
  Partitions/             idempotent CREATE TABLE IF NOT EXISTS ... PARTITION OF
  migrations/             empty until step 2
```

Fill in `release-kit.conf`: `DATABASE=<db>`, the roles the GRANTs name in
`APP_ROLES`, and every file of every folder in its order array. The example file
says what each setting is for. A file present in a folder and missing from its
array is refused on the next run, so nothing is silently never applied.

Functions keep their version in the name (`<name>_yy_mm_dd`). A new version is a
new file and a new entry in `PROCEDURES_ORDER`; the old one stays while any
deployed build still calls it. ADR-0004 says why `CREATE OR REPLACE` is not used.

**Check:** an empty local database, with the `APP_ROLES` created in it, takes
the whole schema.

```
PGDATABASE=scratch <avior>/tools/release-kit/apply-schema.sh
```

## 2. Generate the baseline

**Who:** the project team, once.

```
cd <project>
<avior>/tools/release-kit/generate-migration.sh baseline
```

With `migrations/` empty, the diff is the whole of `Tables/`, and every function,
grant and partition is copied, so this writes up to four files:
`<ts>_baseline.sql`, `<ts+1>_baseline_routines.sql`, `<ts+2>_baseline_grants.sql`
and `<ts+3>_baseline_partitions.sql`, plus `atlas.sum`.

Name each one in `LINT_BASELINE` in `release-kit.conf`. They create the database
on a server serving nothing, so the linter's lock rules are true of them and none
of them matters.

`Security/` files listed in `SECURITY_ORDER` are not copied. They run before the
tables locally; in production, roles belong to provisioning (the runbook above,
step 2), and only grants travel in migrations.

**Check:**

```
<avior>/tools/release-kit/verify-migrations.sh
```

ends `PASS  both lanes build the same database`.

## 3. Issue a release token

**Who:** a DBA with write access to Avior's own store, `aviordb`, once per
project.

The release token is what the project's CI submits with. It can submit and read
what a target already holds; it cannot approve or apply anything. Avior keeps
only its SHA-256, so the plaintext is generated here, handed to the project's CI,
and never stored by Avior.

```
openssl rand -hex 32
```

Put the value in the project's CI secret store first, then record its hash. Typed
at a prompt rather than on a command line, so it is in no shell history:

```sql
-- connected to aviordb as its owner
\prompt 'release token: ' tok
INSERT INTO release_token (id, issuer, token_hash)
VALUES (gen_random_uuid(), '<project>', sha256(convert_to(:'tok', 'UTF8')));
\unset tok
```

`issuer` is unique: one token per project. Revoking it is
`UPDATE release_token SET disabled = true WHERE issuer = '<project>';`.

**Check:** with the token, CI can read the target's ledger.

```
curl -sS -H "Authorization: Bearer $AVIOR_RELEASE_TOKEN" \
  "$AVIOR_URL/api/v1/databases/<db>/migrations"
```

answers `{"migrations": [...]}`, empty before the first release.

## 4. Wire up CI

**Who:** the project team, once.

Start from `<avior>/tools/release-kit/examples/gitlab-ci.yml` and set:

| Variable | What |
| --- | --- |
| `AVIOR_REPO_URL` | a clone URL for this repository, readable by the jobs |
| `AVIOR_REF` | the Avior branch or tag the kit is taken from |
| `AVIOR_URL` | Avior's base URL, reachable from the release jobs |
| `AVIOR_RELEASE_TOKEN` | the token from step 3, masked and protected |
| `DOCKER_RUNNER` | the tag of a runner that provides Docker |

The pipeline it gives:

| Job | When | Refuses on |
| --- | --- | --- |
| `migrations agree with the schema` | every push | the two lanes differ |
| `migrations are safe to apply` | every push | a lint finding |
| `sync staging` | default branch, after both | — |
| `read the target ledger` | **manual**, default branch | the target's history is not its own |
| `build the submission` | after it | — |
| `submit to avior` | after it | Avior refuses the submission |

`sync staging` is left for the project to fill in: Atlas `schema apply` against
staging for the tables, then the functions, grants and partitions applied again.
Keep it serialised (`resource_group`): two syncs into one database undo each
other.

If the runners cannot reach Avior, run the two `curl` calls from somewhere that
can, and pass the token through a secret rather than an argument or a literal
environment value — both of those show up in process listings and audit logs.

## 5. Every day: changing the schema

**Who:** a developer in the project.

1. Change the declarative file — a column in `Tables/`, a new function version in
   `Stored Procedures/` and its entry in `release-kit.conf`.
2. Generate:

   ```
   <avior>/tools/release-kit/generate-migration.sh add_accounts_sort_order
   ```

   It writes the table diff, copies any function, grant or partition never sent
   before, and starts every file with `SET lock_timeout = '5s'` and
   `SET statement_timeout = '30min'`. When nothing differs, it writes nothing.
3. Lint and verify:

   ```
   <avior>/tools/release-kit/lint-migrations.sh
   <avior>/tools/release-kit/verify-migrations.sh
   ```

4. Commit the declarative change and the generated files together, and open the
   merge request. The reviewer reads the generated SQL: that is what will run.

**Never edit a generated file.** An edit severs it from the file it came from,
and `atlas.sum` refuses it. When the linter objects, the answer goes in the
declarative file or on the generator's command line, and the file is generated
again (section 7).

## 6. Releasing

**Who:** someone with access to the project's pipeline, then a
`release_approver` in Avior.

1. On the commit to release, on the default branch, press **read the target
   ledger**. The two jobs after it follow on their own.
2. `build the submission` works out what `<db>` has not run and the digest it
   should have before this release. When nothing is pending it says so and stops.
3. `submit to avior` posts it. Avior now lists it as pending, with the SQL, the
   commit, the author and the merge request.
4. A `release_approver` reads the SQL in Avior and approves.
5. Avior checks `<db>` has not drifted from that digest, applies each file in
   order as `<db>_schema_owner`, and records the outcome.

**Check:** the release reaches **applied**, and Avior's applied history for `<db>`
lists every file with the status `recorded`.

A file declaring `-- atlas:txmode none` (one `CONCURRENTLY` statement, nothing
else) runs outside a transaction; every other file runs in one, and a failure
rolls that file back.

## 7. When something refuses

| Where | What it says | What it means | What to do |
| --- | --- | --- | --- |
| any kit script | `no release-kit.conf` | not run from the project root | `cd <project>`, or set `PROJECT_DIR` |
| any kit script | `<folder>/<file>.sql is not listed` | a file nobody put in the order | add it to its array in `release-kit.conf` |
| lint | `ban-drop-column` | a column disappears; a rename looks exactly like this | a rename: put the column back and expand-and-contract. A drop: regenerate with `--drop-intended "<why>"` |
| lint | `constraint-missing-not-valid` | a `CHECK` validated while it is added | regenerate with `--not-valid "<why>"` |
| lint | `NOT NULL with no DEFAULT` | existing rows would have no value | give the column a `DEFAULT` in `Tables/` and regenerate |
| lint | `runs CONCURRENTLY and does not declare it` | Avior applies a file in a transaction | a file of its own with `-- atlas:txmode none` and that one statement |
| lint | `refusing to lint: base ... resolves to HEAD` | the diff would be empty whatever the change | fix the base CI passes in `LINT_BASE` |
| lint | a file squawk is wrong about | rare, and needs a reason | `SQUAWK_ANSWERED` in `release-kit.conf`, by full file name, with the reason |
| verify | `the two lanes describe different databases` | a declarative change with no migration, or the reverse | run `generate-migration.sh` |
| verify | `the two lanes grant different privileges` | a grant reached only one lane | a table added after the baseline needs its grants in a `Security/` file of its own |
| release | `holds N migration(s) that are not simply recorded` | the target's history is not this project's | Avior's applied history explains each file; resolve it before releasing |
| Avior | `release.database_unknown`, `release.database_missing` | `<db>` is not provisioned | `new-governed-database.md` step 1 |
| Avior | `release.drift_detected` | somebody changed `<db>` by hand | find out who and why before anything is applied over it |
| Avior | `release.txmode_*`, `release.not_transactional` | a file's transaction declaration does not fit what it carries | the lint rule of the same shape says how |

A release that fails part-way leaves the files before the failure applied and
recorded. Fix the cause, release again, and Avior compares against where the
partial release left the target rather than calling its own change drift.

## Keeping the kit current

The kit runs from the Avior checkout CI clones (`AVIOR_REF`), never from a copy
in the project. The kit and the `schemadigest` it computes must come from the
same commit: Avior's drift check compares against that number, and two versions
of the hash compare two unrelated things.
