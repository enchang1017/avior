//go:build acceptance && race

package acceptance

// raceDetector reports a run under -race, which instruments the parser's
// WebAssembly compile roughly tenfold. The production binary is never built
// with it, so a budget on that compile is measured only without it.
const raceDetector = true
