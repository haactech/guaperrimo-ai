package api

import (
	"net/http"

	"stylerag/internal/session"
)

func looksHandler(store session.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if sessionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
			return
		}

		state, err := store.Get(sessionID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found or expired"})
			return
		}

		if len(state.Looks) == 0 {
			writeJSON(w, http.StatusOK, LooksResponse{
				SessionID:  sessionID,
				Status:     "none",
				Looks:      []LookDTO{},
				LookResults: []LookResultDTO{},
				TotalCount: 0,
			})
			return
		}

		// Map looks to DTOs
		lookDTOs := make([]LookDTO, len(state.Looks))
		for i, l := range state.Looks {
			pieces := make([]LookPieceDTO, len(l.Pieces))
			for j, p := range l.Pieces {
				pieces[j] = LookPieceDTO{
					Slot:        p.Slot,
					Description: p.Description,
					Category:    p.Category,
				}
			}
			lookDTOs[i] = LookDTO{
				ID:          l.ID,
				Name:        l.Name,
				Description: l.Description,
				Vibe:        l.Vibe,
				Pieces:      pieces,
			}
		}

		// Map results to DTOs
		resultDTOs := make([]LookResultDTO, len(state.LookResults))
		readyCount := 0
		for i, lr := range state.LookResults {
			pieces := make([]PieceResultDTO, len(lr.Pieces))
			for j, p := range lr.Pieces {
				pieces[j] = PieceResultDTO{
					Slot:            p.Slot,
					ProductID:       p.ProductID,
					ProductName:     p.ProductName,
					ProductImageURL: p.ProductImageURL,
					TryOnImageURL:   p.TryOnImageURL,
					Category:        p.Category,
					GenerationMs:    p.GenerationMs,
				}
			}
			resultDTOs[i] = LookResultDTO{
				LookID:        lr.LookID,
				Status:        string(lr.Status),
				Pieces:        pieces,
				FinalImageURL: lr.FinalImageURL,
				ErrorMessage:  lr.ErrorMessage,
				GenerationMs:  lr.GenerationMs,
			}
			if lr.Status == session.LookStatusReady || lr.Status == session.LookStatusFailed {
				readyCount++
			}
		}

		totalCount := len(state.Looks)
		allReady := readyCount >= totalCount

		// Compute overall status
		status := "pending"
		if allReady {
			status = "ready"
		} else if readyCount > 0 {
			status = "partial"
		} else {
			// Check if any are generating
			for _, lr := range state.LookResults {
				if lr.Status == session.LookStatusGenerating {
					status = "generating"
					break
				}
			}
		}

		writeJSON(w, http.StatusOK, LooksResponse{
			SessionID:   sessionID,
			Status:      status,
			Looks:       lookDTOs,
			LookResults: resultDTOs,
			AllReady:    allReady,
			ReadyCount:  readyCount,
			TotalCount:  totalCount,
		})
	}
}
