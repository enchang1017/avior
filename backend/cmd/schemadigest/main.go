// Command schemadigest prints the schema digest of one database.
//
// It exists so a database project's CI can compute the digest Avior will compare
// against, using Avior's own definition rather than a second implementation of
// it. Two implementations of a hash are two things to keep in step, and the
// drift check is meaningless the moment they disagree — it would report drift on
// a database nobody had touched, which teaches people to click through it.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/enchang1017/avior/backend/internal/feature/release"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <postgres-dsn>\n", os.Args[0])
		os.Exit(64)
	}

	if err := run(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "schemadigest: %v\n", err)
		os.Exit(1)
	}
}

func run(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", dsn, err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database, err := currentDatabase(ctx, db)
	if err != nil {
		return err
	}

	digest, err := release.NewPostgresDeployer(db).Digest(ctx, database)
	if err != nil {
		return err
	}
	// Written to stdout rather than logged. This command's entire output is the
	// digest, read by a shell that captures it; a log line would be the wrong
	// shape and the platform logger writes to stderr.
	if _, err := io.WriteString(os.Stdout, digest+"\n"); err != nil {
		return fmt.Errorf("write the digest: %w", err)
	}
	return nil
}

// currentDatabase asks the connection which database it is on, rather than
// parsing the DSN. A DSN can name a database in more than one place and a
// parser that disagreed with the server would digest the wrong one.
func currentDatabase(ctx context.Context, db *sql.DB) (string, error) {
	var name string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return "", fmt.Errorf("read the current database: %w", err)
	}
	return name, nil
}
