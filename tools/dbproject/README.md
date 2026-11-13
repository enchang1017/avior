# tools/dbproject — a stand-in database project

Steps 1–5 of the release pipeline belong to the project that holds the schema
files, not to Avior. `docs/adr/0004-postgres-deployment.md` draws that line, and
`docs/features/inprogress/FEAT-017-governed-schema-release.md` implements the
half on this side of it.

This directory is the smallest thing that can play the other half, so the
acceptance suite and a person trying the pipeline by hand both have a real
submission to work with rather than a JSON body somebody typed.

```
tables/       declarative — the desired shape
migrations/   what has been generated, in order
submit.sh     compute the digest and the checksum, POST the ledger entry
```

## What it is not

It does not generate a migration and it does not lint. A real database project
does both with `tools/release-kit`; here the migration is written by hand, the
point is what happens after it exists, and `submit.sh` sends whatever `lint.json`
holds so the approval screen can be seen with real findings on it.

## Using it

```
./submit.sh <avior-url> <release-token> <target-database>
```

It reads every file under `migrations/`, in name order, computes the digest of
the target as it currently stands, and posts the entry. The digest has to be
computed the same way on both sides or the drift check compares two unrelated
numbers — that shared definition is the one real coupling between this directory
and Avior, and it is stated in `internal/feature/release/digest.go`.
