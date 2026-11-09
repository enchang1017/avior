// Package sqlguard decides whether a statement submitted by an operator may
// run against a governed database.
//
// The guard runs in two phases, and the split is deliberate.
//
// Phase one is static and lives entirely in this package: tokenise the text,
// confirm it is a single read-only statement, and reject constructs that could
// write, escalate, or smuggle a second statement. It extracts candidate table
// names but deliberately does not try to resolve columns.
//
// Phase two asks the target server for an estimated execution plan and reads
// the resolved object and column references out of it, along with cost and row
// estimates. The plan, not the parser, is the authority on which columns a
// statement touches, so column permissions are enforced against the plan.
//
// That division is what keeps a hand-written parser viable. The parser only has
// to be a conservative gate, never a complete PostgreSQL implementation.
//
// # The fail-closed invariant
//
// Any input the parser cannot fully account for is denied, never allowed. A
// token it cannot classify, a construct it does not model, an unbalanced
// bracket, a keyword outside the modelled subset: all produce a deny with rule
// [RuleUnsupportedSyntax].
//
// This is the single most important property in the package. An incomplete
// parser that fails open is not a weaker gate, it is no gate at all: whatever
// syntax it forgot becomes the bypass. Tests in this package assert the
// invariant directly, and the fuzz targets exist to find inputs that break it.
// Do not add a default branch that returns allow.
package sqlguard
