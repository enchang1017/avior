# Runbook — bringing a new governed database under Avior

For the DBA who creates the next governed database. Every step says what to do,
which credential does it, how to check it worked, and what breaks if it is
skipped. The features behind it are FEAT-059 (provisioning), FEAT-061 (logins),
FEAT-017 (releases), FEAT-009 (writes) and FEAT-076 (CONNECT and write
preparation).

Throughout, `<db>` is the database (for example `shop`) and `<service>` the
application (`role_<service>` is its group role, `<service>_app` its login).
"Provisioning page" is **Release → Provisioning** in Avior, which needs the
`provision.manage` permission (the `dba` role carries it).

## Layers

PostgreSQL privileges sit on three layers, and each has one owner.

| Layer | What lives there | Who changes it |
| --- | --- | --- |
| Cluster | roles, logins, passwords, membership | Provisioning page (create role, login, password); `bootstrap.sql` for Avior's own roles |
| Database | the database itself, `CONNECT`, the `REVOKE ... FROM PUBLIC`, write preparation (`avior_backup`, the writer's grants and default privileges) | Provisioning page |
| Schema / object | tables, functions, `GRANT USAGE/SELECT/EXECUTE ... TO role_<service>`, the service's own default privileges | the application's database repository, as migrations released through Avior |

The rule for anything not listed: **if dropping and recreating the objects would
lose it, it belongs in migrations; otherwise it belongs in provisioning.** A
grant on a table dies with the table, so it is a migration. `CONNECT` on the
database survives every schema change, so it is provisioning.

## 0. Once per PostgreSQL server: Avior's own roles

**Who:** a superuser at a psql prompt, once per server, never again.

```
psql "postgres://postgres@<host>:5432/postgres" -f deploy/store/bootstrap.sql
```

It creates whichever of these is missing and prompts for each password; type
the value OpenBao holds under the key of the same name:

| Role | OpenBao key | Used for |
| --- | --- | --- |
| `avior` | `avior` | owns `aviordb`, Avior's own store |
| `avior_governed` | `avior_governed` | every operator read |
| `avior_writer` | `avior_writer` | approved writes |
| `avior_deployer` | `avior_deployer` | applying releases |
| `avior_provisioner` | `avior_provisioner` | the Provisioning page (`CREATEDB`, `CREATEROLE`) |

`AVIOR_ADMIN_PASSWORD` and `AVIOR_SESSION_SECRET` are application secrets, not
PostgreSQL roles; nothing here creates them.

**`avior_writer` is created here and nowhere else.** Avior never creates it and
never sets or changes its password — preparing a database for writes refuses
while it is missing. It is a cluster role: **one password for every governed
database on that server.** If `bootstrap.sql` is not an option, create it by
hand with the OpenBao value, typed at a prompt rather than on a command line:

```sql
\prompt 'avior_writer password (from OpenBao): ' pw
SELECT format('CREATE ROLE avior_writer LOGIN PASSWORD %L', :'pw') \gexec
\unset pw
```

Rotating it is a deliberate `ALTER ROLE avior_writer PASSWORD ...` followed by
updating OpenBao, and it changes the credential for every database at once.

**Check (read-only):**

```sql
SELECT rolname, rolcanlogin, rolcreatedb, rolcreaterole FROM pg_roles
WHERE rolname IN ('avior','avior_governed','avior_writer','avior_deployer','avior_provisioner');
```

Five rows, all `rolcanlogin = t`; only `avior_provisioner` has `rolcreatedb` and
`rolcreaterole`.

**If skipped:** the pod does not start (the secret mount names every key), or
the feature using the missing role answers that it is not configured. A missing
`avior_writer` shows on the Provisioning page as "avior_writer is not on the
server".

## 1. Provision the database

**Who:** you, on the Provisioning page, as `avior_provisioner`.

Type `<db>` under **Database**, press **Check**, then **Create it** on the
Database row. In one act Avior creates the database, `<db>_schema_owner`
(`NOLOGIN`), makes `avior_deployer` a member of it, and inside the database:
`REVOKE ALL ... FROM PUBLIC`, `CONNECT` for `avior_deployer` and
`avior_governed` only, `public` owned by `<db>_schema_owner`, and the default
privilege that gives `avior_governed` `SELECT` on every table a release adds.

**Check:** after the page reads back, the rows say Database **present**,
`<db>_schema_owner` **present**, Deploy credential owns the schema **yes**.

**If skipped:** releases are refused as `release.database_missing`.

## 2. The application's group role and login

**Who:** you, on the Provisioning page.

1. Put `role_<service>` under **Roles the migration grants to** and press
   **Check**. If its row says **missing**, press **Create it** (a `NOLOGIN`
   group role).
2. Under **Roles**, search for `<db>`, press **New login**, name it
   `<service>_app`, tick `role_<service>` under **Member of**, and **Create
   login**. The password is shown once: put it in OpenBao for the application
   before closing the dialog.

The application's repository may also create `role_<service>` in its
`Security/` files, conditionally (`shop.dbproject` does). Both are fine
together: the migration's `CREATE ROLE` is guarded, so whichever runs second
finds the role there.

**Check:** the Check table shows `role_<service>` **present · cannot connect**.
The roles map under **Roles** draws only roles related to `<db>` — its owner,
roles that may connect or hold a privilege there, and their members — so the
new group and login appear on it once step 3 or the first release ties them to
the database. A role that only shares the database's name never appears.

**If skipped:** the first release fails at its first `GRANT ... TO
role_<service>` (`role "role_<service>" does not exist`), and the application
has nothing to connect as.

## 3. Grant connect to the group role

**Who:** you, on the Provisioning page, acting as `<db>_schema_owner`.

With `role_<service>` declared and **Check** pressed, its row reads
**present · cannot connect**. Press **Grant connect**. The row becomes
**present · may connect**.

Why: step 1 revoked everything from `PUBLIC`, so no role can open a connection
to `<db>` unless it is granted `CONNECT` by name. The grant goes to the group,
and every login that is a member of it can then connect.

**Check (read-only):**

```sql
SELECT has_database_privilege('<service>_app', '<db>', 'CONNECT');
```

`t`. On the roles map for `<db>`, `role_<service>` carries **✓ connect** with
`<service>_app` joined to it by a solid line; clicking either opens
its dialog, whose header reads **✓ connect**.

**If skipped:** the application's login is refused at connect with
`permission denied for database "<db>"`, however many tables its group holds.

## 4. The first release

**Who:** the application's CI submits it; a `release_approver` approves; Avior
applies it as `<db>_schema_owner`. How the project generates and submits it is
`release-a-database-project.md`.

The baseline migration is **schema only** — tables, functions, and the object
grants to `role_<service>` (`USAGE`, `SELECT`, `EXECUTE`, …), which live in the
application's repository like every other line of its schema. Reference data
(seed rows) is **not** a migration; it is loaded in step 6.

**Check:** the release reaches **applied**; **Query** can read one of its tables
(that read runs as `avior_governed`, which the default privilege from step 1
reached); on the roles map, `<db>_schema_owner` reads **owns N tables**, and
the dialog for `role_<service>` shows a ✓ for each privilege the migration
granted on each table.

**If skipped:** nothing else here has anything to act on.

## 5. Prepare for writes

**Who:** you, on the Provisioning page, acting as `<db>_schema_owner`; the
writer role is the one Avior is configured with (`avior_writer`). No password is
typed or sent.

After the first release, so the grants on existing tables cover the baseline.
The **Write path · avior_writer** row reads **not prepared · missing …**; press
**Prepare for writes**. Avior creates the `avior_backup` schema and its
`captured_row` table, and grants `avior_writer` `CONNECT`, `USAGE` on `public`,
`SELECT, INSERT, UPDATE, DELETE` on every table there, the same **by default on
every table `<db>_schema_owner` creates later**, and `SELECT, INSERT, DELETE` on
the capture table. It never creates, alters or reads a password.

**Check:** the row reads **prepared**. Read-only:

```sql
-- connected to <db>
SELECT to_regclass('avior_backup.captured_row') IS NOT NULL;           -- t
SELECT has_table_privilege('avior_writer', 'public.<a table>', 'UPDATE'); -- t
```

**If skipped:** every write request against `<db>` is refused as
`write.database_not_prepared`. Preparing again later is safe: it completes what
is missing and reports nothing changed when nothing is.

## 6. Load reference data

**Who:** a developer submits a write request against `<db>` (one `INSERT` per
request); an `update_approver` approves it; it runs as `avior_writer` and keeps
what it changed in `avior_backup`.

**Check:** the request reaches executed, and **Query** returns the rows.

## 7. Verification checklist

- [ ] Provisioning **Check** for `<db>` with `role_<service>` declared: Database
      present, `<db>_schema_owner` present, Deploy credential owns the schema
      yes, `role_<service>` present · may connect, Write path prepared.
- [ ] Roles map for `<db>`: no card carries **⚠ cannot connect**; the dialog
      for `<service>_app` shows its tables' privileges marked ¹ with the
      footnote **¹ via role_<service>**.
- [ ] **Query** reads a table of `<db>` (as `avior_governed`).
- [ ] A write request against `<db>` is accepted rather than refused as not
      prepared.
- [ ] The application connects as `<service>_app` with the password in OpenBao.

## Resetting a database

Sometimes a non-production database has to go back to empty and take its first
release again. Two things are in the way, and both need a DBA.

**Avior's ledger is append-only.** Triggers on the `release*` tables in
`aviordb` refuse every `UPDATE` of history and every `DELETE`. Making Avior
forget a database's releases is a superuser editing `aviordb` deliberately, not
something any page does.

**The target has to be put back to what provisioning left.** As a superuser,
connected to `<db>`:

```sql
DROP SCHEMA public CASCADE;
CREATE SCHEMA public AUTHORIZATION <db>_schema_owner;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO avior_deployer;
GRANT USAGE ON SCHEMA public TO avior_governed;
ALTER DEFAULT PRIVILEGES FOR ROLE <db>_schema_owner IN SCHEMA public
    GRANT SELECT ON TABLES TO avior_governed;
DROP SCHEMA IF EXISTS avior_backup CASCADE;   -- only if you mean to drop the captures too
```

Then:

- The writer's grants on `public` and its default privileges there go with the
  schema, and `avior_backup` with its own drop: **press Prepare for writes
  again** after the new first release. The page will say **not prepared** until
  then.
- `CONNECT` grants are on the **database**, not the schema, so they survive:
  `role_<service>` may still connect and step 3 need not be repeated.
- Roles and logins are cluster objects and survive as well.
