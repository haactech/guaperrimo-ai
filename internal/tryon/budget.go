package tryon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrBudgetExceeded is returned when a try-on call would exceed the configured
// per-session or per-day limit. Every provider call is billed, so the limit is
// enforced before the call is made.
var ErrBudgetExceeded = errors.New("try-on budget exceeded")

type sessionKey struct{}

// WithSession tags ctx with the session the try-on call belongs to.
func WithSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionKey{}, sessionID)
}

func sessionFrom(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey{}).(string)
	return s
}

// BudgetedVTON wraps a provider with hard limits on calls per session and per
// day (server local time). Counters live in memory, so a restart resets them;
// the provider's own billing alert is the backstop.
type BudgetedVTON struct {
	Inner         VTONProvider
	MaxPerSession int // 0 = unlimited
	MaxPerDay     int // 0 = unlimited
	Now           func() time.Time

	mu         sync.Mutex
	day        string
	dayCount   int
	perSession map[string]int
}

// NewBudgetedVTON builds the wrapper.
func NewBudgetedVTON(inner VTONProvider, maxPerSession, maxPerDay int) *BudgetedVTON {
	return &BudgetedVTON{Inner: inner, MaxPerSession: maxPerSession, MaxPerDay: maxPerDay, Now: time.Now, perSession: map[string]int{}}
}

func (b *BudgetedVTON) Name() string { return b.Inner.Name() }

// Generate reserves one call from both budgets before calling the provider.
func (b *BudgetedVTON) Generate(ctx context.Context, req VTONRequest) (*VTONResult, error) {
	sid := sessionFrom(ctx)
	if err := b.reserve(sid); err != nil {
		slog.WarnContext(ctx, "tryon: budget exceeded", "session_id", sid, "error", err)
		return nil, err
	}
	return b.Inner.Generate(ctx, req)
}

func (b *BudgetedVTON) reserve(sid string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	today := b.Now().Format("2006-01-02")
	if today != b.day {
		b.day, b.dayCount, b.perSession = today, 0, map[string]int{}
	}
	if b.MaxPerDay > 0 && b.dayCount >= b.MaxPerDay {
		return fmt.Errorf("%w: daily limit of %d images reached", ErrBudgetExceeded, b.MaxPerDay)
	}
	if b.MaxPerSession > 0 && b.perSession[sid] >= b.MaxPerSession {
		return fmt.Errorf("%w: session limit of %d images reached", ErrBudgetExceeded, b.MaxPerSession)
	}
	b.dayCount++
	b.perSession[sid]++
	return nil
}

// Usage reports today's count, for logs and health checks.
func (b *BudgetedVTON) Usage() (day string, count int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.day, b.dayCount
}
