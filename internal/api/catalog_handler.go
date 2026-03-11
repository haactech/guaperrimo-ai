package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"stylerag/internal/catalog"
	"stylerag/internal/rag"
)

func listProductsHandler(repo catalog.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		filter := catalog.ProductFilter{
			Category:    q.Get("category"),
			Subcategory: q.Get("subcategory"),
			Fit:         q.Get("fit"),
		}
		if v := q.Get("colors"); v != "" {
			filter.Colors = strings.Split(v, ",")
		}
		if v := q.Get("style_tags"); v != "" {
			filter.StyleTags = strings.Split(v, ",")
		}
		filter.MinPrice = parseFloatParam(q.Get("min_price"))
		filter.MaxPrice = parseFloatParam(q.Get("max_price"))

		offset := parseIntParam(q.Get("offset"), 0)
		limit := parseIntParam(q.Get("limit"), 20)
		if limit > 100 {
			limit = 100
		}
		if limit < 1 {
			limit = 20
		}

		result, err := repo.FilterProducts(r.Context(), filter, offset, limit)
		if err != nil {
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		products := result.Products
		if products == nil {
			products = []rag.Product{}
		}

		writeJSON(w, http.StatusOK, ProductListResponse{
			Products: products,
			Total:    result.Total,
			Offset:   offset,
			Limit:    limit,
		})
	}
}

func getProductHandler(repo catalog.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, `{"error":"missing product id"}`, http.StatusBadRequest)
			return
		}

		product, err := repo.GetProduct(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}

		writeJSON(w, http.StatusOK, product)
	}
}

func batchGetProductsHandler(repo catalog.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BatchGetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if len(req.IDs) == 0 {
			writeJSON(w, http.StatusOK, BatchGetResponse{Products: []rag.Product{}})
			return
		}
		if len(req.IDs) > 100 {
			http.Error(w, `{"error":"max 100 ids allowed"}`, http.StatusBadRequest)
			return
		}

		products, err := repo.GetProducts(r.Context(), req.IDs)
		if err != nil {
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		if products == nil {
			products = []rag.Product{}
		}

		writeJSON(w, http.StatusOK, BatchGetResponse{Products: products})
	}
}

func listCategoriesHandler(repo catalog.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categories, err := repo.ListCategories(r.Context())
		if err != nil {
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		if categories == nil {
			categories = []catalog.CategoryCount{}
		}

		writeJSON(w, http.StatusOK, CategoriesResponse{Categories: categories})
	}
}

func parseIntParam(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

func parseFloatParam(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
