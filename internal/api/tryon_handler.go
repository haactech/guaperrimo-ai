package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
	"stylerag/internal/tryon"
)

// TryOnDeps bundles what the try-on handler needs.
type TryOnDeps struct {
	Store      session.Store
	Images     storage.ImageStore
	VTON       tryon.VTONProvider
	Timeout    time.Duration
	HTTPClient *http.Client
}

func tryonHandler(deps *TryOnDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if !sessionIDPattern.MatchString(sessionID) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		var req TryOnRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.ProductID == "" && req.ActionID == "" {
			writeError(w, http.StatusBadRequest, "missing field: product_id or action_id")
			return
		}
		ctx := r.Context()

		st, err := deps.Store.Get(ctx, sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found or expired")
			return
		}
		if st.Recommendation == nil {
			writeError(w, http.StatusConflict, "try-on is only available after a recommendation")
			return
		}
		if st.ImageKey == "" {
			writeError(w, http.StatusConflict, "no user image in session")
			return
		}

		product, ok := resolveProduct(st, req)
		if !ok {
			writeError(w, http.StatusNotFound, "no product found for that action")
			return
		}
		if product.Thumbnail == "" {
			writeError(w, http.StatusUnprocessableEntity, "product has no image to try on")
			return
		}

		if cached := st.FindTryOn(product.ID); cached != nil {
			writeJSON(w, http.StatusOK, tryOnResponse(sessionID, req.ActionID, product, cached.TryOnImageURL, cached.GenerationMs))
			return
		}

		garment, err := tryon.DownloadImage(ctx, deps.HTTPClient, product.Thumbnail, 10<<20)
		if err != nil {
			slog.ErrorContext(ctx, "tryon: garment download", "error", err, "url", product.Thumbnail)
			writeError(w, http.StatusBadGateway, "failed to download garment image")
			return
		}
		person, err := deps.Images.Download(ctx, st.ImageKey)
		if err != nil {
			slog.ErrorContext(ctx, "tryon: person download", "error", err, "key", st.ImageKey)
			writeError(w, http.StatusInternalServerError, "failed to retrieve user image")
			return
		}

		timeout := deps.Timeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		vtonCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		out, err := deps.VTON.Generate(vtonCtx, tryon.VTONRequest{PersonImage: person, GarmentImage: garment, GarmentDesc: product.Title})
		if err != nil {
			if vtonCtx.Err() != nil {
				writeError(w, http.StatusGatewayTimeout, "try-on generation timed out")
				return
			}
			slog.ErrorContext(ctx, "tryon: generation failed", "error", err, "session_id", sessionID)
			writeError(w, http.StatusBadGateway, "try-on generation failed")
			return
		}

		key := fmt.Sprintf("sessions/%s/tryon_%s_%d.jpg", sessionID, sanitizeKeyPart(product.ID), time.Now().UnixMilli())
		up, err := deps.Images.Upload(ctx, storage.UploadInput{Key: key, Body: bytes.NewReader(out.ImageBytes), ContentType: out.MimeType})
		if err != nil {
			slog.ErrorContext(ctx, "tryon: upload", "error", err, "session_id", sessionID)
			writeError(w, http.StatusInternalServerError, "failed to store result image")
			return
		}

		result := session.TryOnResult{
			ProductID: product.ID, ActionID: req.ActionID, TryOnImageURL: up.URL, GarmentName: product.Title,
			GarmentImageURL: product.Thumbnail, GenerationMs: out.GenerationMs, ProviderName: out.ProviderName, CreatedAt: time.Now(),
		}
		if err := deps.Store.Update(ctx, sessionID, func(cur *session.State) error {
			cur.TryOnResults = append(cur.TryOnResults, result)
			return nil
		}); err != nil {
			slog.ErrorContext(ctx, "tryon: save", "error", err, "session_id", sessionID)
		}

		writeJSON(w, http.StatusOK, tryOnResponse(sessionID, req.ActionID, product, up.URL, out.GenerationMs))
	}
}

func resolveProduct(st *session.State, req TryOnRequest) (shopping.Product, bool) {
	if req.ProductID != "" {
		p, ok := st.Products[req.ProductID]
		return p, ok
	}
	for _, a := range st.Recommendation.PriorityActions {
		if a.ID == req.ActionID {
			for _, p := range st.ProductsFor(a.ProductIDs) {
				if p.Thumbnail != "" {
					return p, true
				}
			}
		}
	}
	// Fall back to the first shopping-list product with an image.
	for _, item := range st.Recommendation.ShoppingList {
		for _, p := range st.ProductsFor(item.ProductIDs) {
			if p.Thumbnail != "" && strings.EqualFold(item.Slot, slotForAction(req)) {
				return p, true
			}
		}
	}
	return shopping.Product{}, false
}

func slotForAction(req TryOnRequest) string {
	text := strings.ToLower(req.ActionID + " " + req.GarmentDescription)
	for _, kw := range []string{"pantal", "jeans", "chino", "short", "bermuda"} {
		if strings.Contains(text, kw) {
			return "lower_body"
		}
	}
	for _, kw := range []string{"zapat", "tenis", "bota", "sneaker", "mocas"} {
		if strings.Contains(text, kw) {
			return "footwear"
		}
	}
	return "upper_body"
}

func tryOnResponse(sessionID, actionID string, p shopping.Product, url string, ms int64) TryOnResponse {
	return TryOnResponse{
		SessionID: sessionID, ActionID: actionID, ProductID: p.ID, TryOnImageURL: url,
		GarmentUsed:  GarmentInfo{Name: p.Title, Source: p.Store, CatalogID: p.ID, ImageURL: p.Thumbnail, Link: p.Link},
		GenerationMs: ms,
	}
}

func sanitizeKeyPart(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
