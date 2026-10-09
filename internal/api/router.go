// Package api exposes the HTTP surface consumed by the iOS app.
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"stylerag/internal/session"
	"stylerag/internal/storage"
)

// Deps wires the handlers.
type Deps struct {
	APIKey string // optional; when set, X-API-Key is required on every route but /health
	Images storage.ImageStore
	Store  session.Store
	Chat   *ChatDeps
	TryOn  *TryOnDeps          // nil = disabled
	Matrix *MatrixDeps         // Renderer may be nil
	Signed *storage.SignedURLs // nil = no image proxy
}

// NewRouter builds the HTTP handler.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	if d.Signed != nil {
		mux.HandleFunc("GET /images/", imagesHandler(d.Signed))
	}
	mux.HandleFunc("POST /session/{id}/image", imageUploadHandler(d.Images))
	mux.HandleFunc("POST /session/{id}/chat", chatHandler(d.Chat))
	mux.HandleFunc("GET /session/{id}/recommendation", recommendationHandler(d.Store))
	mux.HandleFunc("GET /session/{id}/looks", looksHandler(d.Store))
	if d.TryOn != nil {
		mux.HandleFunc("POST /session/{id}/tryon", tryonHandler(d.TryOn))
	}
	if d.Matrix == nil {
		d.Matrix = &MatrixDeps{Store: d.Store}
	}
	mux.HandleFunc("GET /session/{id}/matrix", matrixHandler(d.Matrix))
	mux.HandleFunc("POST /session/{id}/matrix/render", renderHandler(d.Matrix))
	mux.HandleFunc("GET /session/{id}/saved-looks", savedLooksHandler(d.Store))
	mux.HandleFunc("POST /session/{id}/saved-looks", saveLookHandler(d.Matrix))

	var handler http.Handler = mux
	handler = APIKeyMiddleware(d.APIKey, handler)
	handler = LoggingMiddleware(handler)
	handler = PanicRecoveryMiddleware(handler)
	handler = otelhttp.NewHandler(handler, "guaperrimo",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
	return handler
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// APIKeyMiddleware enforces a shared secret when one is configured.
func APIKeyMiddleware(key string, next http.Handler) http.Handler {
	if key == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/images/") {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-API-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
