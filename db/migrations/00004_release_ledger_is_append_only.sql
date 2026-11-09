-- +goose Up
-- FEAT-017. The release ledger records what ran against a production database
-- and who let it. Until now it was append-only because no code wrote an UPDATE,
-- which is not the same as being append-only: the property that matters here is
-- that the record cannot be revised after the fact, and nothing enforced it.
--
-- `release.state` is the one legitimate mutation — pending becomes approved
-- becomes applied — so `release` keeps UPDATE and loses only DELETE. Everything
-- else is a statement about something that already happened and cannot become
-- untrue later.

-- +goose StatementBegin
CREATE FUNCTION refuse_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION
        'the release ledger is append-only: % on % is refused', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- The submitted SQL, the drift comparisons, the decisions and the applications.
-- An audit that reads these is reading them because somebody is asking what
-- actually happened, which is exactly when a revised row does the most damage.
CREATE FUNCTION refuse_release_rewrite() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.database IS DISTINCT FROM OLD.database
        OR NEW.commit_sha IS DISTINCT FROM OLD.commit_sha
        OR NEW.merge_request_url IS DISTINCT FROM OLD.merge_request_url
        OR NEW.author IS DISTINCT FROM OLD.author
        OR NEW.migration_checksum IS DISTINCT FROM OLD.migration_checksum
        OR NEW.lint_findings IS DISTINCT FROM OLD.lint_findings
        OR NEW.verify_result IS DISTINCT FROM OLD.verify_result
        OR NEW.staging_synced_at IS DISTINCT FROM OLD.staging_synced_at
        OR NEW.expected_digest IS DISTINCT FROM OLD.expected_digest
        OR NEW.submitted_at IS DISTINCT FROM OLD.submitted_at
    THEN
        RAISE EXCEPTION
            'a submitted release cannot be rewritten: only state may change'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER release_migration_is_append_only
    BEFORE UPDATE OR DELETE ON release_migration
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

CREATE TRIGGER release_drift_check_is_append_only
    BEFORE UPDATE OR DELETE ON release_drift_check
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

CREATE TRIGGER release_approval_is_append_only
    BEFORE UPDATE OR DELETE ON release_approval
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

CREATE TRIGGER release_application_is_append_only
    BEFORE UPDATE OR DELETE ON release_application
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

-- Deleting a release would take its migrations, decisions and applications with
-- it through ON DELETE CASCADE, so the cascade is now unreachable rather than
-- merely unused. That is the intent: an entry that reached a production
-- database is not something anybody removes.
CREATE TRIGGER release_is_never_deleted
    BEFORE DELETE ON release
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

CREATE TRIGGER release_only_state_changes
    BEFORE UPDATE ON release
    FOR EACH ROW EXECUTE FUNCTION refuse_release_rewrite();

-- +goose Down
DROP TRIGGER release_only_state_changes ON release;
DROP TRIGGER release_is_never_deleted ON release;
DROP TRIGGER release_application_is_append_only ON release_application;
DROP TRIGGER release_approval_is_append_only ON release_approval;
DROP TRIGGER release_drift_check_is_append_only ON release_drift_check;
DROP TRIGGER release_migration_is_append_only ON release_migration;
DROP FUNCTION refuse_release_rewrite();
DROP FUNCTION refuse_mutation();
