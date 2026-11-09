package observatory

import (
	"testing"
	"time"
)

func TestADatedNameIsAVersionOnlyWhenItsDateIsReal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		base string
		date string
		ok   bool
	}{
		{"shop_settle_order_26_09_19", "shop_settle_order", "2026-09-19", true},
		{"x_00_01_01", "x", "2000-01-01", true},
		{"leap_24_02_29", "leap", "2024-02-29", true},
		{"leap_00_02_29", "leap", "2000-02-29", true},
		{"not_leap_26_02_29", "", "", false},
		{"thirty_first_26_04_31", "", "", false},
		{"month_13_26_13_40", "", "", false},
		{"month_zero_26_00_10", "", "", false},
		{"day_zero_26_01_00", "", "", false},
		{"four_digit_year_2026_09_19", "", "", false},
		{"yyyymmdd_20260919", "", "", false},
		{"_26_09_19", "", "", false},
		{"plain_name", "", "", false},
		{"trailing_26_09_1", "", "", false},
		{"nested_25_01_02_26_09_19", "nested_25_01_02", "2026-09-19", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			base, date, ok := ParseVersion(c.name)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !ok {
				return
			}
			if base != c.base || date.Format(time.DateOnly) != c.date {
				t.Errorf("ParseVersion = (%q, %s), want (%q, %s)", base, date.Format(time.DateOnly), c.base, c.date)
			}
		})
	}
}

func routine(schema, name, signature string, lastUsedDays int, flagged bool) Routine {
	r := Routine{Schema: schema, Name: name, Signature: signature, Flagged: flagged}
	if lastUsedDays >= 0 {
		r.LastUsed = daysBefore(lastUsedDays)
	}
	return r
}

func TestShopSettleVersionsGroupWithTheLatestMarkedAndFlaggedOnItsOwn(t *testing.T) {
	t.Parallel()

	groups := GroupVersions([]Routine{
		routine("public", "shop_settle_order_26_09_21", "", 2, false),
		routine("public", "shop_settle_order_26_09_19", "", 60, true),
		routine("public", "shop_settle_order_26_09_25", "", 45, true),
		routine("public", "shop_settle_order_26_09_23", "", -1, true),
		routine("public", "shop_settle_order_26_09_24", "", 40, true),
		routine("public", "shop_settle_order_26_13_40", "", 1, false),
		routine("public", "unrelated", "", 1, false),
	})
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1: %+v", len(groups), groups)
	}
	group := groups[0]
	if group.Base != "shop_settle_order" || group.Latest != "shop_settle_order_26_09_25" {
		t.Errorf("group = %s latest %s", group.Base, group.Latest)
	}
	want := []string{"_26_09_25", "_26_09_24", "_26_09_23", "_26_09_21", "_26_09_19"}
	if len(group.Versions) != len(want) {
		t.Fatalf("versions = %d, want %d", len(group.Versions), len(want))
	}
	for i, suffix := range want {
		v := group.Versions[i]
		if v.Name != "shop_settle_order"+suffix {
			t.Errorf("version %d = %s, want …%s", i, v.Name, suffix)
		}
		if v.Latest != (i == 0) {
			t.Errorf("%s latest = %v", v.Name, v.Latest)
		}
	}
	if !group.Versions[0].Flagged {
		t.Error("the latest version lost its own flag")
	}
	if group.FlaggedCount != 4 {
		t.Errorf("flagged count = %d, want 4", group.FlaggedCount)
	}
	if group.LastUsed == nil || !group.LastUsed.Equal(*daysBefore(2)) {
		t.Errorf("group last used = %v, want 2 days ago", group.LastUsed)
	}
}

func TestOverloadsAreSeparateRowsOfOneVersionAndAllLatest(t *testing.T) {
	t.Parallel()

	groups := GroupVersions([]Routine{
		routine("public", "calc_26_01_02", "integer", 1, false),
		routine("public", "calc_26_01_01", "integer", 1, false),
		routine("public", "calc_26_01_02", "bigint", 1, false),
	})
	if len(groups) != 1 || len(groups[0].Versions) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	versions := groups[0].Versions
	if versions[0].Signature != "bigint" || versions[1].Signature != "integer" {
		t.Errorf("overloads not ordered by signature: %+v", versions)
	}
	if !versions[0].Latest || !versions[1].Latest || versions[2].Latest {
		t.Errorf("latest = %v %v %v, want both overloads of the newest date", versions[0].Latest, versions[1].Latest, versions[2].Latest)
	}
}

func TestTheGroupKeyIsSchemaAndBase(t *testing.T) {
	t.Parallel()

	groups := GroupVersions([]Routine{
		routine("sales", "report_26_01_01", "", -1, false),
		routine("audit", "report_26_01_02", "", -1, false),
		routine("audit", "report_26_01_01", "", -1, false),
		routine("audit", "other_26_01_01", "", -1, false),
	})
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].Schema != "audit" || groups[0].Base != "other" || groups[1].Base != "report" || groups[2].Schema != "sales" {
		t.Errorf("groups out of order: %+v", groups)
	}
	if groups[2].LastUsed != nil || groups[2].FlaggedCount != 0 {
		t.Errorf("a group never used = %+v", groups[2])
	}
}

func TestNoDatedRoutinesMeansNoGroups(t *testing.T) {
	t.Parallel()

	if groups := GroupVersions([]Routine{routine("public", "plain", "", 1, false)}); len(groups) != 0 {
		t.Errorf("groups = %+v, want none", groups)
	}
}
