package rag

import (
	"context"
)

// Product represents a catalog item
type Product struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Subcategory string   `json:"subcategory"`
	Colors      []string `json:"colors"`
	Fit         string   `json:"fit"`
	StyleTags   []string `json:"style_tags"`
	Price       float64  `json:"price"`
	Sizes       []string `json:"sizes"`
	ImageURL    string   `json:"image_url"`
}

// SearchQuery defines the search criteria
type SearchQuery struct {
	Text        string   // Natural language query
	StyleTags   []string // Style tags to match
	Categories  []string // Product categories to include
	ExcludeTags []string // Tags to exclude
	MinPrice    float64
	MaxPrice    float64
	Limit       int
}

// SearchResult contains a product with its relevance score
type SearchResult struct {
	Product Product
	Score   float64
}

// Engine defines the interface for RAG operations
type Engine interface {
	// Search finds products matching the query using vector similarity + filters
	Search(ctx context.Context, query SearchQuery) ([]Product, error)

	// SearchWithScores returns products with relevance scores
	SearchWithScores(ctx context.Context, query SearchQuery) ([]SearchResult, error)

	// Ingest adds or updates products in the vector store
	Ingest(ctx context.Context, products []Product) error

	// Delete removes products from the vector store
	Delete(ctx context.Context, productIDs []string) error
}

// Embedder generates embeddings for text and images
type Embedder interface {
	EmbedText(ctx context.Context, text string) ([]float32, error)
	EmbedImage(ctx context.Context, imageData []byte) ([]float32, error)
}

// ProductHydrator fetches full product data from the catalog store.
// Implemented by catalog.PostgresRepository to break the import cycle.
type ProductHydrator interface {
	GetProducts(ctx context.Context, ids []string) ([]Product, error)
}
