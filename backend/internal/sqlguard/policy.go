package sqlguard

import (
	"strconv"
	"strings"
)

// Limits are the pre-execution thresholds the guard applies to a plan. Unlike a
// statement timeout they reject before the server does any work.
//
// A zero field disables that limit. Both zero means cost is not gated at all,
// which is only ever correct in tests.
type Limits struct {
	MaxSubtreeCost   float64
	MaxEstimatedRows float64
}

// Grant is the effective permission set for one caller against one database.
// Denials win: a column present in DeniedColumns is refused whatever else the
// caller holds.
//
// Keys are matched case-insensitively. Column keys are schema.table.column and
// table keys are schema.table.
type Grant struct {
	DeniedTables  []string
	DeniedColumns []string
}

// NewGrant builds a Grant with its keys normalised for matching.
func NewGrant(deniedTables, deniedColumns []string) Grant {
	return Grant{
		DeniedTables:  lowerAll(deniedTables),
		DeniedColumns: lowerAll(deniedColumns),
	}
}

// TableDenied reports whether the caller is refused this schema.table.
func (g Grant) TableDenied(name string) bool {
	return contains(g.DeniedTables, strings.ToLower(name))
}

// ColumnDenied reports whether the caller is refused this
// schema.table.column. Exported so the schema browser can mark a column as
// unreadable before the operator selects it, rather than letting them build a
// statement that is certain to be refused.
func (g Grant) ColumnDenied(name string) bool {
	return contains(g.DeniedColumns, strings.ToLower(name))
}

// checkPlan applies the grant and the limits to a compiled plan, returning
// every reason that fired. An empty result means the plan passed.
func checkPlan(plan *Plan, grant Grant, limits Limits) []Reason {
	reasons := make([]Reason, 0, 4)
	reasons = append(reasons, checkOpaqueFunctions(plan)...)
	reasons = append(reasons, checkGrant(plan, grant)...)
	reasons = append(reasons, checkLimits(plan, limits)...)
	return reasons
}

// checkOpaqueFunctions refuses a statement invoking a routine the plan does not
// account for. It is this package's fail-closed rule applied to the second
// phase, and it exists because the first phase's version was not enough.
//
// The guard checks column permissions against the plan, because a view resolves
// to columns the text never names. A function does not resolve at all unless
// the optimiser inlines it: a call the planner will not inline reports the
// columns of whatever else the statement touched and none of what the function
// body read. The denial then has nothing to fire on and the statement is
// allowed, which is how a routine reading a denied column returns it.
//
// So the answer is not to identify what the function reads — the plan cannot
// say — but to refuse to decide. Anything this package cannot fully account for
// is denied, in phase two exactly as in phase one.
//
// The cost is real and is stated in FEAT-002: an operator cannot call a routine
// of their own through the gateway. That is a refusal they can see, which is
// the trade being made against a read they could not.
func checkOpaqueFunctions(plan *Plan) []Reason {
	reasons := make([]Reason, 0, len(plan.OpaqueFunctions))
	for _, name := range plan.OpaqueFunctions {
		reasons = append(reasons, Reason{
			Rule:    RuleOpaqueFunction,
			Message: "the guard cannot see what this routine reads, so it will not run the statement",
			Subject: name,
		})
	}
	return reasons
}

func checkGrant(plan *Plan, grant Grant) []Reason {
	var reasons []Reason
	reported := make(map[string]bool)

	for _, ref := range plan.Columns {
		for _, governed := range plan.lineage(ref) {
			reasons = append(reasons, grantReasons(governed, grant, reported)...)
		}
	}
	for _, rel := range plan.Relations {
		for _, governed := range plan.relationLineage(rel) {
			reasons = append(reasons, tableReasons(governed.Qualified(), grant, reported)...)
		}
	}
	return reasons
}

// tableReasons is the table denial on its own, for a relation the plan reads
// whether or not any of its columns is read.
func tableReasons(table string, grant Grant, reported map[string]bool) []Reason {
	key := "table:" + strings.ToLower(table)
	if !grant.TableDenied(table) || reported[key] {
		return nil
	}
	reported[key] = true
	return []Reason{{
		Rule:    RuleTableDenied,
		Message: "caller is not permitted to read this table",
		Subject: table,
	}}
}

// grantReasons is what the grant says about one column on one table, each
// subject reported once however many partitions led to it.
func grantReasons(ref ColumnRef, grant Grant, reported map[string]bool) []Reason {
	reasons := tableReasons(ref.TableName(), grant, reported)

	column := ref.Qualified()
	if key := "column:" + strings.ToLower(column); grant.ColumnDenied(column) && !reported[key] {
		reported[key] = true
		reasons = append(reasons, Reason{
			Rule:    RuleColumnDenied,
			Message: "caller is not permitted to read this column",
			Subject: column,
		})
	}
	return reasons
}

func checkLimits(plan *Plan, limits Limits) []Reason {
	var reasons []Reason
	if limits.MaxSubtreeCost > 0 && plan.SubtreeCost > limits.MaxSubtreeCost {
		reasons = append(reasons, Reason{
			Rule: RuleCostLimitExceeded,
			Message: "estimated plan cost " + formatFloat(plan.SubtreeCost) +
				" exceeds the limit of " + formatFloat(limits.MaxSubtreeCost),
		})
	}
	if limits.MaxEstimatedRows > 0 && plan.EstimatedRows > limits.MaxEstimatedRows {
		reasons = append(reasons, Reason{
			Rule: RuleRowLimitExceeded,
			Message: "estimated row count " + formatFloat(plan.EstimatedRows) +
				" exceeds the limit of " + formatFloat(limits.MaxEstimatedRows),
		})
	}
	return reasons
}

func planTables(plan *Plan) []string {
	out := make([]string, 0, len(plan.Columns)+len(plan.Relations))
	for _, ref := range plan.Columns {
		out = append(out, plan.governing(ref).TableName())
	}
	for _, rel := range plan.Relations {
		chain := plan.relationLineage(rel)
		out = append(out, chain[len(chain)-1].Qualified())
	}
	return normaliseSet(out)
}

func planColumns(plan *Plan) []string {
	out := make([]string, 0, len(plan.Columns))
	for _, ref := range plan.Columns {
		out = append(out, plan.governing(ref).Qualified())
	}
	return normaliseSet(out)
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, strings.ToLower(s))
		}
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
