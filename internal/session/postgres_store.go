package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore persists sessions as JSONB rows. The table is created on
// startup so no migration tooling is needed.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects and ensures the sessions table exists.
func NewPostgresStore(ctx context.Context, url string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		state JSONB NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ensure schema: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) Get(ctx context.Context, id string) (*State, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT state FROM sessions WHERE id = $1`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("postgres: decode session: %w", err)
	}
	return &st, nil
}

func (s *PostgresStore) Save(ctx context.Context, st *State) error {
	st.UpdatedAt = time.Now()
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO sessions (id, state, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, updated_at = now()`, st.ID, data)
	if err != nil {
		return fmt.Errorf("postgres: save session: %w", err)
	}
	return nil
}

func (s *PostgresStore) Update(ctx context.Context, id string, fn func(*State) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var data []byte
	err = tx.QueryRow(ctx, `SELECT state FROM sessions WHERE id = $1 FOR UPDATE`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("postgres: lock session: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("postgres: decode session: %w", err)
	}
	if err := fn(&st); err != nil {
		return err
	}
	st.UpdatedAt = time.Now()
	out, err := json.Marshal(&st)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET state = $2, updated_at = now() WHERE id = $1`, id, out); err != nil {
		return fmt.Errorf("postgres: update session: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}
