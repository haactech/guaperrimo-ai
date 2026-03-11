package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"stylerag/internal/catalog"
	"stylerag/internal/rag"
)

// mockCatalogRepo implements catalog.Repository for testing.
type mockCatalogRepo struct {
	products   []rag.Product
	categories []catalog.CategoryCount
}

func (m *mockCatalogRepo) GetProduct(_ context.Context, id string) (*rag.Product, error) {
	for i := range m.products {
		if m.products[i].ID == id {
			return &m.products[i], nil
		}
	}
	return nil, fmt.Errorf("product %s not found", id)
}

func (m *mockCatalogRepo) GetProducts(_ context.Context, ids []string) ([]rag.Product, error) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var result []rag.Product
	for _, p := range m.products {
		if idSet[p.ID] {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockCatalogRepo) ListProducts(_ context.Context, offset, limit int) ([]rag.Product, error) {
	return m.products, nil
}

func (m *mockCatalogRepo) FilterProducts(_ context.Context, _ catalog.ProductFilter, offset, limit int) (*catalog.ProductListResult, error) {
	end := offset + limit
	if end > len(m.products) {
		end = len(m.products)
	}
	start := offset
	if start > len(m.products) {
		start = len(m.products)
	}
	return &catalog.ProductListResult{
		Products: m.products[start:end],
		Total:    len(m.products),
	}, nil
}

func (m *mockCatalogRepo) ListCategories(_ context.Context) ([]catalog.CategoryCount, error) {
	return m.categories, nil
}

func (m *mockCatalogRepo) CreateProduct(_ context.Context, _ *rag.Product) error { return nil }
func (m *mockCatalogRepo) UpdateProduct(_ context.Context, _ *rag.Product) error { return nil }
func (m *mockCatalogRepo) DeleteProduct(_ context.Context, _ string) error       { return nil }

func seedProducts() []rag.Product {
	return []rag.Product{
		{ID: "aaa-111", Name: "Blue Oxford Shirt", Category: "Topwear", Subcategory: "Shirts", Colors: []string{"Blue"}, Fit: "Regular", StyleTags: []string{"Casual", "Smart"}, Price: 899.00, Sizes: []string{"M", "L"}, ImageURL: "https://example.com/shirt.jpg"},
		{ID: "bbb-222", Name: "Black Slim Jeans", Category: "Bottomwear", Subcategory: "Jeans", Colors: []string{"Black"}, Fit: "Slim", StyleTags: []string{"Casual"}, Price: 1299.00, Sizes: []string{"30", "32"}, ImageURL: "https://example.com/jeans.jpg"},
		{ID: "ccc-333", Name: "White Sneakers", Category: "Footwear", Subcategory: "Sneakers", Colors: []string{"White"}, Fit: "", StyleTags: []string{"Casual", "Sport"}, Price: 1599.00, Sizes: []string{"8", "9", "10"}, ImageURL: "https://example.com/sneakers.jpg"},
	}
}

func newTestMux() (*http.ServeMux, *mockCatalogRepo) {
	repo := &mockCatalogRepo{
		products: seedProducts(),
		categories: []catalog.CategoryCount{
			{Category: "Topwear", Count: 15000},
			{Category: "Bottomwear", Count: 12000},
			{Category: "Footwear", Count: 8000},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /products", listProductsHandler(repo))
	mux.HandleFunc("GET /products/categories", listCategoriesHandler(repo))
	mux.HandleFunc("GET /products/{id}", getProductHandler(repo))
	mux.HandleFunc("POST /products/batch", batchGetProductsHandler(repo))
	return mux, repo
}

// --- GET /products ---

func TestListProducts_DefaultPagination(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp ProductListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Total != 3 {
		t.Errorf("expected total=3, got %d", resp.Total)
	}
	if resp.Offset != 0 {
		t.Errorf("expected offset=0, got %d", resp.Offset)
	}
	if resp.Limit != 20 {
		t.Errorf("expected limit=20, got %d", resp.Limit)
	}
	if len(resp.Products) != 3 {
		t.Errorf("expected 3 products, got %d", len(resp.Products))
	}
}

func TestListProducts_WithLimit(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("GET", "/products?limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp ProductListResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Products) != 2 {
		t.Errorf("expected 2 products, got %d", len(resp.Products))
	}
	if resp.Total != 3 {
		t.Errorf("total should still be 3, got %d", resp.Total)
	}
	if resp.Limit != 2 {
		t.Errorf("expected limit=2, got %d", resp.Limit)
	}
}

func TestListProducts_LimitCappedAt100(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("GET", "/products?limit=500", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp ProductListResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Limit != 100 {
		t.Errorf("expected limit capped at 100, got %d", resp.Limit)
	}
}

func TestListProducts_EmptyResult(t *testing.T) {
	repo := &mockCatalogRepo{products: nil}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /products", listProductsHandler(repo))

	req := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify we get [] not null
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"products":[]`)) {
		t.Errorf("expected empty array, got: %s", body)
	}
}

// --- GET /products/{id} ---

func TestGetProduct_Found(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("GET", "/products/aaa-111", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var p rag.Product
	json.NewDecoder(w.Body).Decode(&p)
	if p.ID != "aaa-111" {
		t.Errorf("expected id aaa-111, got %s", p.ID)
	}
	if p.Name != "Blue Oxford Shirt" {
		t.Errorf("expected Blue Oxford Shirt, got %s", p.Name)
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("GET", "/products/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- POST /products/batch ---

func TestBatchGet_AllFound(t *testing.T) {
	mux, _ := newTestMux()
	body, _ := json.Marshal(BatchGetRequest{IDs: []string{"aaa-111", "ccc-333"}})
	req := httptest.NewRequest("POST", "/products/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp BatchGetResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Products) != 2 {
		t.Errorf("expected 2 products, got %d", len(resp.Products))
	}
}

func TestBatchGet_PartialMatch(t *testing.T) {
	mux, _ := newTestMux()
	body, _ := json.Marshal(BatchGetRequest{IDs: []string{"aaa-111", "nonexistent"}})
	req := httptest.NewRequest("POST", "/products/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even for partial match, got %d", w.Code)
	}

	var resp BatchGetResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Products) != 1 {
		t.Errorf("expected 1 product, got %d", len(resp.Products))
	}
}

func TestBatchGet_EmptyIDs(t *testing.T) {
	mux, _ := newTestMux()
	body, _ := json.Marshal(BatchGetRequest{IDs: []string{}})
	req := httptest.NewRequest("POST", "/products/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	respBody := w.Body.String()
	if !bytes.Contains([]byte(respBody), []byte(`"products":[]`)) {
		t.Errorf("expected empty array, got: %s", respBody)
	}
}

func TestBatchGet_TooManyIDs(t *testing.T) {
	mux, _ := newTestMux()
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%d", i)
	}
	body, _ := json.Marshal(BatchGetRequest{IDs: ids})
	req := httptest.NewRequest("POST", "/products/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >100 ids, got %d", w.Code)
	}
}

func TestBatchGet_InvalidBody(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("POST", "/products/batch", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- GET /products/categories ---

func TestListCategories(t *testing.T) {
	mux, _ := newTestMux()
	req := httptest.NewRequest("GET", "/products/categories", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CategoriesResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Categories) != 3 {
		t.Errorf("expected 3 categories, got %d", len(resp.Categories))
	}
	if resp.Categories[0].Category != "Topwear" {
		t.Errorf("expected first category Topwear, got %s", resp.Categories[0].Category)
	}
	if resp.Categories[0].Count != 15000 {
		t.Errorf("expected count 15000, got %d", resp.Categories[0].Count)
	}
}

func TestListCategories_Empty(t *testing.T) {
	repo := &mockCatalogRepo{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /products/categories", listCategoriesHandler(repo))

	req := httptest.NewRequest("GET", "/products/categories", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"categories":[]`)) {
		t.Errorf("expected empty array, got: %s", body)
	}
}

// --- Helper tests ---

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		input    string
		def      int
		expected int
	}{
		{"", 20, 20},
		{"5", 20, 5},
		{"abc", 20, 20},
		{"0", 20, 0},
	}
	for _, tt := range tests {
		got := parseIntParam(tt.input, tt.def)
		if got != tt.expected {
			t.Errorf("parseIntParam(%q, %d) = %d, want %d", tt.input, tt.def, got, tt.expected)
		}
	}
}

func TestParseFloatParam(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"", 0},
		{"99.99", 99.99},
		{"abc", 0},
	}
	for _, tt := range tests {
		got := parseFloatParam(tt.input)
		if got != tt.expected {
			t.Errorf("parseFloatParam(%q) = %f, want %f", tt.input, got, tt.expected)
		}
	}
}
