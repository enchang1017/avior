// Package ratelimit is FEAT-006: how many statements one account may submit in
// a rolling window.
//
// The arithmetic is here and holds no I/O. Where the admitted submissions are
// kept is the Ledger's business, because production runs several replicas and a
// budget counted in one process's memory would be one budget per pod.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Budget is how many submissions an account may make in any window of the given
// length.
type Budget struct {
	statements int
	window     time.Duration
}

// NewBudget refuses a budget that admits nothing or has no window. A limiter
// that refuses everybody is an outage, and one with no window admits everybody.
func NewBudget(statements int, window time.Duration) (Budget, error) {
	if statements <= 0 {
		return Budget{}, fmt.Errorf("ratelimit: statements must be positive, got %d", statements)
	}
	if window <= 0 {
		return Budget{}, fmt.Errorf("ratelimit: window must be positive, got %s", window)
	}
	return Budget{statements: statements, window: window}, nil
}

// Statements is how many submissions the budget admits per window.
func (b Budget) Statements() int { return b.statements }

// Window is the length of the rolling window.
func (b Budget) Window() time.Duration { return b.window }

// Since is the earliest instant still inside the window that ends at now.
// Anything admitted at or before it has aged out.
func (b Budget) Since(now time.Time) time.Time { return now.Add(-b.window) }

// Decision is the answer to one submission. RetryAfter is zero when admitted
// and positive when refused.
type Decision struct {
	Admitted   bool
	RetryAfter time.Duration
	Budget     Budget
}

// WaitSeconds is RetryAfter in whole seconds, rounded up, and never zero for a
// refusal: a caller told to wait zero seconds retries at once and is refused
// again.
func (d Decision) WaitSeconds() int {
	if d.Admitted {
		return 0
	}
	return max(1, int(math.Ceil(d.RetryAfter.Seconds())))
}

// Explain is the sentence a refused caller reads, in the HTTP answer, the MCP
// tool error and the audit row alike.
func (d Decision) Explain() string {
	return fmt.Sprintf(
		"This account has submitted %d statements in the last %d seconds, which is its budget. "+
			"Wait %d seconds and try again.",
		d.Budget.statements, int(math.Ceil(d.Budget.window.Seconds())), d.WaitSeconds())
}

// Decide admits a submission at now when fewer than the budget's statements were
// admitted inside the window ending at now.
//
// Only admitted submissions are passed in and only an admitted one is counted,
// so a refusal spends nothing and the wait is exact: it is how long until enough
// of the admitted ones age out to leave one slot.
func (b Budget) Decide(admitted []time.Time, now time.Time) Decision {
	since := b.Since(now)
	inside := make([]time.Time, 0, len(admitted))
	for _, at := range admitted {
		if at.After(since) {
			inside = append(inside, at)
		}
	}
	if len(inside) < b.statements {
		return Decision{Admitted: true, Budget: b}
	}
	slices.SortFunc(inside, func(a, c time.Time) int { return a.Compare(c) })
	freeing := inside[len(inside)-b.statements]
	wait := min(freeing.Add(b.window).Sub(now), b.window)
	return Decision{RetryAfter: max(wait, time.Nanosecond), Budget: b}
}

// Ledger keeps the admitted submissions and decides one atomically: the read,
// the decision and the record of an admission happen as one step, so two
// submissions racing for the last slot cannot both be admitted.
type Ledger interface {
	Admit(ctx context.Context, account uuid.UUID, now time.Time, budget Budget) (Decision, error)
}

// Limiter spends one account's budget per submission.
type Limiter struct {
	ledger Ledger
	budget Budget
	clock  func() time.Time
}

// NewLimiter wires a limiter. The clock is injected so the acceptance suite can
// move time rather than sleep through a window.
func NewLimiter(ledger Ledger, budget Budget, clock func() time.Time) (*Limiter, error) {
	switch {
	case ledger == nil:
		return nil, errors.New("ratelimit: ledger is required")
	case clock == nil:
		return nil, errors.New("ratelimit: clock is required")
	case budget.statements <= 0 || budget.window <= 0:
		return nil, errors.New("ratelimit: budget is required; build one with NewBudget")
	}
	return &Limiter{ledger: ledger, budget: budget, clock: clock}, nil
}

// Admit spends one submission of account's budget, or refuses it. An error is
// never an admission: the caller must refuse the submission.
func (l *Limiter) Admit(ctx context.Context, account uuid.UUID) (Decision, error) {
	decision, err := l.ledger.Admit(ctx, account, l.clock(), l.budget)
	if err != nil {
		return Decision{}, fmt.Errorf("spend the statement budget: %w", err)
	}
	return decision, nil
}
