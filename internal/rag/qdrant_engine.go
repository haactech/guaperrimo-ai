package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// QdrantEngine implements Engine using Qdrant vector search + PostgreSQL hydration.
type QdrantEngine struct {
	baseURL    string
	collection string
	embedder   Embedder
	hydrator   ProductHydrator
	client     *http.Client
}

func NewQdrantEngine(baseURL, collection string, embedder Embedder, hydrator ProductHydrator) *QdrantEngine {
	return &QdrantEngine{
		baseURL:    strings.TrimRight(baseURL, "/"),
		collection: collection,
		embedder:   embedder,
		hydrator:   hydrator,
		client:     &http.Client{},
	}
}

// Search finds products matching the query (without scores).
func (e *QdrantEngine) Search(ctx context.Context, query SearchQuery) ([]Product, error) {
	results, err := e.SearchWithScores(ctx, query)
	if err != nil {
		return nil, err
	}
	products := make([]Product, len(results))
	for i, r := range results {
		products[i] = r.Product
	}
	return products, nil
}

// SearchWithScores returns products with relevance scores.
func (e *QdrantEngine) SearchWithScores(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	// 1. Embed the query text
	vector, err := e.embedder.EmbedText(ctx, query.Text)
	if err != nil {
		return nil, fmt.Errorf("qdrant: embed query: %w", err)
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}

	// 2. Build Qdrant search request
	searchReq := qdrantSearchRequest{
		Vector:      vector,
		Limit:       limit,
		WithPayload: false, // we hydrate from PostgreSQL
	}
	if filter := buildQdrantFilter(query); filter != nil {
		searchReq.Filter = filter
	}

	body, err := json.Marshal(searchReq)
	if err != nil {
		return nil, fmt.Errorf("qdrant: marshal search: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points/search", e.baseURL, e.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("qdrant: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("qdrant: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant: search status %d: %s", resp.StatusCode, string(respBody))
	}

	var searchResp qdrantSearchResponse
	if err := json.Unmarshal(respBody, &searchResp); err != nil {
		return nil, fmt.Errorf("qdrant: unmarshal response: %w", err)
	}

	if len(searchResp.Result) == 0 {
		return nil, nil
	}

	// 3. Extract IDs and scores
	ids := make([]string, len(searchResp.Result))
	scoreMap := make(map[string]float64, len(searchResp.Result))
	for i, pt := range searchResp.Result {
		ids[i] = pt.ID
		scoreMap[pt.ID] = pt.Score
	}

	// 4. Hydrate from PostgreSQL
	products, err := e.hydrator.GetProducts(ctx, ids)
	if err != nil {
		slog.Warn("qdrant: catalog hydration failed, returning IDs only", "error", err)
		// Return minimal results with just IDs and scores
		results := make([]SearchResult, len(searchResp.Result))
		for i, pt := range searchResp.Result {
			results[i] = SearchResult{
				Product: Product{ID: pt.ID},
				Score:   pt.Score,
			}
		}
		return results, nil
	}

	// 5. Re-order by Qdrant score (GetProducts returns arbitrary order)
	productMap := make(map[string]Product, len(products))
	for _, p := range products {
		productMap[p.ID] = p
	}

	results := make([]SearchResult, 0, len(ids))
	for _, id := range ids {
		if p, ok := productMap[id]; ok {
			results = append(results, SearchResult{
				Product: p,
				Score:   scoreMap[id],
			})
		}
	}

	return results, nil
}

// Ingest adds products to the vector store (for future use; seed pipeline is the primary path).
func (e *QdrantEngine) Ingest(ctx context.Context, products []Product) error {
	if len(products) == 0 {
		return nil
	}

	texts := make([]string, len(products))
	for i, p := range products {
		texts[i] = fmt.Sprintf("%s %s %s %s %s %s",
			p.Name, p.Description, p.Category, p.Subcategory,
			strings.Join(p.Colors, " "), strings.Join(p.StyleTags, " "))
	}

	vectors, err := e.embedder.(*OpenAIEmbedder).EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("qdrant: embed batch: %w", err)
	}

	points := make([]qdrantPoint, len(products))
	for i, p := range products {
		points[i] = qdrantPoint{
			ID:     p.ID,
			Vector: vectors[i],
			Payload: map[string]any{
				"name":        p.Name,
				"category":    p.Category,
				"subcategory": p.Subcategory,
				"price":       p.Price,
			},
		}
	}

	body, err := json.Marshal(map[string]any{"points": points})
	if err != nil {
		return fmt.Errorf("qdrant: marshal upsert: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points?wait=true", e.baseURL, e.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build upsert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: upsert failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant: upsert status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

// Delete removes products from the vector store.
func (e *QdrantEngine) Delete(ctx context.Context, productIDs []string) error {
	if len(productIDs) == 0 {
		return nil
	}

	reqBody := map[string]any{
		"points": productIDs,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("qdrant: marshal delete: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points/delete?wait=true", e.baseURL, e.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: delete failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant: delete status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

// --- Qdrant filter construction ---

func buildQdrantFilter(q SearchQuery) *qdrantFilter {
	var must []qdrantCondition

	for _, cat := range q.Categories {
		must = append(must, qdrantCondition{
			Key:   "category",
			Match: &qdrantMatch{Value: cat},
		})
	}

	if q.MaxPrice > 0 {
		must = append(must, qdrantCondition{
			Key:   "price",
			Range: &qdrantRange{Lte: &q.MaxPrice},
		})
	}

	if q.MinPrice > 0 {
		must = append(must, qdrantCondition{
			Key:   "price",
			Range: &qdrantRange{Gte: &q.MinPrice},
		})
	}

	if len(must) == 0 {
		return nil
	}
	return &qdrantFilter{Must: must}
}

// --- Qdrant types ---

type qdrantSearchRequest struct {
	Vector      []float32    `json:"vector"`
	Limit       int          `json:"limit"`
	Filter      *qdrantFilter `json:"filter,omitempty"`
	WithPayload bool         `json:"with_payload"`
}

type qdrantSearchResponse struct {
	Result []qdrantScoredPoint `json:"result"`
}

type qdrantScoredPoint struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

type qdrantFilter struct {
	Must []qdrantCondition `json:"must"`
}

type qdrantCondition struct {
	Key   string        `json:"key"`
	Match *qdrantMatch  `json:"match,omitempty"`
	Range *qdrantRange  `json:"range,omitempty"`
}

type qdrantMatch struct {
	Value string `json:"value"`
}

type qdrantRange struct {
	Gte *float64 `json:"gte,omitempty"`
	Lte *float64 `json:"lte,omitempty"`
}

type qdrantPoint struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}
