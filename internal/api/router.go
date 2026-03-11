package api

import (
	"net/http"

	"stylerag/internal/catalog"
	"stylerag/internal/config"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

func NewRouter(cfg *config.Config, imageStore storage.ImageStore, analyzer vision.Analyzer, advisor *vision.StyleAdvisor, chatDeps *ChatDeps, catalogRepo catalog.Repository) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /ready", readyHandler)

	mux.HandleFunc("POST /session/{id}/image", imageUploadHandler(imageStore))
	mux.HandleFunc("POST /session/{id}/analyze", analyzeHandler(imageStore, analyzer, advisor))

	if chatDeps != nil {
		mux.HandleFunc("POST /session/{id}/chat", chatHandler(chatDeps))
	}

	if catalogRepo != nil {
		mux.HandleFunc("GET /products", listProductsHandler(catalogRepo))
		mux.HandleFunc("GET /products/categories", listCategoriesHandler(catalogRepo))
		mux.HandleFunc("GET /products/{id}", getProductHandler(catalogRepo))
		mux.HandleFunc("POST /products/batch", batchGetProductsHandler(catalogRepo))
	}

	return mux
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Check database connections
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}
