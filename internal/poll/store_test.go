package poll

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore is the contract every Store must meet.
func testStore(t *testing.T, s Store) {
	ctx := context.Background()
	session := "sess_" + NewToken()[:8]
	now := time.Now().UTC().Truncate(time.Millisecond)
	older := &Poll{ID: NewToken(), SessionID: session, Occasion: "Boda de día", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(TTL),
		Options: [2]Option{{LookID: "l1", Name: "Clásico", ImageKey: "sessions/x/a.jpg"}, {LookID: "l2", Name: "Relajado", ImageKey: "sessions/x/b.jpg"}}}
	newer := &Poll{ID: NewToken(), SessionID: session, CreatedAt: now, ExpiresAt: now.Add(TTL)}
	for _, p := range []*Poll{older, newer} {
		if err := s.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Get(ctx, older.ID)
	if err != nil || got.Options[1].ImageKey != "sessions/x/b.jpg" || got.Occasion != "Boda de día" || !got.ExpiresAt.Equal(older.ExpiresAt) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := s.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing poll: %v", err)
	}
	list, err := s.BySession(ctx, session)
	if err != nil || len(list) != 2 || list[0].ID != newer.ID {
		t.Fatalf("BySession must list newest first: %+v %v", list, err)
	}

	if err := s.Vote(ctx, Vote{PollID: older.ID, VoterID: "v1", Choice: 0, Reasons: []string{"color"}, Comment: "café", VoterName: "Ana", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(ctx, Vote{PollID: older.ID, VoterID: "v1", Choice: 1, CreatedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(ctx, Vote{PollID: "missing", VoterID: "v1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("vote on a missing poll: %v", err)
	}
	votes, err := s.Votes(ctx, older.ID)
	if err != nil || len(votes) != 1 || votes[0].Choice != 1 || votes[0].Comment != "" || len(votes[0].Reasons) != 0 {
		t.Fatalf("a second vote from the same voter replaces the first: %+v %v", votes, err)
	}
	for i := 1; i < MaxVotesPerPoll; i++ {
		if err := s.Vote(ctx, Vote{PollID: older.ID, VoterID: NewToken(), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Vote(ctx, Vote{PollID: older.ID, VoterID: "one-too-many", CreatedAt: now}); !errors.Is(err, ErrTooManyVotes) {
		t.Fatalf("over the cap: %v", err)
	}
	if err := s.Vote(ctx, Vote{PollID: older.ID, VoterID: "v1", Choice: 0, CreatedAt: now}); err != nil {
		t.Fatalf("an existing voter can change his vote at the cap: %v", err)
	}
}

func TestMemoryStore(t *testing.T) { testStore(t, NewMemoryStore()) }

// POLL_TEST_POSTGRES_URL=postgres://guaperrimo:guaperrimo@localhost:5432/guaperrimo?sslmode=disable
func TestPostgresStore(t *testing.T) {
	url := os.Getenv("POLL_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("set POLL_TEST_POSTGRES_URL to run against Postgres")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s, err := NewPostgresStore(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	testStore(t, s)
}

func TestSummarizeIgnoresUnknownReasons(t *testing.T) {
	s := Summarize([]Vote{
		{Choice: 0, Reasons: []string{"color", "nope"}},
		{Choice: 1, Reasons: []string{"color"}},
		{Choice: 1},
		{Choice: 7},
	})
	if s.Total != 3 || s.Votes != [2]int{1, 2} || s.Reasons[0]["color"] != 1 || s.Reasons[0]["nope"] != 0 || s.Reasons[1]["color"] != 1 {
		t.Fatalf("%+v", s)
	}
}
