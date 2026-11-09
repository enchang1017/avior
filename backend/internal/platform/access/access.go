// Package access holds the refusal every path to a governed database shares.
//
// FEAT-058. Which roles reach which database is one question, asked by the
// schema browser, the object viewer and the query path alike, and answered the
// same way by all three. The error lives here so the transport layer can
// recognise it once rather than three times, and so the three features — which
// may not import each other — say the same thing when they refuse.
package access

import "errors"

// ErrDatabaseNotGranted reports a database the caller's roles do not reach.
//
// Deliberately distinct from a database Avior does not govern. That one is
// structural and applies to everybody; this one is a decision somebody made
// about these roles, and the operator's next move differs: one is a database
// Avior will never open, the other is a database to go and ask for.
var ErrDatabaseNotGranted = errors.New("access: database not granted to these roles")
