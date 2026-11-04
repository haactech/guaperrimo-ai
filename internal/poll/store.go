package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MemoryStore keeps polls in memory; polls are lost on restart, like
// in-memory sessions.
type MemoryStore struct {
	mu    sync.Mutex
	polls map[string]Poll
	votes map[string][]Vote // poll id -> votes
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{polls: map[string]Poll{}, votes: map[string][]Vote{}}
}

func (s *MemoryStore) Create(_ context.Context, p *Poll) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls[p.ID] = *p
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Poll, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.polls[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &p, nil
}

func (s *MemoryStore) BySession(_ context.Context, sessionID string) ([]Poll, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Poll
	for _, p := range s.polls {
		if p.SessionID == sessionID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) Vote(_ context.Context, v Vote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.polls[v.PollID]; !ok {
		return ErrNotFound
	}
	votes := s.votes[v.PollID]
	for i := range votes {
		if votes[i].VoterID == v.VoterID {
			votes[i] = v
			return nil
		}
	}
	if len(votes) >= MaxVotesPerPoll {
		return ErrTooManyVotes
	}
	s.votes[v.PollID] = append(votes, v)
	return nil
}

func (s *MemoryStore) Votes(_ context.Context, pollID string) ([]Vote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Vote(nil), s.votes[pollID]...), nil
}

// PostgresStore keeps polls next to the sessions table. Tables are created on
// startup, as for sessions.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore uses an existing pool and ensures the tables exist.
func NewPostgresStore(ctx context.Context, pool *pgxpool.Pool) (*PostgresStore, error) {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS look_polls (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			occasion   TEXT NOT NULL DEFAULT '',
			options    JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS look_polls_session ON look_polls (session_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS look_poll_votes (
			poll_id    TEXT NOT NULL REFERENCES look_polls (id) ON DELETE CASCADE,
			voter_id   TEXT NOT NULL,
			choice     SMALLINT NOT NULL CHECK (choice IN (0, 1)),
			reasons    TEXT[] NOT NULL DEFAULT '{}',
			comment    TEXT NOT NULL DEFAULT '',
			voter_name TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (poll_id, voter_id)
		)`)
	if err != nil {
		return nil, fmt.Errorf("polls: ensure schema: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Create(ctx context.Context, p *Poll) error {
	opts, err := json.Marshal(p.Options)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO look_polls (id, session_id, occasion, options, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, p.ID, p.SessionID, p.Occasion, opts, p.CreatedAt, p.ExpiresAt)
	if err != nil {
		return fmt.Errorf("polls: create: %w", err)
	}
	return nil
}

func (s *PostgresStore) Get(ctx context.Context, id string) (*Poll, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, session_id, occasion, options, created_at, expires_at
		FROM look_polls WHERE id = $1`, id)
	p, err := scanPoll(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) BySession(ctx context.Context, sessionID string) ([]Poll, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, session_id, occasion, options, created_at, expires_at
		FROM look_polls WHERE session_id = $1 ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("polls: list: %w", err)
	}
	defer rows.Close()
	var out []Poll
	for rows.Next() {
		p, err := scanPoll(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func scanPoll(row pgx.Row) (*Poll, error) {
	var p Poll
	var opts []byte
	if err := row.Scan(&p.ID, &p.SessionID, &p.Occasion, &opts, &p.CreatedAt, &p.ExpiresAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(opts, &p.Options); err != nil {
		return nil, fmt.Errorf("polls: decode options: %w", err)
	}
	return &p, nil
}

func (s *PostgresStore) Vote(ctx context.Context, v Vote) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("polls: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	// Lock the poll so concurrent new voters cannot overshoot the cap.
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM look_polls WHERE id = $1 FOR UPDATE`, v.PollID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("polls: lock: %w", err)
	}
	var exists bool
	var count int
	if err := tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM look_poll_votes WHERE poll_id = $1 AND voter_id = $2),
			(SELECT count(*) FROM look_poll_votes WHERE poll_id = $1)`, v.PollID, v.VoterID).Scan(&exists, &count); err != nil {
		return fmt.Errorf("polls: count votes: %w", err)
	}
	if !exists && count >= MaxVotesPerPoll {
		return ErrTooManyVotes
	}
	reasons := v.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO look_poll_votes (poll_id, voter_id, choice, reasons, comment, voter_name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (poll_id, voter_id) DO UPDATE SET choice = EXCLUDED.choice, reasons = EXCLUDED.reasons,
			comment = EXCLUDED.comment, voter_name = EXCLUDED.voter_name, created_at = EXCLUDED.created_at`,
		v.PollID, v.VoterID, v.Choice, reasons, v.Comment, v.VoterName, v.CreatedAt); err != nil {
		return fmt.Errorf("polls: save vote: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) Votes(ctx context.Context, pollID string) ([]Vote, error) {
	rows, err := s.pool.Query(ctx, `SELECT poll_id, voter_id, choice, reasons, comment, voter_name, created_at
		FROM look_poll_votes WHERE poll_id = $1 ORDER BY created_at`, pollID)
	if err != nil {
		return nil, fmt.Errorf("polls: votes: %w", err)
	}
	defer rows.Close()
	var out []Vote
	for rows.Next() {
		var v Vote
		if err := rows.Scan(&v.PollID, &v.VoterID, &v.Choice, &v.Reasons, &v.Comment, &v.VoterName, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// compile-time checks
var (
	_ Store = (*MemoryStore)(nil)
	_ Store = (*PostgresStore)(nil)
)
