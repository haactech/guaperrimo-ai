package tryon

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countVTON struct{ n int }

func (c *countVTON) Name() string { return "count" }
func (c *countVTON) Generate(context.Context, VTONRequest) (*VTONResult, error) {
	c.n++
	return &VTONResult{ImageBytes: []byte("x"), MimeType: "image/jpeg"}, nil
}

func TestBudgetLimitsPerSessionAndDay(t *testing.T) {
	inner := &countVTON{}
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.Local)
	b := NewBudgetedVTON(inner, 2, 3)
	b.Now = func() time.Time { return now }
	a, c := WithSession(context.Background(), "a"), WithSession(context.Background(), "c")

	for i := 0; i < 2; i++ {
		if _, err := b.Generate(a, VTONRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Generate(a, VTONRequest{}); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("third call in session a should exceed the session budget, got %v", err)
	}
	if _, err := b.Generate(c, VTONRequest{}); err != nil {
		t.Fatalf("another session still has room: %v", err)
	}
	if _, err := b.Generate(c, VTONRequest{}); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("fourth call of the day should exceed the daily budget, got %v", err)
	}
	if inner.n != 3 {
		t.Fatalf("provider must only be called within budget, got %d calls", inner.n)
	}

	now = now.Add(24 * time.Hour)
	if _, err := b.Generate(a, VTONRequest{}); err != nil {
		t.Fatalf("budget resets the next day: %v", err)
	}
}
