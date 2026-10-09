package api

import (
	"net/http"

	"stylerag/internal/session"
)

func looksHandler(store session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if !sessionIDPattern.MatchString(sessionID) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		st, err := store.Get(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found or expired")
			return
		}
		if len(st.Looks) == 0 {
			writeJSON(w, http.StatusOK, LooksResponse{SessionID: sessionID, Status: "none", Looks: []LookDTO{}, LookResults: []LookResultDTO{}})
			return
		}

		looks := make([]LookDTO, 0, len(st.Looks))
		for _, l := range st.Looks {
			pieces := make([]LookPieceDTO, 0, len(l.Pieces))
			for _, p := range l.Pieces {
				dto := LookPieceDTO{Slot: p.Slot, Description: p.Description, Category: p.Slot, ProductID: p.ProductID}
				if prod, ok := st.Products[p.ProductID]; ok {
					dto.ProductName = prod.Title
					dto.ProductImageURL = prod.Thumbnail
					dto.ProductLink = prod.Link
					dto.Price = prod.Price
					dto.Store = prod.Store
					if dto.Description == "" {
						dto.Description = prod.Title
					}
				}
				pieces = append(pieces, dto)
			}
			looks = append(looks, LookDTO{ID: l.ID, Name: l.Name, Description: l.Description, Vibe: l.Vibe, Pieces: pieces})
		}

		results := make([]LookResultDTO, 0, len(st.LookResults))
		done, generating := 0, false
		for _, lr := range st.LookResults {
			pieces := make([]PieceResultDTO, 0, len(lr.Pieces))
			for _, p := range lr.Pieces {
				pieces = append(pieces, PieceResultDTO{
					Slot: p.Slot, ProductID: p.ProductID, ProductName: p.ProductName, ProductImageURL: p.ProductImageURL,
					TryOnImageURL: p.TryOnImageURL, Category: p.Slot, GenerationMs: p.GenerationMs,
				})
			}
			results = append(results, LookResultDTO{
				LookID: lr.LookID, Status: string(lr.Status), Pieces: pieces, FinalImageURL: lr.FinalImageURL,
				ErrorMessage: lr.ErrorMessage, GenerationMs: lr.GenerationMs,
			})
			switch lr.Status {
			case session.LookStatusReady, session.LookStatusFailed:
				done++
			case session.LookStatusGenerating:
				generating = true
			}
		}

		total := len(st.Looks)
		allReady := done >= total
		status := "pending"
		switch {
		case len(st.LookResults) == 0:
			status = "none"
			allReady = false
		case allReady:
			status = "ready"
		case done > 0:
			status = "partial"
		case generating:
			status = "generating"
		}
		writeJSON(w, http.StatusOK, LooksResponse{
			SessionID: sessionID, Status: status, Looks: looks, LookResults: results,
			AllReady: allReady, ReadyCount: done, TotalCount: total,
		})
	}
}
