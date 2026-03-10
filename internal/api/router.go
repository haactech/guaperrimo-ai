package api

import (
	"net/http"

	"stylerag/internal/config"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

func NewRouter(cfg *config.Config, imageStore storage.ImageStore, analyzer vision.Analyzer, advisor *vision.StyleAdvisor, chatDeps *ChatDeps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /ready", readyHandler)

	mux.HandleFunc("POST /session/{id}/image", imageUploadHandler(imageStore))
	mux.HandleFunc("POST /session/{id}/analyze", analyzeHandler(imageStore, analyzer, advisor))

	if chatDeps != nil {
		mux.HandleFunc("POST /session/{id}/chat", chatHandler(chatDeps))
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
