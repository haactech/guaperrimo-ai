package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"stylerag/internal/storage"
)

const maxImageSize = 10 << 20 // 10 MB

var allowedContentTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

func imageUploadHandler(store storage.ImageStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if sessionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxImageSize)

		file, header, err := r.FormFile("image")
		if err != nil {
			if err.Error() == "http: request body too large" {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "image exceeds 10MB limit"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid image field"})
			return
		}
		defer file.Close()

		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = detectContentType(header.Filename)
		}
		ext, ok := allowedContentTypes[contentType]
		if !ok {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
				"error": "unsupported image type, allowed: JPEG, PNG, WebP",
			})
			return
		}

		key := fmt.Sprintf("sessions/%s/welcome_%d%s", sessionID, time.Now().UnixMilli(), ext)

		out, err := store.Upload(r.Context(), storage.UploadInput{
			Key:         key,
			Body:        file,
			ContentType: contentType,
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "image upload failed", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"url":        out.URL,
			"session_id": sessionID,
		})
	}
}

func detectContentType(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
