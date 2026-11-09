package sqlguard

import (
	"fmt"
	"sort"
	"strings"
)

// Decision is the outcome of the guard pipeline.
type Decision string

const (
	// Allow means every phase passed and the statement may execute.
	Allow Decision = "allow"
	// Deny means at least one rule fired. Reasons is never empty.
	Deny Decision = "deny"
)

// Rule identifies which guard rule produced a reason. Values are stable and
// part of the HTTP contract: they appear verbatim in GuardReason.rule.
type Rule string

// The complete set of rules. Each value is part of the HTTP contract and each
// is documented as an operator-visible refusal in
// docs/features/FEAT-002-guarded-query-execution.md, or for the write rules in
// FEAT-009.
const (
	RuleParseFailed       Rule = "parse_failed"
	RuleNotReadOnly       Rule = "statement_not_read_only"
	RuleWildcardSelect    Rule = "wildcard_select"
	RuleColumnDenied      Rule = "column_denied"
	RuleTableDenied       Rule = "table_denied"
	RuleDatabaseDenied    Rule = "database_denied"
	RuleCostLimitExceeded Rule = "cost_limit_exceeded"
	RuleRowLimitExceeded  Rule = "row_limit_exceeded"
	RuleUnsupportedSyntax Rule = "unsupported_syntax"
	RuleOpaqueFunction    Rule = "opaque_function"

	// FEAT-009, the write path.
	RuleNotSingleWrite    Rule = "statement_not_single_write"
	RuleUnsupportedWrite  Rule = "unsupported_write_syntax"
	RuleMissingPredicate  Rule = "missing_where_clause"
	RuleMultipleTables    Rule = "multiple_tables"
	RuleIrreversibleWrite Rule = "irreversible_write"
	RuleServerTooOld      Rule = "server_version_unsupported"

	// FEAT-006, decided before the guard runs, as RuleDatabaseDenied is.
	RuleRateLimited Rule = "rate_limited"
)

// Rules returns every rule this package can produce, in a stable order.
//
// It exists so the HTTP contract's enum can be checked against the guard rather
// than kept in step by hand: the two are the same fact written twice, and the
// half that drifts is the one the frontend renders on.
func Rules() []Rule {
	return []Rule{
		RuleParseFailed, RuleNotReadOnly, RuleWildcardSelect,
		RuleColumnDenied, RuleTableDenied, RuleDatabaseDenied,
		RuleCostLimitExceeded, RuleRowLimitExceeded,
		RuleUnsupportedSyntax, RuleOpaqueFunction,
		RuleNotSingleWrite, RuleUnsupportedWrite, RuleMissingPredicate,
		RuleMultipleTables, RuleIrreversibleWrite, RuleServerTooOld,
		RuleRateLimited,
	}
}

// Reason explains one rule firing. Subject names the table, column, or token
// the rule fired on, and is empty when the rule is not about a single object.
type Reason struct {
	Rule    Rule
	Message string
	Subject string
}

func (r Reason) String() string {
	if r.Subject == "" {
		return fmt.Sprintf("%s: %s", r.Rule, r.Message)
	}
	return fmt.Sprintf("%s: %s (%s)", r.Rule, r.Message, r.Subject)
}

// Verdict is the guard's answer for one statement.
//
// The zero Verdict is not valid. Build one with [Allowed] or [Denied] so the
// invariant between Decision and Reasons cannot be broken by construction.
type Verdict struct {
	Decision          Decision
	Reasons           []Reason
	ReferencedTables  []string
	ReferencedColumns []string
	EstimatedCost     *float64
	EstimatedRows     *float64
	// Limits are the caps this statement was held to. FEAT-068: present on
	// every verdict, planned or not, so an operator reads the cap beside the
	// estimate instead of only inside a refusal.
	Limits Limits
}

// Allowed returns a passing verdict carrying the objects the statement touched.
func Allowed(tables, columns []string) Verdict {
	return Verdict{
		Decision:          Allow,
		Reasons:           []Reason{},
		ReferencedTables:  normaliseSet(tables),
		ReferencedColumns: normaliseSet(columns),
	}
}

// Denied returns a failing verdict. It panics when given no reasons: a deny
// the operator cannot act on is a bug, and this package must never emit one.
func Denied(reasons ...Reason) Verdict {
	if len(reasons) == 0 {
		panic("sqlguard: Denied called with no reasons")
	}
	return Verdict{
		Decision:          Deny,
		Reasons:           reasons,
		ReferencedTables:  []string{},
		ReferencedColumns: []string{},
	}
}

// Denyf is Denied for the common single-reason case.
func Denyf(rule Rule, subject, format string, args ...any) Verdict {
	return Denied(Reason{Rule: rule, Message: fmt.Sprintf(format, args...), Subject: subject})
}

// WithEstimates attaches plan estimates, returning a copy.
func (v Verdict) WithEstimates(cost, rows float64) Verdict {
	v.EstimatedCost = &cost
	v.EstimatedRows = &rows
	return v
}

// WithLimits attaches the caps the statement was held to, returning a copy.
func (v Verdict) WithLimits(limits Limits) Verdict {
	v.Limits = limits
	return v
}

// IsAllowed reports whether the statement may execute.
func (v Verdict) IsAllowed() bool { return v.Decision == Allow }

// Valid reports whether the Decision and Reasons agree. Tests assert this on
// every verdict the package can produce.
func (v Verdict) Valid() bool {
	switch v.Decision {
	case Allow:
		return len(v.Reasons) == 0
	case Deny:
		return len(v.Reasons) > 0
	default:
		return false
	}
}

func (v Verdict) String() string {
	if v.IsAllowed() {
		return string(Allow)
	}
	parts := make([]string, 0, len(v.Reasons))
	for _, r := range v.Reasons {
		parts = append(parts, r.String())
	}
	return string(Deny) + " [" + strings.Join(parts, "; ") + "]"
}

func normaliseSet(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}
