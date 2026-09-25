package search

import (
	"context"

	"stylerag/internal/rag"
)

// RAGAdapter wraps a rag.Engine to implement ProductSearcher.
type RAGAdapter struct {
	engine rag.Engine
}

func NewRAGAdapter(engine rag.Engine) *RAGAdapter {
	return &RAGAdapter{engine: engine}
}

func (a *RAGAdapter) SearchProducts(ctx context.Context, query string, opts SearchOptions) ([]rag.Product, error) {
	q := rag.SearchQuery{
		Text:     query,
		MaxPrice: opts.MaxPrice,
		Limit:    opts.Limit,
	}
	if q.Limit <= 0 {
		q.Limit = 3
	}
	return a.engine.Search(ctx, q)
}
