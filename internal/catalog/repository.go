package catalog

import (
	"context"

	"stylerag/internal/rag"
)

// ProductFilter defines SQL-level filters for catalog browsing.
// Separate from rag.SearchQuery which is for semantic/vector search.
type ProductFilter struct {
	Category    string
	Subcategory string
	Colors      []string
	StyleTags   []string
	Fit         string
	MinPrice    float64
	MaxPrice    float64
}

// ProductListResult wraps a page of products with the total count.
type ProductListResult struct {
	Products []rag.Product
	Total    int
}

// CategoryCount represents a category with its product count.
type CategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// Repository defines the interface for catalog data access (PostgreSQL)
type Repository interface {
	// GetProduct retrieves a product by ID
	GetProduct(ctx context.Context, id string) (*rag.Product, error)

	// GetProducts retrieves multiple products by IDs
	GetProducts(ctx context.Context, ids []string) ([]rag.Product, error)

	// ListProducts returns paginated products
	ListProducts(ctx context.Context, offset, limit int) ([]rag.Product, error)

	// FilterProducts returns products matching the filter with pagination.
	FilterProducts(ctx context.Context, filter ProductFilter, offset, limit int) (*ProductListResult, error)

	// ListCategories returns all categories with their product counts.
	ListCategories(ctx context.Context) ([]CategoryCount, error)

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
