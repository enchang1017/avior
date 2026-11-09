// Package pgparse reads routine bodies and view definitions with libpg_query
// (ADR 0010) and reports what they say, syntactically. It resolves nothing and
// decides nothing: that is internal/platform/lineage's.
//
// A body the parser rejects anywhere is reported unparsed as a whole, never in
// part, because a body read halfway would draw the half that parsed as if it
// were everything the routine does.
package pgparse

import (
	"errors"
	"fmt"
	"strings"

	pgquery "github.com/wasilibs/go-pgquery"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

type unparsedError struct{ message string }

func (e unparsedError) Error() string { return e.message }

func parseError(err error) error {
	return unparsedError{message: err.Error()}
}

// Warm compiles the parser module now, so the first snapshot build does not
// pay for it. The library compiles it once per process whether or not this is
// called.
func Warm() error {
	if _, err := pgquery.Parse("SELECT 1"); err != nil {
		return fmt.Errorf("pgparse: warm the parser: %w", err)
	}
	return nil
}

// ParseRoutine reads one pg_get_functiondef text in its language. libpg_query
// aborts on some inputs its own grammar accepted, which surfaces as a panic
// out of the WebAssembly module; that body is reported unparsed rather than
// taking the snapshot build down with it.
func ParseRoutine(definition, language string) (body lineage.Body, err error) {
	defer func() {
		if trap := recover(); trap != nil {
			body, err = lineage.Body{Status: lineage.StatusUnparsed, Message: fmt.Sprintf("the parser failed: %v", trap)}, nil
		}
	}()
	switch strings.ToLower(language) {
	case "plpgsql":
		body, err = parsePlpgsql(definition)
	case "sql":
		body, err = parseSQLRoutine(definition)
	default:
		return lineage.Body{Status: lineage.StatusOpaque, Message: "language " + language + " is not read"}, nil
	}
	var unparsed unparsedError
	if errors.As(err, &unparsed) {
		return lineage.Body{Status: lineage.StatusUnparsed, Message: unparsed.message}, nil
	}
	if err != nil {
		return lineage.Body{}, err
	}
	body.Status = lineage.StatusParsed
	return body, nil
}
