package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"stylerag/internal/imageutil"
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

		var body io.Reader = file
		if contentType == "image/jpeg" {
			// Phones store portraits sideways plus an EXIF flag that some
			// try-on models ignore; store upright pixels without metadata
			// (this also drops GPS). Keep the original if it cannot be decoded.
			raw, err := io.ReadAll(file)
			if err != nil {
				writeError(w, http.StatusBadRequest, "could not read image")
				return
			}
			body = bytes.NewReader(raw)
			if upright, orientation, err := imageutil.UprightJPEG(raw); err != nil {
				slog.WarnContext(r.Context(), "image upload: kept original jpeg", "session_id", sessionID, "error", err)
			} else {
				body = bytes.NewReader(upright)
				if orientation != 1 {
					slog.InfoContext(r.Context(), "image upload: rotated to upright", "session_id", sessionID, "exif_orientation", orientation)
				}
			}
		}

		key := fmt.Sprintf("sessions/%s/welcome_%d%s", sessionID, time.Now().UnixMilli(), ext)
		out, err := store.Upload(r.Context(), storage.UploadInput{Key: key, Body: body, ContentType: contentType})
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
