package logging

import (
	"encoding/json"
	"sort"
	"strings"
)

// MaxBodyBytes is how much of a body reaches a log line.
//
// A query console answers with rows. Logging all of them would put a result set
// into the log every time somebody reads a table, which costs more to store than
// the rows did and is read by a wider audience than the audit trail allows. The
// first few kilobytes answer what a body log is for -- which call this was and
// what shape came back -- and the rest is the query's own result, which belongs
// where results belong.
const MaxBodyBytes = 4096

// Redacted is what stands in for a value that must not be logged. It is a
// sentence rather than asterisks so that reading the log tells you a value was
// removed on purpose, not that the field was empty.
const Redacted = "[redacted]"

// secretField reports whether a JSON field's name means its value must not be
// logged.
//
// Matched on the name rather than the value, because a value cannot be
// recognised: a password is a string like any other. The list errs toward
// redacting -- a field wrongly hidden costs somebody one question, and a field
// wrongly logged cannot be taken back out of a log aggregator.
func secretField(name string) bool {
	lower := strings.ToLower(name)
	for _, needle := range []string{
		"password", "passwd", "secret", "token", "totp", "credential",
		"apikey", "api_key", "privatekey", "private_key", "authorization", "cookie",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// Body renders a request or response body for a log line.
//
// It returns the empty string for a body that must not be logged at all, which
// the caller writes as no field rather than as an empty one.
//
// JSON is walked and its secret-named fields replaced. Anything that is not
// JSON is NOT logged: this exists to record the shape of an API call, every one
// of which is JSON here, and a body that failed to parse is as likely to be a
// certificate or an upload as it is to be interesting. Guessing would be the
// one case where this leaks something nobody listed.
func Body(contentType string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return ""
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ""
	}
	cleaned, err := json.Marshal(redact(parsed))
	if err != nil {
		return ""
	}
	if len(cleaned) > MaxBodyBytes {
		return string(cleaned[:MaxBodyBytes]) + "…[truncated]"
	}
	return string(cleaned)
}

// redact walks a decoded body and replaces the values of secret-named fields.
//
// Whole subtrees go, not just string leaves: a field called "credentials"
// holding an object is exactly the shape this is for, and keeping its children
// because the field itself was not a string would defeat the rule at the first
// nested case.
func redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for _, key := range sortedKeys(typed) {
			if secretField(key) {
				out[key] = Redacted
				continue
			}
			out[key] = redact(typed[key])
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redact(item)
		}
		return out
	default:
		return value
	}
}

// sortedKeys keeps a rendered body stable between runs, so two log lines for the
// same call can be compared without the map's iteration order making them look
// different.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
