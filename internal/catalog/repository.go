package catalog

import (
	"context"

	"stylerag/internal/rag"
)

// Repository defines the interface for catalog data access (PostgreSQL)
type Repository interface {
	// GetProduct retrieves a product by ID
	GetProduct(ctx context.Context, id string) (*rag.Product, error)

	// GetProducts retrieves multiple products by IDs
	GetProducts(ctx context.Context, ids []string) ([]rag.Product, error)

	// ListProducts returns paginated products
	ListProducts(ctx context.Context, offset, limit int) ([]rag.Product, error)

	// CreateProduct inserts a new product
	CreateProduct(ctx context.Context, product *rag.Product) error

	// UpdateProduct updates an existing product
	UpdateProduct(ctx context.Context, product *rag.Product) error

	// DeleteProduct removes a product
	DeleteProduct(ctx context.Context, id string) error
}

// Importer handles bulk data import from various formats
type Importer interface {
	// ImportCSV imports products from a CSV file
	ImportCSV(ctx context.Context, filePath string) (int, error)

	// ImportJSON imports products from a JSON file
	ImportJSON(ctx context.Context, filePath string) (int, error)
}
