package observatory

import "testing"

func TestFlagsAreWithheldWhileAConnectedStandbyIsNotDeclared(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		connected []Standby
		declared  []string
		want      string
	}{
		{"no standby", nil, nil, ""},
		{"no standby and one declared", nil, []string{"replica-1"}, ""},
		{
			"one undeclared",
			[]Standby{{"replica-1", "10.0.0.2"}},
			nil,
			"1 standby is connected that the observatory does not read; flags are withheld",
		},
		{"declared by name", []Standby{{"replica-1", "10.0.0.2"}}, []string{"replica-1"}, ""},
		{"declared by address", []Standby{{"walreceiver", "10.0.0.2"}}, []string{" 10.0.0.2 "}, ""},
		{"declared in another case", []Standby{{"Replica-1", "10.0.0.2"}}, []string{"REPLICA-1"}, ""},
		{
			"a blank declaration matches nothing",
			[]Standby{{"", ""}},
			[]string{"  "},
			"1 standby is connected that the observatory does not read; flags are withheld",
		},
		{
			"two undeclared of three",
			[]Standby{{"a", "1"}, {"b", "2"}, {"c", "3"}},
			[]string{"b"},
			"2 standbys are connected that the observatory does not read; flags are withheld",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Withheld(c.connected, c.declared); got != c.want {
				t.Errorf("Withheld = %q, want %q", got, c.want)
			}
		})
	}
}
