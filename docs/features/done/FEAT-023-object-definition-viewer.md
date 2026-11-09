# FEAT-023 — Object definition viewer

Status: `done`
Group: Query and export
Predecessor origin: `Developer/ContentController.Index/GetContent/GetObjectNameByDatabase`,
`Models/Db/{StoredProcedure,View,Function,UserDefinedType,IndexContent}.cs`,
`Database.GetContentProviders`
Size: M

## What an operator can do

Pick a database, pick one of five object types, pick an object, and read its
definition as script. One menu entry per type, as the predecessor has: View, Stored
Procedure, User Defined Type, Function, Index.

Read-only. Nothing here executes the definition it shows.

## Acceptance criteria

- **AC-1** — Only five types are accepted: `view`, `stored_procedure`,
  `user_defined_type`, `function`, `index`. Any other value is refused, on both
  the list request and the definition request.
- **AC-2** — Naming an instance, a database and a type lists the objects of that
  type in that database, sorted by name.
- **AC-3** — For `index` the list is the relations that can **carry** one:
  tables, partitioned tables and materialised views. An index is read through
  the relation that holds it, which is why the predecessor labels that menu "Table". A
  materialised view carries indexes and listing tables alone would leave them
  unreachable — the same shape of omission as AC-11. A partition is left out,
  as FEAT-008's tree leaves it out (FEAT-075): its indexes are attached to its
  parent's, and the parent is where they are read.
- **AC-4** — Naming an object of a listed type returns its definition as text.
- **AC-5** — A view, procedure or function definition is followed by the `GRANT`
  statements that say which principals may use it. Two grantees are excluded,
  both because they are defaults rather than decisions:

  - **`PUBLIC`.** PostgreSQL grants `EXECUTE` on every new routine to it, so
    every routine carries one. An empty grant list therefore does not mean
    nobody may use the object.
  - **The object's owner.** An owner holds every privilege on what it owns by
    definition. `pg_dump` omits the same row for the same reason: it is implied
    by `OWNER TO`, not granted by anyone.

  Listing either buries the grant somebody actually made, which is the whole
  point of showing the list.
- **AC-6** — Each renderable kind of user defined type comes back as its own
  `CREATE` script: a composite type as `CREATE TYPE … AS (…)` carrying every
  attribute's rendered type, a domain as `CREATE DOMAIN … AS` with its
  `NOT NULL`, `DEFAULT` and named constraints, an enum as `AS ENUM (…)` in
  label order, and a range as `AS RANGE (subtype = …)`.

  Re-derived, not translated. SQL Server's table type carried nullability,
  defaults, identity and a primary key; a PostgreSQL composite type carries none
  of those and there is nothing in the catalogue to read them from. What is left
  is the attribute list, and it has to be exact, because a type recreated with
  the wrong attribute types is a different type.
- **AC-7** — A relation is returned as the statements that recreate every index
  it carries: `CREATE INDEX` for a plain one, and
  `ALTER TABLE … ADD CONSTRAINT … PRIMARY KEY` for a constraint-backed one,
  because a primary key cannot be recreated by creating its index. A partial
  index keeps its `WHERE` and a covering index keeps its `INCLUDE`: those are
  the parts a naive reconstruction drops, and they change what the index does.
- **AC-8** — A table with no indexes says it has none. An empty answer is not
  the same as no answer.
- **AC-9** — An object that does not exist is refused **by name**. The predecessor returns
  an empty definition here and the operator cannot tell it apart from an object
  with no content; Avior refuses instead.

  The list predicate and the body predicate are one shared constant, so the two
  can never disagree: anything absent from the list is "not found", and
  "unsupported" means listed but unrenderable. That is the structural fix for
  the predecessor's defect rather than an avoidance of it. It also decides a case SQL
  Server had no equivalent of — an aggregate asked for as a function is **not
  found**, because it was never in the function list.
- **AC-10** — A user defined type with no script to give — a base type from an
  extension, or a shell type, whose definition is C functions in a shared
  library — is refused as unsupported **by name**, rather than answered with an
  empty definition. It stays in the list: The predecessor's defect was not that it
  listed such a type, it was that listing and reading disagreed, so an operator
  picked something and got a blank panel. One predicate decides both here, which
  is the same argument FEAT-008 makes for showing a denied column rather than
  hiding it.

  This criterion narrowed on PostgreSQL. Composite, domain, enum and range all
  render, so SQL Server's case — a scalar alias type — has an answer here.
- **AC-11** — The function list selects on `prokind` alone and never on
  `proretset` or `prorettype`, so a set-returning function is listed and readable
  exactly like a scalar one. Aggregates and window functions are excluded; they
  have no body to read.

  The predecessor lost its inline table-valued function by querying
  `type IN ('TF','FN')` — it filtered on **what the routine returns**. PostgreSQL
  does not encode set-returning-ness in `prokind` at all, so the same mistake
  here would be reintroduced by anyone "optimising" the query with a
  `proretset` predicate. A test asserts neither column appears in it.
- **AC-12** — Reading a definition is audited: who, which instance, database,
  type and object, and when. The row is written before the outcome is known, so
  it records three outcomes rather than two: a hit is found, an absent object is
  a miss, and **a type that exists but cannot be rendered is a hit** — it was
  there, it simply had no script to give.
- **AC-13** — An unauthenticated caller reaches neither the list nor the
  definition.

## What is refused

| Case | Answer |
| --- | --- |
| A type outside the five | `422 content.type_unknown`, naming the value |
| A database the caller cannot reach on that instance | `404 schema.database_unknown` |
| An object absent from the database | `404 content.object_unknown`, naming it (AC-9) |
| A user defined type that is not a table type | `422 content.type_unsupported` (AC-10) |
| No session | `401` (AC-13) |

## What the predecessor does that is not carried over

Four defects found while reading the source. Each is a decision, not an
oversight, and each has an acceptance criterion above.

| The predecessor | Why it is not kept |
| --- | --- |
| A missing object returns `string.Empty` — the `SqlException` matching `"does not exist in database"` is swallowed in all five providers | The operator sees a blank panel and cannot tell "no such object" from "object with no body". AC-9 |
| The user defined type list is `sys.types WHERE is_user_defined = 1`, but `GetContent` reads `sys.table_types` | A scalar alias type is listed and then returns nothing. AC-10 |
| The function list is `type IN ('TF','FN')` | `IF`, an inline table-valued function, is missing from the list and unreachable. AC-11 |
| The database is one string, `"name (server)"`, split back apart with `([\w|-]+)\s\(.*\)` | A database or instance name holding a space, a dot or a bracket parses wrongly and silently addresses something else. Avior carries the instance and the database as separate fields |

Two more, kept as they are:

- `contentProvider.Name = "[" + model.Name + "]"` wraps the name in brackets with
  no escaping. The value reaches SQL Server as a parameter, so this is not an
  injection, but a name containing `]` addresses the wrong object. Avior matches
  on the bare name and quotes only when it renders.

The predecessor's inconsistent use of `WITH (NOLOCK)` across the five catalogue queries
was recorded here as something Avior would make uniform. ADR-0005 deletes the
rule: a PostgreSQL reader does not block a writer, so a catalogue read needs no
hint at all.

## Where the definitions come from

| Type | Listed from | Definition from |
| --- | --- | --- |
| Stored procedure | `pg_proc WHERE prokind = 'p'` | `pg_get_functiondef` |
| View | `pg_class WHERE relkind IN ('v','m')` | `pg_get_viewdef` |
| Function | `pg_proc WHERE prokind = 'f'` | `pg_get_functiondef` |
| User defined type | `pg_type`, by `typtype` | rendered per kind: composite, domain, enum, range |
| Index | `pg_class` relations that can carry one | `pg_get_indexdef`, and `ALTER TABLE … ADD CONSTRAINT` for a constraint-backed index |

Four of the five come from PostgreSQL's own deparsers, which is a real
difference from the predecessor: `sp_helptext` returns the stored source text, while
`pg_get_functiondef` and `pg_get_viewdef` regenerate it from the parse tree. The
script that comes back is therefore normalised — comments and original
formatting are gone, and an identifier may be quoted differently from how it was
typed. It is correct and it will recreate the object; it is not a transcript of
what the author wrote.

Type rendering is the exception and is Avior's own, because PostgreSQL has no
deparser for a type definition.

## What gets audited

Every definition read: caller, instance, database, type, object name, and
whether it succeeded. The predecessor audits none of this — `BaseController` logs
exceptions only — so this is new. A definition is the shape of the system, and
who read which one is worth keeping.

## Integration cases

`backend/test/acceptance/objects_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a type outside the allowlist is refused on both endpoints` |
| AC-2 | `each type lists its own objects in name order` |
| AC-3 | `the index list is tables` |
| AC-4 | `a procedure returns its body` |
| AC-5 | `a definition carries the grants on it, without public` |
| AC-6 | `a table type is rendered as CREATE TYPE with its key` |
| AC-7 | `a table returns its index statements` |
| AC-8 | `a table with no indexes says so` |
| AC-9 | `an absent object is refused by name` |
| AC-10 | `a user defined type that is not a table type is refused as unsupported` |
| AC-11 | `an inline table-valued function is listed and readable` |
| AC-12 | `reading a definition is audited, hit and miss alike` |
| AC-13 | `an unauthenticated caller reaches neither endpoint` |

The pure parts — the type allowlist, the CREATE TYPE and CREATE INDEX
rendering, the no-index case — also carry domain tests in
`backend/internal/feature/objects/`, because they need no database and a
rendering bug should not need a container to find.

## Outstanding

- **The storage-options gap SQL Server had is closed.** That entry recorded
  `PAD_INDEX`, `FILLFACTOR` and `DATA_COMPRESSION` being dropped by the predecessor's
  generated script. `pg_get_indexdef` deparses the real index, and the partial
  predicate and `INCLUDE` list — the parts a reconstruction loses — come back
  verbatim. Nothing here is known to be missing.
- `objects` imports `schema.RenderType` so a type renders identically in both
  places. That is one feature package importing another, which the linter
  permits today and probably should not. The function belongs in
  `internal/platform`; moving it means touching FEAT-008 and was left out of
  this change.
- **Are definitions permission-gated?** the predecessor shows any object in any database
  the caller can reach. FEAT-004 gates columns, not definitions, and a procedure
  body can name columns the caller may not read. Left as the predecessor has it for now;
  raise it when FEAT-004 lands.
- Index definitions carry no `GRANT` statements in the predecessor. Whether that is
  deliberate or an omission has not been established.
- **An overloaded routine returns every definition it has.** PostgreSQL matches
  a routine by name and argument types, and the viewer addresses one by name
  alone. Two functions sharing a name currently come back concatenated. Nobody
  has said whether the operator should pick a signature or read them together.
- **Extension-owned functions are in the function list.** Installing `pgcrypto`
  adds a few dozen entries an operator did not write and cannot change. Whether
  to filter on `pg_depend` and hide them is unanswered; hiding them would also
  hide the fact that they exist.
- **The definition is regenerated, not the author's text.** `pg_get_functiondef`
  and `pg_get_viewdef` deparse from the parse tree, so comments and original
  formatting are gone. The predecessor's `sp_helptext` returned the stored source. This
  is a visible change for anyone used to reading their own comments back, and it
  is not recoverable — PostgreSQL does not keep the source.
