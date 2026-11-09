-- +goose Up
-- FEAT-017 AC-22. Where a partially-applied release left the target.
--
-- A failed apply can be run again, and the files that landed are skipped by the
-- target's own ledger. The drift check could not be: it compares the target
-- against expected_schema_digest, the state the migration was generated
-- against — and after a partial apply the target is no longer in that state,
-- because this release moved it. Every retry was refused as drift, by the very
-- change it had just made.
--
-- Skipping the check on a retry would have answered that and given up what the
-- check is for: production can move between a failure and a retry, and a hand
-- edit in that window is exactly what AC-6 exists to catch.
--
-- So the digest the target held when this entry stopped is recorded here, and a
-- retry compares against that. Movement this release caused is accounted for;
-- movement anybody else caused is still refused.
--
-- Empty until an apply fails with something applied. A release that has never
-- been applied, or whose first file failed, has moved nothing and compares
-- against expected_schema_digest as before.
-- Deliberately outside refuse_release_rewrite (00004). That guard names the
-- columns a submitted release may not change, so this one is writable by having
-- been left out -- which is what it needs, and which is also worth saying out
-- loud: the guard is a deny-list, so a column added later is mutable unless
-- somebody adds it. This one is meant to be. The next one may not be.
ALTER TABLE release ADD COLUMN resume_digest text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE release DROP COLUMN resume_digest;
