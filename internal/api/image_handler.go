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
		if !sessionIDPattern.MatchString(sessionID) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxImageSize)
		file, header, err := r.FormFile("image")
		if err != nil {
			if strings.Contains(err.Error(), "request body too large") {
				writeError(w, http.StatusRequestEntityTooLarge, "image exceeds 10MB limit")
				return
			}
			writeError(w, http.StatusBadRequest, "missing or invalid image field")
			return
		}
		defer file.Close()

		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = detectContentType(header.Filename)
		}
		ext, ok := allowedContentTypes[contentType]
		if !ok {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported image type, allowed: JPEG, PNG, WebP")
			return
		}

		key := fmt.Sprintf("sessions/%s/welcome_%d%s", sessionID, time.Now().UnixMilli(), ext)
		out, err := store.Upload(r.Context(), storage.UploadInput{Key: key, Body: file, ContentType: contentType})
		if err != nil {
			slog.ErrorContext(r.Context(), "image upload failed", "error", err, "session_id", sessionID)
			writeError(w, http.StatusInternalServerError, "upload failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": out.URL, "session_id": sessionID})
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
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	return dec.Decode(v)
}
