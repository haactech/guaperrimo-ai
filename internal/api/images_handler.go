package api

import (
	"log/slog"
	"net/http"
	"path"
	"strings"

	"stylerag/internal/storage"
)

// imagesHandler streams a stored object when the signed URL checks out. The
// bucket never has to be public and the app loads images with a plain GET.
func imagesHandler(signed *storage.SignedURLs) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/images/")
		if key == "" || strings.Contains(key, "..") {
			writeError(w, http.StatusBadRequest, "invalid key")
			return
		}
		q := r.URL.Query()
		if err := signed.Verify(key, q.Get("exp"), q.Get("sig")); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		data, err := signed.Download(r.Context(), key)
		if err != nil {
			slog.WarnContext(r.Context(), "images: download failed", "key", key, "error", err)
			writeError(w, http.StatusNotFound, "image not found")
			return
		}
		ct := "application/octet-stream"
		switch strings.ToLower(path.Ext(key)) {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".png":
			ct = "image/png"
		case ".webp":
			ct = "image/webp"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
