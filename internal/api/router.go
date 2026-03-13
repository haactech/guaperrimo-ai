package api

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"stylerag/internal/catalog"
	"stylerag/internal/config"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

func NewRouter(cfg *config.Config, imageStore storage.ImageStore, analyzer vision.Analyzer, advisor *vision.StyleAdvisor, chatDeps *ChatDeps, catalogRepo catalog.Repository, tryonDeps *TryOnDeps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /ready", readyHandler)

	mux.HandleFunc("POST /session/{id}/image", imageUploadHandler(imageStore))
	mux.HandleFunc("POST /session/{id}/analyze", analyzeHandler(imageStore, analyzer, advisor))

	if chatDeps != nil {
		mux.HandleFunc("POST /session/{id}/chat", chatHandler(chatDeps))
		mux.HandleFunc("GET /session/{id}/looks", looksHandler(chatDeps.Store))
	}

	if tryonDeps != nil {
		mux.HandleFunc("POST /session/{id}/tryon", tryonHandler(tryonDeps))
	}

	if catalogRepo != nil {
		mux.HandleFunc("GET /products", listProductsHandler(catalogRepo))
		mux.HandleFunc("GET /products/categories", listCategoriesHandler(catalogRepo))
		mux.HandleFunc("GET /products/{id}", getProductHandler(catalogRepo))
		mux.HandleFunc("POST /products/batch", batchGetProductsHandler(catalogRepo))
	}

	if cfg.ImageDir != "" {
		imageFS := http.StripPrefix("/images/products/", http.FileServer(http.Dir(cfg.ImageDir)))
		mux.Handle("GET /images/products/", imageFS)
	}

	// Middleware chain: otelhttp (outer) → PanicRecovery → Logging → mux
	var handler http.Handler = mux
	handler = LoggingMiddleware(handler)
	handler = PanicRecoveryMiddleware(handler)
	handler = otelhttp.NewHandler(handler, "stylerag",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	return handler
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
