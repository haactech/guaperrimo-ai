package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stylerag/internal/rag"
)

// PostgresRepository implements Repository using PostgreSQL via pgx.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a new catalog repository backed by PostgreSQL.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) GetProduct(ctx context.Context, id string) (*rag.Product, error) {
	query := `
		SELECT id, name, description, category, subcategory,
		       colors, fit, style_tags, price, sizes, image_url
		FROM products
		WHERE id = $1 AND is_active = true`

	var p rag.Product
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.Category, &p.Subcategory,
		&p.Colors, &p.Fit, &p.StyleTags, &p.Price, &p.Sizes, &p.ImageURL,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("product %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("querying product: %w", err)
	}
	return &p, nil
}

func (r *PostgresRepository) GetProducts(ctx context.Context, ids []string) ([]rag.Product, error) {
	query := `
		SELECT id, name, description, category, subcategory,
		       colors, fit, style_tags, price, sizes, image_url
		FROM products
		WHERE id = ANY($1) AND is_active = true`

	rows, err := r.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("querying products: %w", err)
	}
	defer rows.Close()

	return scanProducts(rows)
}

func (r *PostgresRepository) ListProducts(ctx context.Context, offset, limit int) ([]rag.Product, error) {
	query := `
		SELECT id, name, description, category, subcategory,
		       colors, fit, style_tags, price, sizes, image_url
		FROM products
		WHERE is_active = true
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing products: %w", err)
	}
	defer rows.Close()

	return scanProducts(rows)
}

func (r *PostgresRepository) CreateProduct(ctx context.Context, product *rag.Product) error {
	query := `
		INSERT INTO products (name, description, category, subcategory,
		                      colors, fit, style_tags, price, sizes, image_url,
		                      retailer_id, sku)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        (SELECT id FROM retailers WHERE slug = 'poc-store'), $1)
		RETURNING id`

	return r.pool.QueryRow(ctx, query,
		product.Name, product.Description, product.Category, product.Subcategory,
		product.Colors, product.Fit, product.StyleTags, product.Price,
		product.Sizes, product.ImageURL,
	).Scan(&product.ID)
}

func (r *PostgresRepository) UpdateProduct(ctx context.Context, product *rag.Product) error {
	query := `
		UPDATE products
		SET name = $2, description = $3, category = $4, subcategory = $5,
		    colors = $6, fit = $7, style_tags = $8, price = $9,
		    sizes = $10, image_url = $11
		WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query,
		product.ID, product.Name, product.Description, product.Category,
		product.Subcategory, product.Colors, product.Fit, product.StyleTags,
		product.Price, product.Sizes, product.ImageURL,
	)
	if err != nil {
		return fmt.Errorf("updating product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("product %s not found", product.ID)
	}
	return nil
}

func (r *PostgresRepository) DeleteProduct(ctx context.Context, id string) error {
	query := `UPDATE products SET is_active = false WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("deleting product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("product %s not found", id)
	}
	return nil
}

func (r *PostgresRepository) FilterProducts(ctx context.Context, filter ProductFilter, offset, limit int) (*ProductListResult, error) {
	where := "is_active = true"
	args := []any{}
	idx := 1

	if filter.Category != "" {
		where += fmt.Sprintf(" AND category = $%d", idx)
		args = append(args, filter.Category)
		idx++
	}
	if filter.Subcategory != "" {
		where += fmt.Sprintf(" AND subcategory = $%d", idx)
		args = append(args, filter.Subcategory)
		idx++
	}
	if len(filter.Colors) > 0 {
		where += fmt.Sprintf(" AND colors && $%d", idx)
		args = append(args, filter.Colors)
		idx++
	}
	if len(filter.StyleTags) > 0 {
		where += fmt.Sprintf(" AND style_tags && $%d", idx)
		args = append(args, filter.StyleTags)
		idx++
	}
	if filter.Fit != "" {
		where += fmt.Sprintf(" AND fit = $%d", idx)
		args = append(args, filter.Fit)
		idx++
	}
	if filter.MinPrice > 0 {
		where += fmt.Sprintf(" AND price >= $%d", idx)
		args = append(args, filter.MinPrice)
		idx++
	}
	if filter.MaxPrice > 0 {
		where += fmt.Sprintf(" AND price <= $%d", idx)
		args = append(args, filter.MaxPrice)
		idx++
	}

	// Count total matching rows
	var total int
	countQuery := "SELECT COUNT(*) FROM products WHERE " + where
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting products: %w", err)
	}

	// Fetch page
	selectQuery := fmt.Sprintf(`
		SELECT id, name, description, category, subcategory,
		       colors, fit, style_tags, price, sizes, image_url
		FROM products
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("filtering products: %w", err)
	}
	defer rows.Close()

	products, err := scanProducts(rows)
	if err != nil {
		return nil, err
	}

	return &ProductListResult{Products: products, Total: total}, nil
}

func (r *PostgresRepository) ListCategories(ctx context.Context) ([]CategoryCount, error) {
	query := `
		SELECT category, COUNT(*) AS count
		FROM products
		WHERE is_active = true
		GROUP BY category
		ORDER BY count DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing categories: %w", err)
	}
	defer rows.Close()

	var categories []CategoryCount
	for rows.Next() {
		var cc CategoryCount
		if err := rows.Scan(&cc.Category, &cc.Count); err != nil {
			return nil, fmt.Errorf("scanning category row: %w", err)
		}
		categories = append(categories, cc)
	}
	return categories, rows.Err()
}

func scanProducts(rows pgx.Rows) ([]rag.Product, error) {
	var products []rag.Product
	for rows.Next() {
		var p rag.Product
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Category, &p.Subcategory,
			&p.Colors, &p.Fit, &p.StyleTags, &p.Price, &p.Sizes, &p.ImageURL,
		); err != nil {
			return nil, fmt.Errorf("scanning product row: %w", err)
		}
		products = append(products, p)
	}
	return products, rows.Err()
}
