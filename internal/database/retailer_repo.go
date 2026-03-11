package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Retailer represents a B2B customer.
type Retailer struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Slug       string         `json:"slug"`
	APIKeyHash string         `json:"-"`
	Config     map[string]any `json:"config"`
	IsActive   bool           `json:"is_active"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// RetailerRepo handles retailer CRUD operations.
type RetailerRepo struct {
	pool *pgxpool.Pool
}

// NewRetailerRepo creates a new retailer repository.
func NewRetailerRepo(pool *pgxpool.Pool) *RetailerRepo {
	return &RetailerRepo{pool: pool}
}

func (r *RetailerRepo) GetByID(ctx context.Context, id string) (*Retailer, error) {
	query := `
		SELECT id, name, slug, api_key_hash, config, is_active, created_at, updated_at
		FROM retailers WHERE id = $1`

	ret := &Retailer{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&ret.ID, &ret.Name, &ret.Slug, &ret.APIKeyHash,
		&ret.Config, &ret.IsActive, &ret.CreatedAt, &ret.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("retailer %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("querying retailer: %w", err)
	}
	return ret, nil
}

func (r *RetailerRepo) GetBySlug(ctx context.Context, slug string) (*Retailer, error) {
	query := `
		SELECT id, name, slug, api_key_hash, config, is_active, created_at, updated_at
		FROM retailers WHERE slug = $1 AND is_active = true`

	ret := &Retailer{}
	err := r.pool.QueryRow(ctx, query, slug).Scan(
		&ret.ID, &ret.Name, &ret.Slug, &ret.APIKeyHash,
		&ret.Config, &ret.IsActive, &ret.CreatedAt, &ret.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("retailer with slug %q not found", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("querying retailer by slug: %w", err)
	}
	return ret, nil
}

func (r *RetailerRepo) Create(ctx context.Context, ret *Retailer) error {
	query := `
		INSERT INTO retailers (name, slug, api_key_hash, config)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`

	return r.pool.QueryRow(ctx, query,
		ret.Name, ret.Slug, ret.APIKeyHash, ret.Config,
	).Scan(&ret.ID, &ret.CreatedAt, &ret.UpdatedAt)
}

func (r *RetailerRepo) Update(ctx context.Context, ret *Retailer) error {
	query := `
		UPDATE retailers
		SET name = $2, config = $3, is_active = $4
		WHERE id = $1
		RETURNING updated_at`

	err := r.pool.QueryRow(ctx, query,
		ret.ID, ret.Name, ret.Config, ret.IsActive,
	).Scan(&ret.UpdatedAt)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("retailer %s not found", ret.ID)
	}
	return err
}

func (r *RetailerRepo) List(ctx context.Context) ([]Retailer, error) {
	query := `
		SELECT id, name, slug, api_key_hash, config, is_active, created_at, updated_at
		FROM retailers WHERE is_active = true ORDER BY name`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing retailers: %w", err)
	}
	defer rows.Close()

	var retailers []Retailer
	for rows.Next() {
		var ret Retailer
		if err := rows.Scan(
			&ret.ID, &ret.Name, &ret.Slug, &ret.APIKeyHash,
			&ret.Config, &ret.IsActive, &ret.CreatedAt, &ret.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning retailer: %w", err)
		}
		retailers = append(retailers, ret)
	}
	return retailers, rows.Err()
}
