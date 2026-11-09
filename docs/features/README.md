# Feature registry

The port backlog, and the contract with the people who own this system. This is
the document to read to know what Avior does and what it does not yet; the
implementation is not read by hand.

Every entry has a `FEAT-NNN.md` file. A `todo` entry holds a skeleton: the
registry data and an empty acceptance-criteria section. Picking a card up starts
by reading the predecessor source it names and writing the `AC-n` list — **before any
code** — because the integration cases are enumerated from it, one per AC plus
one per distinct persisted outcome. A file whose AC section still says "not yet
derived" has not been started, whatever its status column says.

Recovered from the predecessor at `b0c1b8c` (2026-09-01): 20,814 lines of C# across 32
controllers, 11 services, 5 schedulers, and four SSDT database projects. The
navigation is taken from `Legacy/Views/Shared/_SideBar.cshtml`, which is the
only authoritative statement of what an operator can reach.

## Status

| Status | Meaning |
| --- | --- |
| `done` | Implemented, integration cases passing, behaviour recovered from the C# and recorded |
| `wip` | FEAT document written, implementation in progress |
| `spec` | FEAT document written, nothing built |
| `todo` | Nothing written. Behaviour still only exists in the predecessor C# |

Size is rough and only useful for ordering: `S` is a day or two, `M` about a
week, `L` more than that or carrying an unresolved design question. A `?` means
the estimate is a guess because the predecessor behaviour has not been read yet.

---

## Platform

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-001](inprogress/FEAT-001-session-authentication.md) | Session authentication | Sign in, session cookie, sign out, roles | `HomeController.Index/Otp/Logout`, `AccountService`; domain user plus TOTP | M | `wip` |
| [FEAT-002](inprogress/FEAT-002-guarded-query-execution.md) | Guarded query execution | Inspect and execute a read-only statement through the guard | `Developer/QueryController.Index`, `SqlParser.cs`, `ExecutionPlanAnalyzer`, `CommandTimer`; `LogQuery`, `LogExecutionPlan` | L | `wip` |
| [FEAT-003](done/FEAT-003-audit-trail-and-personal-query-log.md) | Audit trail and personal query log | Every submission, who and what; the operator's own history, and an administrator's view of everyone's | `Developer/PersonalLogController.GetPersonalQueryLog`, `Administrator/LogController.Index`, `LogService`, `DbLogService`; `LogQuery`, `ActionLog` | M | `done` |
| [FEAT-004](done/FEAT-004-role-administration.md) | Role administration | List the operators, grant and revoke a role, switch an account off | `Administrator/AccountController.Index/UpdateRole/UpdateStatus`; `Account`, `Role` | M | `done` |
| [FEAT-055](done/FEAT-055-column-restrictions.md) | Column restrictions | A restricted column is refused to everybody, including a DBA, until somebody unlocks it for a role and says why | `Administrator/PermissionController.Index/PermissionColumn`; `VisibleTableColumn` | M | `done` |
| [FEAT-058](done/FEAT-058-database-access-by-role.md) | Database access by role | Which roles reach which governed database, refused on every path rather than hidden from a list | none — the predecessor reached every database through one connection to `master` | M | `done` |
| [FEAT-005](todo/FEAT-005-db-resource-management.md) | DB resource management | The cost and row limits the guard applies. Labelled "DB Resource Mgmt" in the predecessor's menu even though the controller is `ExecutionPlan` | `Administrator/ExecutionPlanController.Index`; `ExecutionPlan` | S | `todo` |
| [FEAT-006](done/FEAT-006-rate-limiting-per-caller.md) | Rate limiting per caller | A statement budget per account, shared by every token and replica, so one caller cannot saturate an instance | `Services/TrafficLimiter`, `Developer/FetchController` | S | `done` |
| [FEAT-007](done/FEAT-007-ldap-credential-verifier.md) | LDAP credential verifier | Authenticate against the directory instead of Avior's own store | `DomainUser`, `System.Security.Principal.Windows` | M | `done` |
| [FEAT-008](done/FEAT-008-schema-browsing.md) | Schema browsing and generated SELECT | Databases, tables, columns; a SELECT the guard accepts | the `DataBase List` / `Tables and Views List` / `Columns List` / `Insert` controls of `QueryController` | M | `done` |
| [FEAT-009](done/FEAT-009-governed-write-requests.md) | **Governed write requests** | A developer requests an UPDATE or DELETE; an approver reads what it would touch and how many rows, and lets it run. Every write keeps what it overwrote, in the same transaction | `Developer/UpdateController.Create/CancelRequest/GetDataBase/GetTables/GetColumns`, `Dba/UpdateController.AdminApprove`, `Administrator/UpdateController`, `IBOUpdateQuery`; `UpdateQuery`, `EnumApproveStatus`, `EnumDeployStatus` | L | `done` |
| [FEAT-056](done/FEAT-056-declared-local-accounts.md) | Declared local accounts | Accounts a deployment declares in a file that always work, so a directory outage is not a lockout and an administrator can exist at all | none — recovered from this estate's ClickHouse, `chi-prod.yaml` | M | `done` |
| [FEAT-053](done/FEAT-053-api-token-for-machine-callers.md) | API token for machine callers | A token an operator mints so a script, a service or an agent queries through the gateway as them, guarded and audited the same way | `Developer/QueryController.LegacyToken`, `Services/ApiAuthService`, `ApiBaseController` | M | `done` |
| [FEAT-060](done/FEAT-060-mcp-endpoint.md) | MCP endpoint | An agent connects to Avior itself over the Model Context Protocol with a FEAT-053 token and reads through the same guard, access table and audit as the console — six tools, no server on the laptop | none — the predecessor predates agents | M | `done` |
| [FEAT-062](done/FEAT-062-mcp-structured-output.md) | MCP structured tool output | Every MCP tool declares an output schema and answers with its data beside the text — rows as arrays, the audit id, numbers with every digit — and a refusal carries none | none — the predecessor predates agents | S | `done` |
| [FEAT-063](done/FEAT-063-service-accounts.md) | Service accounts | An account for an agent that is nobody's person, owned by administration, granted access directly, reading through its own tokens and audited as itself | none — the predecessor's only machine caller borrowed the minting operator's account | M | `done` |
| [FEAT-069](done/FEAT-069-audit-sky.md) | Audit sky | Seven days of submissions as hourly bars above the audit log, allowed and refused apart, with a brush that narrows the log; the same visibility as the log it sits on | none — the predecessor's logs are tables with a date range | S | `done` |

FEAT-009 was the largest thing missing from the earlier plan, and it was missing
because the controller name read like a settings page. It is the write path, and
a large part of why the predecessor is allowed near production at all. The predecessor's
two-stage approval became one: the administrator's decision executed nothing, so
it was a signature rather than a gate.

## Query and export

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-010](todo/FEAT-010-result-export.md) | Result export | Export to Excel, to a ZIP, or to a file server, asynchronously, with a status to poll | `Developer/QueryController.Export/ExportToZip/ExportFileToServer/GetExportFileStatus/File`, `Developer/ExportController.Export`, `Services/ExportService`, EPPlus, SharpCompress, SSH.NET; `ExportFileStatus` | L | `todo` |
| [FEAT-054](done/FEAT-054-table-structure-viewer.md) | Table structure viewer | One table as its columns, indexes, grants and comment, in sections rather than as a script | none — the predecessor shows a table only as the indexes it carries | M | `done` |
| [FEAT-057](done/FEAT-057-vim-keybindings.md) | Vim keybindings | Modal editing in the query editor, off by default, with the mode on screen | none — the predecessor's editor was a plain textarea | S | `done` |
| [FEAT-011](todo/FEAT-011-saved-queries.md) | Saved queries | Save a statement, list them, delete one, with soft delete | `Developer/QueryController.SaveQuery/GetQueryList/DeleteSaveQuery`, `SaveQuery.feature`; `SaveQuery`, `DeletedSaveQuery` | S | `todo` |
| [FEAT-021](todo/FEAT-021-cancel-a-running-statement.md) | Cancel a running statement | Stop a statement already sent, keyed by session | `Developer/QueryController.Cancel`, `Developer/FetchController.Cancel`, `LegacyApplication.SqlCommandCache` | M | `todo` |
| [FEAT-022](todo/FEAT-022-fetch-by-database-name.md) | Fetch by database name | Name a database and let Avior find which instance holds it, instead of picking the instance first | `Developer/FetchController.Index` | S | `todo` |
| [FEAT-023](done/FEAT-023-object-definition-viewer.md) | Object definition viewer | Read the definition of a view, stored procedure, user defined type, function or index, one menu entry per type, through a type allowlist | `Developer/ContentController.Index?type=…/GetContent/GetObjectNameByDatabase` | M | `done` |
| [FEAT-068](done/FEAT-068-plan-cost-gauge.md) | Plan cost against the cap | Inspect and execute answer with the plan's estimated cost and rows beside the caps the guard applied, so the console can draw how close a statement came to being refused | `ExecutionPlanAnalyzer`, which reports only the refusal | S | `done` |

## Console

The redesign of ADR 0009 and the three things it draws that need data of their
own. FEAT-065 changes no request, refusal or audit row. The others add an
endpoint each, filtered by the same access rules as the data they summarise.
FEAT-064 is reserved for OAuth sign-in for MCP clients, whose specification is
kept outside the repository with ADR 0007, so the numbering skips it.
FEAT-070 is the full-page counterpart of FEAT-067's chart, and the first
feature to parse routine bodies (ADR 0010).

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-065](done/FEAT-065-the-console-redesign.md) | The console redesign | The star-chart shell on Bytebase's working furniture: instrument rail, TARGET readout, production beacon, env-tinted editor tabs, connection bar, Result/Plan/Messages, requests and releases as issues, a request read beside its list and a release on its own page (ADR 0012), dark by default, inside a performance budget | none — the predecessor's look is not kept, only its menu | L | `done` |
| [FEAT-066](done/FEAT-066-global-search.md) | Global search | ⌘K finds pages, reachable databases, their tables and views, requests and releases on them, and operators for an account manager; never a database the caller cannot reach | none — neither the predecessor nor Bytebase has a palette | M | `done` |
| [FEAT-067](done/FEAT-067-schema-star-chart.md) | Schema star chart | One database drawn as a constellation of tables and views joined by declared foreign keys and view dependencies, centred on the target, collapsible, never blocking the tree | none — the predecessor lists tables in a dropdown | M | `done` |
| [FEAT-070](done/FEAT-070-lineage.md) | Lineage | Who writes a column and who reads it: an object's neighbourhood of tables, views and routines, catalogue-exact edges solid, edges parsed from routine bodies dashed, dynamic SQL listed and never drawn, and the route between two objects | none — the predecessor shows a procedure's text and nothing about what it touches | L | `done` |
| [FEAT-071](done/FEAT-071-observatory.md) | Observatory | Per database: Avior usage by operator, size and growth, when each table, index and routine was last used with a 30-day flag and date-versioned routines grouped, top and running statements with text behind a permission. History in Avior, realtime in Grafana (ADR 0011) | none — nearest are FEAT-026 and FEAT-030 | L | `done` |
| [FEAT-072](done/FEAT-072-observatory-dashboard.md) | Observatory dashboard | The Observatory's landing view: range, database and refresh toolbar, six KPI cards with deltas and sparklines, eight hand-drawn charts that drill into FEAT-071's tabs, and statements running now | none — builds on FEAT-071 | M | `done` |
| [FEAT-073](done/FEAT-073-routine-timing.md) | Routine timing | Every function and procedure with its calls, mean per call, total and self time over 24 h, 7 d or 30 d, sortable and searchable, versions grouped, the slowest top-level call where one matches; no threshold, operators judge | none — builds on FEAT-071 and FEAT-072 | M | `done` |
| [FEAT-074](done/FEAT-074-loading-states.md) | Loading states and the branded loader | Avior's star and comet as the boot, route and section loader, a redesigned sign-in page, skeletons shaped like every list, and no empty-state text while an answer is pending | none — extends FEAT-065 | M | `done` |
| [FEAT-075](done/FEAT-075-partitioned-tables.md) | Partitioned tables | A partitioned table listed once with its partition count, its partitions read on the table page with bounds, estimates, sizes and warnings, and the plan saying which partitions a statement scans | none — the predecessor predates PostgreSQL | M | `done` |

## Configuration

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-012](todo/FEAT-012-instance-and-database-configuration.md) | Instance and database configuration | Register a governed instance, test the connection, encrypt and decrypt its credentials | `Developer/DbConfigController.CreateUpdate/Delete/TestConnection/FastTestConnection/Encrypt/Decrypt`, `Developer/ServerInstanceController.HealthyCheck`; `DbConfig`, `DbConfigNew` | M | `todo` |
| [FEAT-013](todo/FEAT-013-application-settings.md) | Application settings | Key and value settings, per website and per database | `Developer/SimpleSettingController` CRUD, `Developer/AppSettingController.GetClientNameByDb/GetWebsitesByDb`, `Services/ConfigService`; `SimpleSettings` | S | `todo` |
| [FEAT-018](todo/FEAT-018-scheduled-task-administration.md) | Scheduled task administration | Create, list, update and delete the schedulers' own settings | `Developer/SchedulerSettingController` CRUD, `Developer/TaskController.Index`; `SchedulerSettings` | M | `todo` |
| [FEAT-020](todo/FEAT-020-satan-integration-configuration.md) | Satan integration configuration | Register the Satan system's connections | `Developer/SatanConfigController` CRUD | S | `todo` |
| [FEAT-024](todo/FEAT-024-bifrost-integration.md) | Bifrost integration | Unknown. A table exists in `ApplicationSettingDB` and nothing in the C# reads it | `ApplicationSettingDB/Bifrost` | ? | `todo` |

## Release and deployment

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-017](done/FEAT-017-governed-schema-release.md) | Governed schema release | A migration reaches a governed database through Avior: CI submits it, the drift check proves production has not moved, an approver decides, Avior applies and records | `Developer/SqlCompareController`, `Developer/ApproveMgmtController`, `ReleaseApprover/ReleaseMgmtController`, `Services/DbDeployService`; `DBDeployRecord` | L | `done` |
| [FEAT-037](todo/FEAT-037-staging-to-production-mapping.md) | Staging to production mapping | Which staging instance corresponds to which production one | `Services/StagingProductionServerMappingService` | S | `todo` |
| [FEAT-039](done/FEAT-039-applied-migration-history.md) | Applied migration history | What actually reached each governed database, which file put it there, and who let it in. Reconciles Avior's ledger against the target's own | `Developer/ApproveMgmtController.Index`; `Legacy_Mgmt_GetDBDeployRecordList`, `ByCId`, `ByAccount` | M | `done` |
| [FEAT-059](done/FEAT-059-release-preconditions-and-provisioning.md) | Release preconditions and environment provisioning | Whether the target database and the roles a migration names exist, before it is applied — and creating the ones that are the environment's rather than the schema's | none — the predecessor deployed to databases a DBA had already provisioned outside it | M | `done` |
| [FEAT-061](done/FEAT-061-application-logins.md) | Application logins | A login role for a service, with a generated password shown once and kept nowhere; rotating it; dropping a role Avior created | none — the predecessor never created a database credential | M | `done` |
| [FEAT-076](done/FEAT-076-database-layer-access.md) | Database-layer access | Whether each role a migration grants to may CONNECT to its database, and granting it; whether a database is prepared for the write path, and preparing it with the writer Avior is configured with — no psql, no password | none — the predecessor reached every database through one login | M | `done` |

The order a DBA does these in for a new database, with a check for each step,
is [the new governed database runbook](../runbooks/new-governed-database.md).

## DBA operations

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-016](todo/FEAT-016-database-restore.md) | Database restore | Restore a database from backup | `Developer/RestoreDBController.Restore`, `RestoreFunctionthe predecessor.ps1` | L | `todo` |
| [FEAT-025](todo/FEAT-025-transaction-log-backup.md) | Transaction log backup | Trigger a log backup for one database | `Dba/DatabaseController.LogBackup` | S | `todo` |
| [FEAT-026](todo/FEAT-026-cpu-and-memory-history.md) | CPU and memory history | Per-database resource history, one database or all of them | `Dba/DatabaseController.GetCpuMemoryLogInfo/GetAllCpuMemoryLogInfo` | M | `todo` |
| [FEAT-027](todo/FEAT-027-stored-procedure-review.md) | Stored procedure review | A DBA marks a procedure reviewed | `Legacy_Dba_ReviewStoredProcedure` | S | `todo` |
| [FEAT-030](todo/FEAT-030-instance-health-monitoring.md) | Instance health monitoring | Whether each registered instance answers | `Developer/ServerInstanceController.HealthyCheck`, `Scheduler/ServerInstanceHealthyChecker` | S | `todo` |
| [FEAT-031](todo/FEAT-031-online-index-maintenance.md) | Online index maintenance | Rebuild an index online, with a history of what was rebuilt when | `Dba/ReIndexController.Index/OnlineReIndex`; `ReindexLog`, `Legacy_Mgmt_GetLastReindexActionLog` | M | `todo` |
| [FEAT-032](todo/FEAT-032-dns-mapping-administration.md) | DNS mapping administration | Which name currently points at which instance | `Dba/DnsMappingController.Index/List`, `Scheduler/ActiveDnsMappingScheduler` | M | `todo` |
| [FEAT-033](todo/FEAT-033-database-inventory.md) | Database inventory | The databases on every registered instance, refreshed on a schedule | `Scheduler/DatabaseScheduler` | S | `todo` |
| [FEAT-034](todo/FEAT-034-stored-procedure-inventory.md) | Stored procedure inventory | Every procedure on every instance with its text, refreshed on a schedule | `Scheduler/StoredProcedureScheduler`, `Legacy_Dba_GetStoredProcedures`; `StoredProcedures` | M | `todo` |
| [FEAT-035](todo/FEAT-035-domain-user-synchronisation.md) | Domain user synchronisation | Pull accounts from the directory on a schedule | `Scheduler/DomainUserScheduler`, `DomainUser` | M | `todo` |

## Compliance

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-038](todo/FEAT-038-sensitive-content-scanner.md) | Sensitive content scanner | Scan every server's stored procedure and job definitions for sensitive content on a schedule, and report what was found | `SiteController.SpContent/JobContent`, `Dba/MonitorController.Index`; `AnalyzeSensitiveDbContent`, `AnalyzeSensitiveDbContentTable` | L | `todo` |

The least visible feature in the predecessor's navigation and arguably the most
valuable: it is the only thing that looks for credentials and personal data
*inside* database code, rather than governing reads of the data.

## Cross-cutting

| ID | Feature | What it does Predecessor origin | Size | Status |
| --- | --- | --- | --- | --- | --- |
| [FEAT-050](todo/FEAT-050-slack-notification.md) | Slack notification | Tell a channel when an approval is waiting or a deployment ran | `Services/Slack/SlackClient`, `SlackBotClient` | S | `todo` |
| [FEAT-052](todo/FEAT-052-service-restart.md) | Service restart | Restart the application from the browser | `Administrator/RestartController.Index` | S | `todo` |

---

## The menu, one to one

The authoritative source is `Legacy/Views/Shared/_SideBar.cshtml`. Every entry
below is a real menu item, in menu order, with the FEAT that covers it. Three
groups are gated: Admin on `account.IsAdministrator`, DBA on `account.IsDba`,
and SP Release Approve on `account.IsReleaseApprover`. Restore DB is hidden
entirely when the `isStg` app setting is present, so it does not appear on
staging.

| Menu path | Goes to | FEAT |
| --- | --- | --- |
| Query | `Developer/Query.Index` | FEAT-002, FEAT-008 |
| Update Query | `Developer/Update.Index` | [FEAT-009](done/FEAT-009-governed-write-requests.md) |
| Export Data | `Developer/Export.Export` | [FEAT-010](todo/FEAT-010-result-export.md) |
| Restore DB *(not on staging)* | `Developer/RestoreDb.Index` | [FEAT-016](todo/FEAT-016-database-restore.md) |
| Database Objects → View | `Developer/Content.Index?type=View` | [FEAT-023](done/FEAT-023-object-definition-viewer.md) |
| Database Objects → Stored Procedure | `?type=StoredProcedure` | [FEAT-023](done/FEAT-023-object-definition-viewer.md) |
| Database Objects → User Defined Type | `?type=UserDefinedType` | [FEAT-023](done/FEAT-023-object-definition-viewer.md) |
| Database Objects → Function | `?type=Function` | [FEAT-023](done/FEAT-023-object-definition-viewer.md) |
| Database Objects → Index | `?type=Index` | [FEAT-023](done/FEAT-023-object-definition-viewer.md) |
| Application Setting → Simple Setting | `Developer/SimpleSetting.Index` | [FEAT-013](todo/FEAT-013-application-settings.md) |
| Application Setting → Scheduler Setting | `Developer/SchedulerSetting.Index` | [FEAT-018](todo/FEAT-018-scheduled-task-administration.md) |
| Application Setting → DB Configuration | `Developer/DbConfig.Index` | [FEAT-012](todo/FEAT-012-instance-and-database-configuration.md) |
| Application Setting → Satan Configuration | `Developer/SatanConfig.Index` | [FEAT-020](todo/FEAT-020-satan-integration-configuration.md) |
| Application Setting → Satan MongoDB Configuration | `Developer/SatanMongoDBConfig.Index` | dropped, see below |
| Release → DB Compare | `Developer/ReleaseMgmt.Index` | [FEAT-017](done/FEAT-017-governed-schema-release.md) |
| Release → History Record | `Developer/ApproveMgmt.Index` | [FEAT-039](done/FEAT-039-applied-migration-history.md) |
| Release → SP Release Approve *(release approver)* | `ReleaseApprover/ReleaseMgmt.Index` | [FEAT-017](done/FEAT-017-governed-schema-release.md), one queue |
| Admin → Account Management *(administrator)* | `Administrator/Account.Index` | [FEAT-004](done/FEAT-004-role-administration.md) |
| Admin → Permission Management *(administrator)* | `Administrator/Permission.Index` | [FEAT-055](todo/FEAT-055-column-permission-administration.md) |
| Admin → Log *(administrator)* | `Administrator/Log.Index` | [FEAT-003](done/FEAT-003-audit-trail-and-personal-query-log.md) |
| Admin → Update Query Mgmt *(administrator)* | `Administrator/Update.Index` | [FEAT-009](done/FEAT-009-governed-write-requests.md) |
| Admin → DB Resource Mgmt *(administrator)* | `Administrator/ExecutionPlan.Index` | [FEAT-005](todo/FEAT-005-db-resource-management.md) |
| Admin → Restart *(administrator)* | `Administrator/Restart.Index` | [FEAT-052](todo/FEAT-052-service-restart.md) |
| DBA → DNS Mapping *(dba)* | `Dba/DnsMapping.Index` | [FEAT-032](todo/FEAT-032-dns-mapping-administration.md) |
| DBA → Online Reindex *(dba)* | `Dba/ReIndex.Index` | [FEAT-031](todo/FEAT-031-online-index-maintenance.md) |
| DBA → Update Query Mgmt *(dba)* | `Dba/Update.Index` | [FEAT-009](done/FEAT-009-governed-write-requests.md) |
| DBA → Monitor *(dba)* | `Dba/Monitor.Index` | [FEAT-038](todo/FEAT-038-sensitive-content-scanner.md) |
| DBA → Table Release Approve *(dba)* | `Dba/ApproveMgmt.Index` | [FEAT-017](done/FEAT-017-governed-schema-release.md), one queue |
| Healthy Check | `Developer/ServerInstance.HealthyCheck` | [FEAT-030](todo/FEAT-030-instance-health-monitoring.md) |
| Personal Log | `Developer/PersonalLog.Index` | [FEAT-003](done/FEAT-003-audit-trail-and-personal-query-log.md) |

Four things the menu shows that the first pass had wrong:

- **Database Objects has five types, and `Index` is one of them.** The earlier
  entry said "stored procedure, view, function or job". Job is not in this menu:
  job definitions are read by the sensitive content scanner (FEAT-038), not by
  the object viewer.
- **`Administrator/ExecutionPlan` is labelled "DB Resource Mgmt".** The controller
  name says execution plan; the operators call it resource management, which is
  what FEAT-005 should be called in the UI.
- **"DB Compare" opens `ReleaseMgmt`, not `SqlCompare`.** The page is
  ReleaseMgmt; `SqlCompareController` is the endpoint it calls. So FEAT-017 is a
  page plus an action, and FEAT-039 is its sibling list.
- **There are two separate approval queues.** `Dba/ApproveMgmt` is "Table
  Release Approve" and `ReleaseApprover/ReleaseMgmt` is "SP Release Approve" —
  different roles approving different object kinds. They are one queue here,
  because the split existed to serve two release mechanisms and PostgreSQL has
  one; see FEAT-017.

Not in the menu at all, reached from inside a page or by a scheduler:
FEAT-011 saved queries, FEAT-021 cancel, FEAT-022 fetch by database name,
FEAT-025 log backup, FEAT-026 CPU and memory history, FEAT-027 stored procedure
review, FEAT-033 database inventory, FEAT-034 procedure inventory, FEAT-035
domain user sync, FEAT-037 staging mapping, FEAT-050 Slack, FEAT-053 API
token.

---

## Schema comparison and PostgreSQL deployment

FEAT-017 is the one entry that cannot be ported by reading the C#, because most
of the behaviour is not in the C#. The predecessor shells out to PowerShell, which shells
out to Redgate SQL Compare 15 — Windows only, licensed per seat, and it will
never compare PostgreSQL.

**The pipeline is decided in `docs/adr/0004-postgres-deployment.md`.** In short:
the hub becomes an ephemeral container, CI generates and lints the migration and
applies it to staging, the change reaches Avior with its commit and lint
findings, and Avior checks production for drift, takes the approval, applies and
records. Comparison splits into three layers — object inventory, table schema,
and a drift check — because the versioned naming already makes the first of them
a set difference rather than a diff.

That makes FEAT-017 **smaller** than it looks here: Avior stops comparing and
stops generating scripts, and keeps the governance and the audit trail.

Three questions are still open, all of them for whoever owns the release
process rather than for more reading: whether migrations are generated or
hand-written, whether staging is rebuilt or accumulated, and whether the
inventory comparison also runs the other way to catch hand edits on production.
The real cost is the thirty table types and the thirty-two procedures that take
one; no choice of tool changes that.

---

## Deliberately not ported

Anything dropped goes here with the reason, so its absence is a decision rather
than an oversight.

| The predecessor behaviour | Reason |
| --- | --- |
| Helper stored procedures installed into every governed server's `master` | The predecessor installs versioned procedures (`Legacy_GetDatabaseTableColumnStructure_15.03`, `Legacy_GetDatabaseSynonyms_15.05`, `Legacy_Query_QueryData_15.04`, and others) onto each server it governs and reads schema through them. Avior queries `sys.*` directly, so nothing has to be installed on a server to bring it under governance. That removes a deployment ordering problem, not just code |
| `Microsoft.AnalysisServices.AdomdClient` | No consumer found in the C#. Confirm with the DBA team before deleting the capability outright |
| MongoDB instance configuration, `Developer/SatanMongoDBConfigController` (was FEAT-019) | Avior governs PostgreSQL and only PostgreSQL. One engine means one engine, so Mongo goes with SQL Server. See `docs/adr/0005-avior-governs-postgresql.md` |
| Table release approval and the SP release approval queue, `Dba/ApproveMgmtController` and `ReleaseApprover/ReleaseMgmtController` (were FEAT-036 and FEAT-040) | Two queues existed because tables and stored procedures reached production by different mechanisms. On PostgreSQL both are a migration, so there is one artefact and one queue. Folded into FEAT-017 |
| Stored procedure release, `Developer/ReleaseController` (was FEAT-015) | The predecessor releases a procedure by comparing staging against production and copying the text across. A PostgreSQL function reaches production the same way a table does — as a migration in the ledger — so there is nothing separate left to build. Folded into FEAT-017 |
| Backup tables in a database of their own, the `BKTablesDB` project and the `bktablesdb` special case in `SqlParser.cs` (was FEAT-051) | The idea is kept and the separate database is not. A capture has to commit in the same transaction as the write it reverses, and PostgreSQL has no transaction spanning two databases — a backup one database away needs two-phase commit, which reopens the window the capture exists to close. The predecessor also made the backup database a carve-out, exempting a script that targeted it from approval; Avior takes the useful half and makes it universal, so every write keeps what it overwrote and nothing is exempt. Folded into FEAT-009 |
| Reqnroll `.feature` files | Six existed against 32 controllers. Replaced by the integration suite; see `docs/adr/0002-test-strategy.md` |
