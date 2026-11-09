package ratelimit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var epoch = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func at(seconds float64) time.Time {
	return epoch.Add(time.Duration(seconds * float64(time.Second)))
}

func budget(t *testing.T, statements int, window time.Duration) Budget {
	t.Helper()
	b, err := NewBudget(statements, window)
	if err != nil {
		t.Fatalf("NewBudget(%d, %s): %v", statements, window, err)
	}
	return b
}

func TestNewBudgetRefusesWhatWouldNotLimit(t *testing.T) {
	cases := []struct {
		name       string
		statements int
		window     time.Duration
		wantErr    bool
	}{
		{"one per second", 1, time.Second, false},
		{"thirty per minute", 30, time.Minute, false},
		{"zero statements refuses everybody", 0, time.Minute, true},
		{"negative statements", -1, time.Minute, true},
		{"zero window admits everybody", 30, 0, true},
		{"negative window", 30, -time.Second, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := NewBudget(c.statements, c.window)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err == nil && (b.Statements() != c.statements || b.Window() != c.window) {
				t.Errorf("budget = %d per %s, want %d per %s",
					b.Statements(), b.Window(), c.statements, c.window)
			}
		})
	}
}

func TestDecideAdmitsUpToTheBudgetInsideTheWindow(t *testing.T) {
	b := budget(t, 3, time.Minute)
	cases := []struct {
		name     string
		admitted []time.Time
		now      time.Time
		want     bool
	}{
		{"nothing admitted yet", nil, at(0), true},
		{"one short of the budget", []time.Time{at(0), at(1)}, at(2), true},
		{"the budget spent", []time.Time{at(0), at(1), at(2)}, at(3), false},
		{"spent in one instant", []time.Time{at(5), at(5), at(5)}, at(5), false},
		{"the oldest aged out exactly at the window", []time.Time{at(0), at(1), at(2)}, at(60), true},
		{
			"the oldest a nanosecond short of aging out",
			[]time.Time{at(0), at(1), at(2)},
			at(60).Add(-time.Nanosecond), false,
		},
		{"all aged out long ago", []time.Time{at(0), at(1), at(2)}, at(3600), true},
		{"aged-out rows are not counted", []time.Time{at(0), at(0), at(0), at(61), at(62)}, at(63), true},
		{
			"more than the budget inside, as after lowering it",
			[]time.Time{at(10), at(11), at(12), at(13)},
			at(14), false,
		},
		{"order passed in does not matter", []time.Time{at(2), at(0), at(1)}, at(3), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := b.Decide(c.admitted, c.now)
			if got.Admitted != c.want {
				t.Fatalf("Admitted = %v, want %v", got.Admitted, c.want)
			}
			if got.Admitted && got.RetryAfter != 0 {
				t.Errorf("admitted with RetryAfter %s; an admission has no wait", got.RetryAfter)
			}
			if !got.Admitted && got.RetryAfter <= 0 {
				t.Errorf("refused with RetryAfter %s; a refusal names a wait", got.RetryAfter)
			}
			if got.Budget != b {
				t.Errorf("decision carries budget %+v, want %+v", got.Budget, b)
			}
		})
	}
}

func TestDecideNamesTheWaitUntilOneSlotFrees(t *testing.T) {
	b := budget(t, 3, time.Minute)
	cases := []struct {
		name     string
		admitted []time.Time
		now      time.Time
		want     time.Duration
	}{
		{"until the oldest ages out", []time.Time{at(0), at(10), at(20)}, at(30), 30 * time.Second},
		{"unsorted input", []time.Time{at(20), at(0), at(10)}, at(30), 30 * time.Second},
		{
			"a nanosecond before the oldest ages out",
			[]time.Time{at(0), at(1), at(2)},
			at(60).Add(-time.Nanosecond), time.Nanosecond,
		},
		{
			"over budget: until enough age out to leave one slot",
			[]time.Time{at(0), at(10), at(20), at(30)},
			at(40), 30 * time.Second,
		},
		{
			"a row from a clock ahead waits no longer than the window",
			[]time.Time{at(100), at(100), at(100)},
			at(50), time.Minute,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := b.Decide(c.admitted, c.now)
			if got.Admitted {
				t.Fatal("admitted; the case is a refusal")
			}
			if got.RetryAfter != c.want {
				t.Errorf("RetryAfter = %s, want %s", got.RetryAfter, c.want)
			}
		})
	}
}

func TestWaitingTheNamedTimeIsAdmitted(t *testing.T) {
	b := budget(t, 2, 10*time.Second)
	admitted := []time.Time{at(0), at(3)}
	refused := b.Decide(admitted, at(4))
	if refused.Admitted {
		t.Fatal("the third inside the window was admitted")
	}
	if b.Decide(admitted, at(4).Add(refused.RetryAfter)).Admitted != true {
		t.Errorf("waiting %s was not enough", refused.RetryAfter)
	}
	if b.Decide(admitted, at(4).Add(refused.RetryAfter-time.Nanosecond)).Admitted {
		t.Errorf("a nanosecond short of %s was admitted; the wait overstates nothing", refused.RetryAfter)
	}
}

func TestSinceIsTheStartOfTheWindow(t *testing.T) {
	b := budget(t, 1, time.Minute)
	if got := b.Since(at(90)); !got.Equal(at(30)) {
		t.Errorf("Since = %s, want %s", got, at(30))
	}
}

func TestWaitSecondsRoundsUpAndIsNeverZeroForARefusal(t *testing.T) {
	cases := []struct {
		name     string
		decision Decision
		want     int
	}{
		{"admitted", Decision{Admitted: true}, 0},
		{"a nanosecond", Decision{RetryAfter: time.Nanosecond}, 1},
		{"exactly one second", Decision{RetryAfter: time.Second}, 1},
		{"just over one second", Decision{RetryAfter: time.Second + time.Millisecond}, 2},
		{"twelve and a half", Decision{RetryAfter: 12500 * time.Millisecond}, 13},
		{"zero on a refusal", Decision{}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.decision.WaitSeconds(); got != c.want {
				t.Errorf("WaitSeconds = %d, want %d", got, c.want)
			}
		})
	}
}

func TestExplainNamesTheBudgetAndTheWait(t *testing.T) {
	d := Decision{RetryAfter: 11500 * time.Millisecond, Budget: budget(t, 30, time.Minute)}
	want := "This account has submitted 30 statements in the last 60 seconds, which is its budget. " +
		"Wait 12 seconds and try again."
	if got := d.Explain(); got != want {
		t.Errorf("Explain =\n  %q\nwant\n  %q", got, want)
	}
}

func TestExplainRoundsASubSecondWindowUp(t *testing.T) {
	d := Decision{RetryAfter: time.Millisecond, Budget: budget(t, 1, 500*time.Millisecond)}
	if got := d.Explain(); !strings.Contains(got, "in the last 1 seconds") {
		t.Errorf("Explain = %q; a window under a second reads as one", got)
	}
}

type memoryLedger struct {
	admitted map[uuid.UUID][]time.Time
	err      error
	sawNow   time.Time
}

func (m *memoryLedger) Admit(_ context.Context, account uuid.UUID, now time.Time, b Budget) (Decision, error) {
	m.sawNow = now
	if m.err != nil {
		return Decision{}, m.err
	}
	d := b.Decide(m.admitted[account], now)
	if d.Admitted {
		m.admitted[account] = append(m.admitted[account], now)
	}
	return d, nil
}

func TestNewLimiterRefusesAMissingPart(t *testing.T) {
	ledger := &memoryLedger{admitted: map[uuid.UUID][]time.Time{}}
	clock := func() time.Time { return epoch }
	b := budget(t, 1, time.Minute)
	cases := []struct {
		name   string
		ledger Ledger
		budget Budget
		clock  func() time.Time
	}{
		{"no ledger", nil, b, clock},
		{"no clock", ledger, b, nil},
		{"a zero budget", ledger, Budget{}, clock},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewLimiter(c.ledger, c.budget, c.clock); err == nil {
				t.Error("wired; a limiter missing a part must not start")
			}
		})
	}
}

func TestLimiterSpendsEachAccountsOwnBudgetAtTheClocksTime(t *testing.T) {
	ledger := &memoryLedger{admitted: map[uuid.UUID][]time.Time{}}
	now := at(0)
	limiter, err := NewLimiter(ledger, budget(t, 2, time.Minute), func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	alice, bob := uuid.New(), uuid.New()
	ctx := context.Background()

	for i, want := range []bool{true, true, false} {
		d, err := limiter.Admit(ctx, alice)
		if err != nil || d.Admitted != want {
			t.Fatalf("alice submission %d: Admitted = %v, err %v; want %v", i+1, d.Admitted, err, want)
		}
	}
	if d, err := limiter.Admit(ctx, bob); err != nil || !d.Admitted {
		t.Errorf("bob was refused for alice's submissions: admitted %v, err %v", d.Admitted, err)
	}
	now = at(60)
	if d, err := limiter.Admit(ctx, alice); err != nil || !d.Admitted {
		t.Errorf("alice was refused after her window rolled: admitted %v, err %v", d.Admitted, err)
	}
	if !ledger.sawNow.Equal(at(60)) {
		t.Errorf("the ledger saw %s, want the clock's %s", ledger.sawNow, at(60))
	}
}

func TestLimiterNeverAdmitsOnAnError(t *testing.T) {
	broken := errors.New("store unreachable")
	ledger := &memoryLedger{err: broken}
	limiter, err := NewLimiter(ledger, budget(t, 1, time.Minute), func() time.Time { return epoch })
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	d, err := limiter.Admit(context.Background(), uuid.New())
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want it to carry %v", err, broken)
	}
	if d.Admitted {
		t.Error("an error was an admission")
	}
}
