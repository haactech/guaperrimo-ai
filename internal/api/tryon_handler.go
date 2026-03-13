package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"stylerag/internal/rag"
	"stylerag/internal/session"
	"stylerag/internal/storage"
	"stylerag/internal/tryon"
)

// TryOnDeps bundles dependencies for the try-on handler.
type TryOnDeps struct {
	Store        session.SessionStore
	ImageStore   storage.ImageStore
	RAGEngine    rag.Engine
	VTONProvider tryon.VTONProvider
}

func tryonHandler(deps *TryOnDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if sessionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
			return
		}

		var req TryOnRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if req.ActionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing field: action_id"})
			return
		}

		ctx := r.Context()

		state, err := deps.Store.Get(sessionID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found or expired"})
			return
		}
		if state.Phase != session.PhaseRecommendation {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "try-on is only available after recommendations"})
			return
		}
		if state.ImageKey == "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no user image found in session"})
			return
		}

		// Cache check
		if cached := findTryOnResult(state.TryOnResults, req.ActionID); cached != nil {
			slog.InfoContext(ctx, "tryon: cache hit", "session_id", sessionID, "action_id", req.ActionID)
			writeJSON(w, http.StatusOK, TryOnResponse{
				SessionID:    sessionID,
				ActionID:     cached.ActionID,
				TryOnImageURL: cached.TryOnImageURL,
				GarmentUsed: GarmentInfo{
					Name:      cached.GarmentName,
					Source:    cached.GarmentSource,
					CatalogID: cached.CatalogID,
					ImageURL:  cached.GarmentImageURL,
				},
				GenerationMs: cached.GenerationMs,
			})
			return
		}

		// Category mapping
		category := tryon.MapActionToCategory(req.ActionID, req.GarmentDescription)

		// RAG search for the garment
		if deps.RAGEngine == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "product search not available"})
			return
		}

		searchDesc := req.GarmentDescription
		if searchDesc == "" {
			searchDesc = req.ActionID
		}
		products, err := deps.RAGEngine.Search(ctx, rag.SearchQuery{
			Text:       searchDesc,
			Categories: []string{category},
			Limit:      1,
		})
		if err != nil {
			slog.ErrorContext(ctx, "tryon: RAG search failed", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "product search failed"})
			return
		}
		if len(products) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no matching garment found"})
			return
		}
		product := products[0]

		// Download garment image
		garmentBytes, err := downloadExternalImage(ctx, product.ImageURL)
		if err != nil {
			slog.ErrorContext(ctx, "tryon: failed to download garment image", "error", err, "url", product.ImageURL)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to download garment image"})
			return
		}

		// Download person image from R2
		personBytes, err := deps.ImageStore.Download(ctx, state.ImageKey)
		if err != nil {
			slog.ErrorContext(ctx, "tryon: failed to download person image", "error", err, "key", state.ImageKey)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve user image"})
			return
		}

		// Call VTON provider with timeout
		vtonCtx, vtonCancel := context.WithTimeout(ctx, 30*time.Second)
		defer vtonCancel()

		vtonResult, err := deps.VTONProvider.Generate(vtonCtx, tryon.VTONRequest{
			PersonImage:  personBytes,
			GarmentImage: garmentBytes,
			GarmentDesc:  req.GarmentDescription,
			Category:     category,
		})
		if err != nil {
			if vtonCtx.Err() != nil {
				writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "try-on generation timed out"})
				return
			}
			slog.ErrorContext(ctx, "tryon: VTON generation failed", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try-on generation failed"})
			return
		}

		// Upload result to R2
		resultKey := fmt.Sprintf("sessions/%s/tryon_%s_%d.jpg", sessionID, req.ActionID, time.Now().UnixMilli())
		uploadOut, err := deps.ImageStore.Upload(ctx, storage.UploadInput{
			Key:         resultKey,
			Body:        bytes.NewReader(vtonResult.ImageBytes),
			ContentType: vtonResult.MimeType,
		})
		if err != nil {
			slog.ErrorContext(ctx, "tryon: failed to upload result", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store result image"})
			return
		}

		// Cache in session
		tryonResult := session.TryOnResult{
			ActionID:        req.ActionID,
			TryOnImageURL:   uploadOut.URL,
			GarmentName:     product.Name,
			GarmentSource:   "catalog",
			CatalogID:       product.ID,
			GarmentImageURL: product.ImageURL,
			GenerationMs:    vtonResult.GenerationMs,
			ProviderName:    vtonResult.ProviderName,
			CreatedAt:       time.Now(),
		}
		state.TryOnResults = append(state.TryOnResults, tryonResult)

		if err := deps.Store.Save(state); err != nil {
			slog.ErrorContext(ctx, "tryon: failed to save session", "error", err, "session_id", sessionID)
		}

		slog.InfoContext(ctx, "tryon: generation complete",
			"session_id", sessionID,
			"action_id", req.ActionID,
			"provider", vtonResult.ProviderName,
			"generation_ms", vtonResult.GenerationMs,
			"garment", product.Name,
		)

		writeJSON(w, http.StatusOK, TryOnResponse{
			SessionID:    sessionID,
			ActionID:     req.ActionID,
			TryOnImageURL: uploadOut.URL,
			GarmentUsed: GarmentInfo{
				Name:      product.Name,
				Source:    "catalog",
				CatalogID: product.ID,
				ImageURL:  product.ImageURL,
			},
			GenerationMs: vtonResult.GenerationMs,
		})
	}
}

func downloadExternalImage(ctx context.Context, url string) ([]byte, error) {
	dlCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image fetch returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading image body: %w", err)
	}
	return data, nil
}

func findTryOnResult(results []session.TryOnResult, actionID string) *session.TryOnResult {
	for i := range results {
		if results[i].ActionID == actionID {
			return &results[i]
		}
	}
	return nil
}
