package search

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"stylerag/internal/rag"
)

const serpAPIBaseURL = "https://serpapi.com/search.json"

// SerpSearcher searches Google Shopping via SerpAPI.
type SerpSearcher struct {
	apiKey   string
	location string // default location, e.g. "Mexico City, Mexico"
	client   *http.Client
}

func NewSerpSearcher(apiKey, defaultLocation string) *SerpSearcher {
	return &SerpSearcher{
		apiKey:   apiKey,
		location: defaultLocation,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *SerpSearcher) SearchProducts(ctx context.Context, query string, opts SearchOptions) ([]rag.Product, error) {
	params := url.Values{
		"engine":  {"google_shopping"},
		"q":       {query},
		"api_key": {s.apiKey},
		"hl":      {"es"},
		"gl":      {"mx"},
	}

	loc := opts.Location
	if loc == "" {
		loc = s.location
	}
	if loc != "" {
		params.Set("location", loc)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 3
	}
	// Request more than needed to allow filtering
	params.Set("num", strconv.Itoa(limit+5))

	reqURL := serpAPIBaseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("serp: build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serp: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("serp: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.WarnContext(ctx, "serp: non-200 response", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("serp: status %d", resp.StatusCode)
	}

	var result serpResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("serp: parse response: %w", err)
	}

	products := make([]rag.Product, 0, limit)
	for _, item := range result.ShoppingResults {
		if opts.MaxPrice > 0 && item.ExtractedPrice > opts.MaxPrice {
			continue
		}
		products = append(products, mapToProduct(item))
		if len(products) >= limit {
			break
		}
	}

	slog.InfoContext(ctx, "serp: search done",
		"query", query,
		"results_raw", len(result.ShoppingResults),
		"results_filtered", len(products),
	)

	return products, nil
}

func mapToProduct(item serpShoppingResult) rag.Product {
	id := shortHash(item.Link)
	return rag.Product{
		ID:          id,
		Name:        item.Title,
		Description: item.Title,
		Price:       item.ExtractedPrice,
		ImageURL:    item.Thumbnail,
	}
}

// shortHash returns a deterministic 12-char hex ID from a URL.
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:6])
}

// SerpAPI response types

type serpResponse struct {
	ShoppingResults []serpShoppingResult `json:"shopping_results"`
}

type serpShoppingResult struct {
	Title          string  `json:"title"`
	Link           string  `json:"link"`
	Source         string  `json:"source"`
	Price          string  `json:"price"`
	ExtractedPrice float64 `json:"extracted_price"`
	Thumbnail      string  `json:"thumbnail"`
	Rating         float64 `json:"rating,omitempty"`
	Extensions     []string `json:"extensions,omitempty"`
}
