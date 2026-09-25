package search

import (
	"context"

	"stylerag/internal/rag"
)

// ProductSearcher abstracts product search across different backends (RAG, web).
type ProductSearcher interface {
	SearchProducts(ctx context.Context, query string, opts SearchOptions) ([]rag.Product, error)
}

// SearchOptions controls search behavior.
type SearchOptions struct {
	MaxPrice float64
	Location string
	Limit    int
}
