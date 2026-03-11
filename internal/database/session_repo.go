package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRecord represents a session row in PostgreSQL (for analytics).
type SessionRecord struct {
	ID                string     `json:"id"`
	RetailerID        string     `json:"retailer_id"`
	ExternalSessionID *string    `json:"external_session_id,omitempty"`
	UserAgent         *string    `json:"user_agent,omitempty"`
	IPHash            *string    `json:"ip_hash,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	TurnCount         int        `json:"turn_count"`
	HasImageUpload    bool       `json:"has_image_upload"`
	HasPurchase       bool       `json:"has_purchase"`
	TotalTokensUsed   int        `json:"total_tokens_used"`
}

// SessionEvent represents a single event in a session.
type SessionEvent struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// Recommendation tracks a product recommendation and its outcome.
type Recommendation struct {
	ID           string         `json:"id"`
	SessionID    string         `json:"session_id"`
	ProductID    string         `json:"product_id"`
	Rank         int            `json:"rank"`
	Score        *float64       `json:"score,omitempty"`
	Context      map[string]any `json:"context"`
	WasClicked   bool           `json:"was_clicked"`
	WasPurchased bool           `json:"was_purchased"`
	CreatedAt    time.Time      `json:"created_at"`
}

// SessionRepo handles session, event, and recommendation persistence.
type SessionRepo struct {
	pool *pgxpool.Pool
}

// NewSessionRepo creates a new session repository.
func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// --- Sessions ---

func (r *SessionRepo) CreateSession(ctx context.Context, s *SessionRecord) error {
	query := `
		INSERT INTO sessions (retailer_id, external_session_id, user_agent, ip_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id, started_at`

	return r.pool.QueryRow(ctx, query,
		s.RetailerID, s.ExternalSessionID, s.UserAgent, s.IPHash,
	).Scan(&s.ID, &s.StartedAt)
}

func (r *SessionRepo) GetSession(ctx context.Context, id string) (*SessionRecord, error) {
	query := `
		SELECT id, retailer_id, external_session_id, user_agent, ip_hash,
		       started_at, ended_at, turn_count, has_image_upload, has_purchase, total_tokens_used
		FROM sessions WHERE id = $1`

	s := &SessionRecord{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&s.ID, &s.RetailerID, &s.ExternalSessionID, &s.UserAgent, &s.IPHash,
		&s.StartedAt, &s.EndedAt, &s.TurnCount, &s.HasImageUpload,
		&s.HasPurchase, &s.TotalTokensUsed,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("session %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("querying session: %w", err)
	}
	return s, nil
}

func (r *SessionRepo) EndSession(ctx context.Context, id string) error {
	query := `UPDATE sessions SET ended_at = NOW() WHERE id = $1 AND ended_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("ending session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session %s not found or already ended", id)
	}
	return nil
}

func (r *SessionRepo) IncrementTurnCount(ctx context.Context, id string, tokensUsed int) error {
	query := `
		UPDATE sessions
		SET turn_count = turn_count + 1, total_tokens_used = total_tokens_used + $2
		WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id, tokensUsed)
	return err
}

func (r *SessionRepo) SetImageUploaded(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions SET has_image_upload = true WHERE id = $1`, id)
	return err
}

func (r *SessionRepo) SetPurchased(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions SET has_purchase = true WHERE id = $1`, id)
	return err
}

// --- Session Events ---

func (r *SessionRepo) RecordEvent(ctx context.Context, e *SessionEvent) error {
	query := `
		INSERT INTO session_events (session_id, event_type, payload)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`

	return r.pool.QueryRow(ctx, query,
		e.SessionID, e.EventType, e.Payload,
	).Scan(&e.ID, &e.CreatedAt)
}

func (r *SessionRepo) GetSessionEvents(ctx context.Context, sessionID string) ([]SessionEvent, error) {
	query := `
		SELECT id, session_id, event_type, payload, created_at
		FROM session_events
		WHERE session_id = $1
		ORDER BY created_at`

	rows, err := r.pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying session events: %w", err)
	}
	defer rows.Close()

	var events []SessionEvent
	for rows.Next() {
		var e SessionEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Recommendations ---

func (r *SessionRepo) RecordRecommendation(ctx context.Context, rec *Recommendation) error {
	query := `
		INSERT INTO recommendations (session_id, product_id, rank, score, context)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`

	return r.pool.QueryRow(ctx, query,
		rec.SessionID, rec.ProductID, rec.Rank, rec.Score, rec.Context,
	).Scan(&rec.ID, &rec.CreatedAt)
}

func (r *SessionRepo) RecordRecommendationsBatch(ctx context.Context, recs []Recommendation) error {
	batch := &pgx.Batch{}
	query := `
		INSERT INTO recommendations (session_id, product_id, rank, score, context)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`

	for i := range recs {
		batch.Queue(query,
			recs[i].SessionID, recs[i].ProductID, recs[i].Rank,
			recs[i].Score, recs[i].Context,
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := range recs {
		if err := br.QueryRow().Scan(&recs[i].ID, &recs[i].CreatedAt); err != nil {
			return fmt.Errorf("recording recommendation %d: %w", i, err)
		}
	}
	return nil
}

func (r *SessionRepo) MarkRecommendationClicked(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE recommendations SET was_clicked = true WHERE id = $1`, id)
	return err
}

func (r *SessionRepo) MarkRecommendationPurchased(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE recommendations SET was_purchased = true WHERE id = $1`, id)
	return err
}

func (r *SessionRepo) GetSessionRecommendations(ctx context.Context, sessionID string) ([]Recommendation, error) {
	query := `
		SELECT id, session_id, product_id, rank, score, context,
		       was_clicked, was_purchased, created_at
		FROM recommendations
		WHERE session_id = $1
		ORDER BY rank`

	rows, err := r.pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying recommendations: %w", err)
	}
	defer rows.Close()

	var recs []Recommendation
	for rows.Next() {
		var rec Recommendation
		if err := rows.Scan(
			&rec.ID, &rec.SessionID, &rec.ProductID, &rec.Rank,
			&rec.Score, &rec.Context, &rec.WasClicked, &rec.WasPurchased,
			&rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning recommendation: %w", err)
		}
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}
