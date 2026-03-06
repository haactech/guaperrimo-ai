package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

func analyzeHandler(store storage.ImageStore, analyzer vision.Analyzer, advisor *vision.StyleAdvisor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if sessionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
			return
		}

		slog.Info("analyze: started", "session_id", sessionID)
		start := time.Now()

		ctx := r.Context()
		prefix := fmt.Sprintf("sessions/%s/", sessionID)

		// Find the most recent image for this session
		slog.Info("analyze: listing keys", "prefix", prefix)
		keys, err := store.ListKeys(ctx, prefix)
		if err != nil {
			slog.Error("analyze: failed to list keys", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve session images"})
			return
		}
		slog.Info("analyze: keys found", "count", len(keys), "keys", keys)

		if len(keys) == 0 {
			slog.Warn("analyze: no images found", "session_id", sessionID, "prefix", prefix)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no image found for this session"})
			return
		}

		// Keys include timestamps, so sorting gives us chronological order
		sort.Strings(keys)
		latestKey := keys[len(keys)-1]
		slog.Info("analyze: downloading image", "key", latestKey)

		// Download image bytes
		imageData, err := store.Download(ctx, latestKey)
		if err != nil {
			slog.Error("analyze: download failed", "error", err, "key", latestKey)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to download image"})
			return
		}
		slog.Info("analyze: image downloaded", "size_bytes", len(imageData), "elapsed", time.Since(start))

		// Analyze outfit
		slog.Info("analyze: calling LLM analyzer (potent)")
		analysisStart := time.Now()
		analysis, err := analyzer.AnalyzeOutfit(ctx, imageData)
		if err != nil {
			slog.Error("analyze: outfit analysis failed", "error", err, "session_id", sessionID, "elapsed", time.Since(analysisStart))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "image analysis failed"})
			return
		}
		slog.Info("analyze: outfit analysis done",
			"styles", analysis.DetectedStyles,
			"items_count", len(analysis.DetectedItems),
			"elapsed", time.Since(analysisStart),
		)

		// Generate conversational style advice
		slog.Info("analyze: calling style advisor (economy)")
		advisorStart := time.Now()
		advice, err := advisor.GenerateStyleAdvice(ctx, analysis)
		if err != nil {
			slog.Error("analyze: style advice failed", "error", err, "session_id", sessionID, "elapsed", time.Since(advisorStart))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "style advice generation failed"})
			return
		}
		slog.Info("analyze: style advice done",
			"options_count", len(advice.Options),
			"elapsed", time.Since(advisorStart),
		)

		// Map to iOS-compatible response
		options := make([]StyleOptionResponse, len(advice.Options))
		for i, opt := range advice.Options {
			options[i] = StyleOptionResponse{
				ID:          opt.ID,
				Title:       opt.Title,
				Description: opt.Description,
			}
		}

		slog.Info("analyze: completed", "session_id", sessionID, "total_elapsed", time.Since(start))
		writeJSON(w, http.StatusOK, StyleAnalysisResponse{
			Message:  advice.Message,
			Options:  options,
			Analysis: analysis.Observations,
		})
	}
}
