//go:build acceptance && !race

package acceptance

// raceDetector reports a run under -race; see race_on_test.go.
const raceDetector = false
