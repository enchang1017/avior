package api

import "testing"

func TestBrowserRowsDoesNotChangeMachineResults(t *testing.T) {
	rows := [][]any{{int64(9223372036854775807), int64(42), nil}}
	got := browserRows(rows)
	if got[0][0] != "9223372036854775807" || got[0][1] != int64(42) || got[0][2] != nil {
		t.Fatalf("browser rows = %#v", got)
	}
	if rows[0][0] != int64(9223372036854775807) {
		t.Fatal("browser formatting changed the shared execution result")
	}
}

func TestBrowserValue(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		want  any
	}{
		{"positive bigint", int64(9223372036854775807), "9223372036854775807"},
		{"negative bigint", int64(-9223372036854775808), "-9223372036854775808"},
		{"first unsafe positive", int64(9007199254740992), "9007199254740992"},
		{"first unsafe negative", int64(-9007199254740992), "-9007199254740992"},
		{"safe positive", int64(9007199254740991), int64(9007199254740991)},
		{"safe negative", int64(-9007199254740991), int64(-9007199254740991)},
		{"zero", int64(0), int64(0)},
		{"null", nil, nil},
		{"boolean", false, false},
		{"decimal text", "123.4500", "123.4500"},
		{"float", 1.5, 1.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := browserValue(test.value); got != test.want {
				t.Errorf("browserValue(%v) = %v (%T), want %v (%T)", test.value, got, got, test.want, test.want)
			}
		})
	}
}
