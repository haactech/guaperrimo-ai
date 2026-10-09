package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreReturnsCopies(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	defer s.Stop()
	ctx := context.Background()

	st := New("abc")
	st.Profile.Occasion = "boda"
	if err := s.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	got.Profile.Occasion = "cita"
	again, _ := s.Get(ctx, "abc")
	if again.Profile.Occasion != "boda" {
		t.Fatalf("Get must return a private copy, got %q", again.Profile.Occasion)
	}
}

func TestMemoryStoreUpdateAndTTL(t *testing.T) {
	s := NewMemoryStore(20 * time.Millisecond)
	defer s.Stop()
	ctx := context.Background()

	if err := s.Save(ctx, New("abc")); err != nil {
		t.Fatal(err)
	}
	err := s.Update(ctx, "abc", func(st *State) error {
		st.Turn = 7
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, "abc")
	if got.Turn != 7 {
		t.Fatalf("update not applied: %d", got.Turn)
	}
	if err := s.Update(ctx, "missing", func(*State) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	time.Sleep(40 * time.Millisecond)
	if _, err := s.Get(ctx, "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expiry, got %v", err)
	}
}
