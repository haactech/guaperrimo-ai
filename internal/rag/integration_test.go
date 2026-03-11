package rag

import (
	"context"
	"os"
	"testing"
)

// These tests require a running Qdrant instance with seeded data.
// Run: go test -v -run TestIntegration ./internal/rag/ -tags integration
// Prereqs:
//   docker compose up -d qdrant
//   go run ./cmd/qdrantseed --input data/products_test.csv --fake-vectors --vector-size 64 --recreate

func skipIfNoQdrant(t *testing.T) {
	t.Helper()
	if os.Getenv("RAG_INTEGRATION") == "" {
		t.Skip("skipping integration test (set RAG_INTEGRATION=1 to run)")
	}
}

// mockHydrator returns canned products for testing without PostgreSQL.
type mockHydrator struct{}

func (m *mockHydrator) GetProducts(ctx context.Context, ids []string) ([]Product, error) {
	products := make([]Product, len(ids))
	for i, id := range ids {
		products[i] = Product{
			ID:   id,
			Name: "Product " + id[:8],
		}
	}
	return products, nil
}

// fakeEmbedder returns deterministic vectors matching the seed pipeline.
type fakeEmbedder struct {
	size int
}

func (f *fakeEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	// Match deterministicVector from qdrantseed
	vec64 := deterministicVectorFloat64(text, f.size)
	vec32 := make([]float32, len(vec64))
	for i, v := range vec64 {
		vec32[i] = float32(v)
	}
	return vec32, nil
}

func (f *fakeEmbedder) EmbedImage(ctx context.Context, imageData []byte) ([]float32, error) {
	return nil, nil
}

// deterministicVectorFloat64 mirrors the qdrantseed logic for test compatibility.
func deterministicVectorFloat64(text string, size int) []float64 {
	// Import same logic from qdrantseed (copy since it's in a different main package)
	import_fnv := func(data []byte) uint64 {
		// FNV-1a 64-bit
		var hash uint64 = 14695981039346656037
		for _, b := range data {
			hash ^= uint64(b)
			hash *= 1099511628211
		}
		return hash
	}

	vec := make([]float64, size)
	words := splitWords(text)
	if len(words) == 0 {
		return vec
	}

	for _, token := range words {
		hash := import_fnv([]byte(token))
		idx := int(hash % uint64(size))
		sign := 1.0
		if (hash>>8)&1 == 1 {
			sign = -1.0
		}
		weight := 1.0 + float64((hash>>16)%7)/10.0
		vec[idx] += sign * weight
	}

	// Normalize
	var sum float64
	for _, v := range vec {
		sum += v * v
	}
	if sum > 0 {
		norm := 1.0 / (sum * 0 + 1) // just to avoid import of math
		_ = norm
		// Actually need math.Sqrt, let's just return unnormalized for search test
		// The cosine similarity still works with unnormalized in Qdrant
	}
	return vec
}

func splitWords(s string) []string {
	var words []string
	word := []byte{}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == ' ' || b == '\t' || b == '\n' {
			if len(word) > 0 {
				words = append(words, toLower(string(word)))
				word = word[:0]
			}
		} else {
			word = append(word, b)
		}
	}
	if len(word) > 0 {
		words = append(words, toLower(string(word)))
	}
	return words
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func TestIntegration_QdrantSearch(t *testing.T) {
	skipIfNoQdrant(t)

	engine := NewQdrantEngine(
		"http://localhost:6333",
		"products",
		&fakeEmbedder{size: 64},
		&mockHydrator{},
	)

	ctx := context.Background()
	results, err := engine.SearchWithScores(ctx, SearchQuery{
		Text:  "blue oxford shirt casual",
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	t.Logf("got %d results", len(results))
	for i, r := range results {
		t.Logf("  [%d] id=%s name=%q score=%.4f", i, r.Product.ID, r.Product.Name, r.Score)
	}

	if len(results) == 0 {
		t.Error("expected at least 1 result")
	}
}

func TestIntegration_QdrantSearchWithCategoryFilter(t *testing.T) {
	skipIfNoQdrant(t)

	engine := NewQdrantEngine(
		"http://localhost:6333",
		"products",
		&fakeEmbedder{size: 64},
		&mockHydrator{},
	)

	ctx := context.Background()

	// Search only in Topwear
	results, err := engine.Search(ctx, SearchQuery{
		Text:       "shirt",
		Categories: []string{"Topwear"},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	t.Logf("Topwear results: %d", len(results))
	for _, r := range results {
		t.Logf("  id=%s name=%q", r.ID, r.Name)
	}
}

func TestIntegration_QdrantSearchWithPriceFilter(t *testing.T) {
	skipIfNoQdrant(t)

	engine := NewQdrantEngine(
		"http://localhost:6333",
		"products",
		&fakeEmbedder{size: 64},
		&mockHydrator{},
	)

	ctx := context.Background()

	// Only products under 1000 MXN
	results, err := engine.Search(ctx, SearchQuery{
		Text:     "shirt",
		MaxPrice: 1000,
		Limit:    5,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	t.Logf("results under 1000 MXN: %d", len(results))
	for _, r := range results {
		t.Logf("  id=%s name=%q", r.ID, r.Name)
	}
}
