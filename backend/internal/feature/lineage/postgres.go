package lineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// Handles hands out the kept pool on one governed database. pgtarget.Target
// satisfies it.
type Handles interface {
	Handle(ctx context.Context, database string) (*sql.DB, error)
}

// PostgresCatalogue reads what a lineage snapshot is built from, on the
// database's kept pool, inside one read-only transaction so every query sees
// the same catalogue.
type PostgresCatalogue struct {
	handles Handles
}

// NewPostgresCatalogue returns a catalogue reader over the governed server.
func NewPostgresCatalogue(handles Handles) (*PostgresCatalogue, error) {
	if handles == nil {
		return nil, errors.New("lineage: catalogue handles are required")
	}
	return &PostgresCatalogue{handles: handles}, nil
}

// Holds reports whether the server holds database, opening its kept pool. It
// reads no catalogue.
func (c *PostgresCatalogue) Holds(ctx context.Context, database string) error {
	if _, err := c.handles.Handle(ctx, database); err != nil {
		return handleError(database, err)
	}
	return nil
}

// Fingerprint is FEAT-070's DDL probe: a count and an xmin sum over the
// catalogues lineage reads, user objects only. Any DDL that changes lineage
// rewrites a row in one of them, which gives it a new xmin.
func (c *PostgresCatalogue) Fingerprint(ctx context.Context, database string) (string, error) {
	pool, err := c.handles.Handle(ctx, database)
	if err != nil {
		return "", handleError(database, err)
	}
	var fingerprint string
	if err := pool.QueryRowContext(ctx, fingerprintQuery).Scan(&fingerprint); err != nil {
		return "", fmt.Errorf("probe the catalogue of %q: %w", database, pgtarget.Classify(err))
	}
	return fingerprint, nil
}

// Catalogue reads one database's lineage catalogue.
func (c *PostgresCatalogue) Catalogue(ctx context.Context, database string) (_ domain.Catalogue, err error) {
	pool, err := c.handles.Handle(ctx, database)
	if err != nil {
		return domain.Catalogue{}, handleError(database, err)
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return domain.Catalogue{}, fmt.Errorf("acquire a connection on %q: %w", database, pgtarget.Classify(err))
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the connection on %q: %w", database, closeErr)
		}
	}()
	tx, err := pgtarget.BeginReadOnly(ctx, conn)
	if err != nil {
		return domain.Catalogue{}, fmt.Errorf("begin on %q: %w", database, err)
	}
	cat, err := readCatalogue(ctx, tx)
	if err != nil {
		return domain.Catalogue{}, pgtarget.Rollback(tx, fmt.Errorf("read the catalogue of %q: %w", database, err))
	}
	if err := tx.Commit(); err != nil {
		return domain.Catalogue{}, fmt.Errorf("end the catalogue read on %q: %w", database, err)
	}
	return cat, nil
}

func readCatalogue(ctx context.Context, tx *sql.Tx) (domain.Catalogue, error) {
	var cat domain.Catalogue
	var err error
	if cat.Relations, err = readRelations(ctx, tx); err != nil {
		return cat, err
	}
	if cat.Routines, err = readRoutines(ctx, tx); err != nil {
		return cat, err
	}
	if cat.ForeignKeys, err = readForeignKeys(ctx, tx); err != nil {
		return cat, err
	}
	if cat.Triggers, err = readTriggers(ctx, tx); err != nil {
		return cat, err
	}
	if cat.Dependencies, err = readDependencies(ctx, tx); err != nil {
		return cat, err
	}
	if cat.Inheritance, err = readInheritance(ctx, tx); err != nil {
		return cat, err
	}
	return cat, nil
}

// handleError reads a database that could not be opened as one the server does
// not hold, and one Avior does not govern as that, by name.
func handleError(database string, err error) error {
	if errors.Is(err, pgtarget.ErrDatabaseNotGoverned) {
		return fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, database)
	}
	if errors.Is(err, pgtarget.ErrDatabaseUnavailable) {
		return fmt.Errorf("%w: %q: %w", ErrDatabaseUnknown, database, err)
	}
	return fmt.Errorf("open %q: %w", database, err)
}

func each(ctx context.Context, tx *sql.Tx, what, query string, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query %s: %w", what, pgtarget.Classify(err))
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("scan %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s: %w", what, err)
	}
	return nil
}

func readRelations(ctx context.Context, tx *sql.Tx) ([]domain.Relation, error) {
	out := []domain.Relation{}
	err := each(ctx, tx, "relations", relationsQuery, func(rows *sql.Rows) error {
		var r domain.Relation
		var relkind, columns string
		var definition sql.NullString
		if err := rows.Scan(&r.OID, &r.Root, &r.Schema, &r.Name, &relkind, &columns, &definition); err != nil {
			return err
		}
		r.Kind, r.Definition = relationKind(relkind), definition.String
		if err := json.Unmarshal([]byte(columns), &r.Columns); err != nil {
			return fmt.Errorf("columns of %s.%s: %w", r.Schema, r.Name, err)
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func relationKind(relkind string) domain.NodeKind {
	switch relkind {
	case "v":
		return domain.KindView
	case "m":
		return domain.KindMatview
	case "S":
		return domain.KindSequence
	default:
		return domain.KindTable
	}
}

func readRoutines(ctx context.Context, tx *sql.Tx) ([]domain.Routine, error) {
	out := []domain.Routine{}
	err := each(ctx, tx, "routines", routinesQuery, func(rows *sql.Rows) error {
		var r domain.Routine
		var prokind string
		var searchPath sql.NullString
		if err := rows.Scan(&r.OID, &r.Schema, &r.Name, &r.Arguments, &r.ArgCount, &prokind,
			&r.Language, &r.Definition, &r.Atomic, &r.Extension, &searchPath); err != nil {
			return err
		}
		r.Kind = domain.KindFunction
		if prokind == "p" {
			r.Kind = domain.KindProcedure
		}
		if searchPath.Valid {
			r.SearchPath = splitSearchPath(searchPath.String)
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

// splitSearchPath reads proconfig's search_path value, which PostgreSQL stores
// as written: comma separated, a name quoted when it needs to be.
func splitSearchPath(value string) []string {
	out := []string{}
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if len(name) >= 2 && strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
			name = strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func readForeignKeys(ctx context.Context, tx *sql.Tx) ([]domain.ForeignKey, error) {
	out := []domain.ForeignKey{}
	err := each(ctx, tx, "foreign keys", foreignKeysQuery, func(rows *sql.Rows) error {
		var k domain.ForeignKey
		var from, to string
		if err := rows.Scan(&k.From, &k.To, &from, &to, &k.OnDelete, &k.OnUpdate); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(from), &k.FromColumns); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(to), &k.ToColumns); err != nil {
			return err
		}
		out = append(out, k)
		return nil
	})
	return out, err
}

func readTriggers(ctx context.Context, tx *sql.Tx) ([]domain.Trigger, error) {
	out := []domain.Trigger{}
	err := each(ctx, tx, "triggers", triggersQuery, func(rows *sql.Rows) error {
		var tr domain.Trigger
		var tgtype int
		var columns string
		if err := rows.Scan(&tr.Name, &tr.Table, &tr.Function, &tgtype, &columns); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(columns), &tr.Columns); err != nil {
			return err
		}
		tr.Events = triggerEvents(tgtype)
		out = append(out, tr)
		return nil
	})
	return out, err
}

// triggerEvents reads pg_trigger.tgtype's event bits, TRIGGER_TYPE_INSERT to
// TRIGGER_TYPE_TRUNCATE in PostgreSQL's own order.
func triggerEvents(tgtype int) []domain.Event {
	bits := []struct {
		bit   int
		event domain.Event
	}{{4, domain.EventInsert}, {8, domain.EventDelete}, {16, domain.EventUpdate}, {32, domain.EventTruncate}}
	out := []domain.Event{}
	for _, b := range bits {
		if tgtype&b.bit != 0 {
			out = append(out, b.event)
		}
	}
	return out
}

func readDependencies(ctx context.Context, tx *sql.Tx) ([]domain.Dependency, error) {
	out := []domain.Dependency{}
	err := each(ctx, tx, "dependencies", dependenciesQuery, func(rows *sql.Rows) error {
		var d domain.Dependency
		var kind string
		var fromColumn, toColumn, name sql.NullString
		if err := rows.Scan(&kind, &d.From, &fromColumn, &d.FromRoutine,
			&d.To.Relation, &toColumn, &d.To.Routine, &name, &d.Generated); err != nil {
			return err
		}
		d.Kind, d.FromColumn, d.To.Column = domain.EdgeKind(kind), fromColumn.String, toColumn.String
		d.Name = name.String
		out = append(out, d)
		return nil
	})
	return out, err
}

func readInheritance(ctx context.Context, tx *sql.Tx) ([]domain.Inheritance, error) {
	out := []domain.Inheritance{}
	err := each(ctx, tx, "inheritance", inheritanceQuery, func(rows *sql.Rows) error {
		var i domain.Inheritance
		if err := rows.Scan(&i.Child, &i.Parent); err != nil {
			return err
		}
		out = append(out, i)
		return nil
	})
	return out, err
}
