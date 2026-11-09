package identity

import (
	"strings"
	"unicode"
)

// ServiceAccountPrefix marks the name of an account that is nobody's person.
// FEAT-063.
//
// A prefix rather than a flag alone, because the name is the one thing the
// directory and Avior both see: a person whose directory name carried it could
// otherwise arrive on first sign-in holding a name that reads, everywhere it is
// recorded, as an agent's.
const ServiceAccountPrefix = "svc-"

// maxServiceAccountName is PostgreSQL's identifier length, so a service
// account's name fits wherever a role name does.
const maxServiceAccountName = 63

// IsServiceAccountName reports whether name carries the prefix, compared
// without regard to case or surrounding space.
//
// Lenient on purpose: it is what refuses a directory sign-in, and a refusal
// that a stray capital or space could step around is not one.
func IsServiceAccountName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), ServiceAccountPrefix)
}

// ValidServiceAccountName reports whether name may be given to a new service
// account: the prefix, something after it, and nothing a list or a log would
// render ambiguously.
func ValidServiceAccountName(name string) bool {
	if !IsServiceAccountName(name) || name != strings.TrimSpace(name) {
		return false
	}
	if len(name) <= len(ServiceAccountPrefix) || len(name) > maxServiceAccountName {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
