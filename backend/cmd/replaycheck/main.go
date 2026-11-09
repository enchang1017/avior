// Command replaycheck is ADR-0004's replay gate.
//
//	replay migrations/ into an empty database
//	assert the result equals tables/*.sql
//
// Keeping a declarative file and a migration directory means keeping two
// descriptions of the same thing, and two descriptions drift. This is the one
// check that makes drift impossible to merge, which is why the ADR calls it the
// trust root of the whole arrangement.
//
// It also catches a stale generation: a schema change pushed after the
// migration was written leaves the replayed history no longer matching the
// files, and this fails until it is regenerated. Nothing else needs to detect
// that case.
//
// It does not care who wrote the migration. Choosing a generator is recorded as
// open in ADR-0004, and a hand-written, generated or model-drafted file is
// verifiable here on the same terms.
//
// It lives in Avior for the same reason schemadigest does: the digest is Avior's
// definition, and a second implementation of it would be a second thing to keep
// in step.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/enchang1017/avior/backend/internal/feature/release"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <admin-postgres-dsn> <database-project-directory>\n", os.Args[0])
		os.Exit(64)
	}

	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAIL  %v\n", err)
		os.Exit(1)
	}
	// Written to stdout the way schemadigest writes its digest: this command's
	// output is read by a person or a pipeline, and the platform logger writes
	// structured lines to stderr, which is the wrong shape for both.
	if _, err := io.WriteString(os.Stdout, "PASS  the migration history replays to what tables/ declares\n"); err != nil {
		fmt.Fprintf(os.Stderr, "replaycheck: write the result: %v\n", err)
		os.Exit(1)
	}
}

func run(adminDSN, project string) (err error) {
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("open %s: %w", adminDSN, err)
	}
	defer func() { _ = admin.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Named for the process, so two runs on one server do not collide and a
	// crashed run leaves something identifiable rather than a name the next run
	// silently reuses.
	replayed := fmt.Sprintf("avior_replay_%d", os.Getpid())
	declared := fmt.Sprintf("avior_declared_%d", os.Getpid())
	defer func() { err = errors.Join(err, drop(ctx, admin, replayed, declared)) }()

	replayedDigest, err := build(ctx, admin, adminDSN, replayed,
		filepath.Join(project, "migrations"))
	if err != nil {
		return err
	}
	declaredDigest, err := build(ctx, admin, adminDSN, declared,
		filepath.Join(project, "tables"))
	if err != nil {
		return err
	}

	if replayedDigest != declaredDigest {
		return fmt.Errorf(
			"the migration history and the declarative files describe different schemas.\n"+
				"      replaying migrations/  %s\n"+
				"      applying   tables/     %s\n\n"+
				"      Either a migration is missing for a change made to tables/, or\n"+
				"      tables/ was never updated for a migration that was written",
			replayedDigest, declaredDigest)
	}
	return nil
}

// build creates a database, applies every .sql file in one directory in
// filename order, and returns the digest of what results.
func build(
	ctx context.Context, admin *sql.DB, adminDSN, name, dir string,
) (digest string, err error) {
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+quote(name)); err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}

	db, err := sql.Open("pgx", swapDatabase(adminDSN, name))
	if err != nil {
		return "", fmt.Errorf("open %s: %w", name, err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", name, closeErr)
		}
	}()

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("%s holds no .sql files", dir)
	}
	// Filename order, because that is the order the pipeline applies them in.
	// A directory whose files do not apply in that order is already broken,
	// whatever the two digests then say.
	sort.Strings(files)

	for _, file := range files {
		statements, readErr := os.ReadFile(file)
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", file, readErr)
		}
		if _, execErr := db.ExecContext(ctx, string(statements)); execErr != nil {
			return "", fmt.Errorf(
				"applying %s failed.\n"+
					"      The history has to stand on its own: a directory that begins\n"+
					"      with an ALTER describes a table nobody created.\n\n"+
					"      %w", file, execErr)
		}
	}

	return release.NewPostgresDeployer(db).Digest(ctx, name)
}

func drop(ctx context.Context, admin *sql.DB, names ...string) error {
	var failures error
	for _, name := range names {
		_, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+quote(name)+` WITH (FORCE)`)
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("drop %s: %w", name, err))
		}
	}
	return failures
}

// swapDatabase points a DSN at a different database on the same server.
//
// The path is replaced rather than the DSN re-parsed and rebuilt: a DSN can
// carry credentials and options this command has no business rewriting.
func swapDatabase(dsn, database string) string {
	options := ""
	if at := strings.IndexByte(dsn, '?'); at >= 0 {
		options = dsn[at:]
		dsn = dsn[:at]
	}
	return dsn[:strings.LastIndexByte(dsn, '/')] + "/" + database + options
}

func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
