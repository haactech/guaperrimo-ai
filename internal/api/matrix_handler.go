package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/tryon"
)

// MatrixDeps bundles what the mix-and-match handlers need.
type MatrixDeps struct {
	Store    session.Store
	Renderer *tryon.Renderer // nil = try-on unavailable; the grid still works without images
}

// --- DTOs ---

// MatrixOptionResponse is a product inside a row plus the stylist's reason.
type MatrixOptionResponse struct {
	shopping.Product
	Why      string `json:"why,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// MatrixSlotResponse is a swipeable row.
type MatrixSlotResponse struct {
	Slot    string                 `json:"slot"`
	Label   string                 `json:"label"`
	Options []MatrixOptionResponse `json:"options"`
}

// RenderResponse is the state of one combination's try-on image.
type RenderResponse struct {
	Key          string            `json:"key"`
	Selection    map[string]string `json:"selection"`
	Status       string            `json:"status"` // pending | generating | ready | failed
	ImageURL     string            `json:"image_url,omitempty"`
	Error        string            `json:"error,omitempty"`
	GenerationMs int64             `json:"generation_time_ms,omitempty"`
}

// MatrixResponse is the output of GET /session/{id}/matrix.
type MatrixResponse struct {
	SessionID    string               `json:"session_id"`
	Available    bool                 `json:"tryon_available"`
	BaseImageURL string               `json:"base_image_url"`
	Slots        []MatrixSlotResponse `json:"slots"`
	Default      map[string]string    `json:"default"`
	Renders      []RenderResponse     `json:"renders"`
	SavedLooks   []session.SavedLook  `json:"saved_looks"`
}

// RenderRequest is the input of POST /session/{id}/matrix/render.
type RenderRequest struct {
	Selection map[string]string `json:"selection"`
}

// SaveLookRequest is the input of POST /session/{id}/saved-looks.
type SaveLookRequest struct {
	Selection map[string]string `json:"selection"`
	Note      string            `json:"note,omitempty"`
}

// --- handlers ---

func matrixHandler(deps *MatrixDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, ok := loadSession(w, r, deps.Store)
		if !ok {
			return
		}
		if st.Recommendation == nil {
			writeError(w, http.StatusNotFound, "no recommendation yet")
			return
		}
		writeJSON(w, http.StatusOK, buildMatrixResponse(st, deps.Renderer != nil))
	}
}

func renderHandler(deps *MatrixDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, ok := loadSession(w, r, deps.Store)
		if !ok {
			return
		}
		if st.Matrix == nil {
			writeError(w, http.StatusNotFound, "no outfit grid for this session")
			return
		}
		if deps.Renderer == nil {
			writeError(w, http.StatusServiceUnavailable, "virtual try-on is not enabled")
			return
		}
		if st.ImageKey == "" {
			writeError(w, http.StatusConflict, "session has no photo")
			return
		}
		var req RenderRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		sel, msg := st.Matrix.Validate(session.Selection(req.Selection))
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		pieces := st.Matrix.Combo(sel)
		key := session.ComboKey(pieces)

		if rd, ok := st.Renders[key]; ok && rd.Status == session.LookStatusReady {
			writeJSON(w, http.StatusOK, renderResponse(st.Matrix, rd))
			return
		}

		// Mark as pending so polls see it, then render this one first and
		// its one-swipe neighbours speculatively.
		_ = deps.Store.Update(r.Context(), st.ID, func(cur *session.State) error {
			if cur.Renders == nil {
				cur.Renders = map[string]session.Render{}
			}
			if existing, ok := cur.Renders[key]; !ok || existing.Status == session.LookStatusFailed {
				cur.Renders[key] = session.Render{Key: key, Pieces: pieces, Status: session.LookStatusPending, UpdatedAt: time.Now()}
			}
			return nil
		})
		deps.Renderer.Request(st.ID, pieces, false)
		for _, n := range st.Matrix.Neighbors(sel) {
			np := st.Matrix.Combo(n)
			if rd, ok := st.Renders[session.ComboKey(np)]; ok && rd.Status != session.LookStatusFailed {
				continue
			}
			deps.Renderer.Request(st.ID, np, true)
		}

		status := session.LookStatusPending
		if rd, ok := st.Renders[key]; ok && rd.Status == session.LookStatusGenerating {
			status = rd.Status
		}
		writeJSON(w, http.StatusAccepted, RenderResponse{Key: key, Selection: sel, Status: string(status)})
	}
}

func saveLookHandler(deps *MatrixDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, ok := loadSession(w, r, deps.Store)
		if !ok {
			return
		}
		if st.Matrix == nil {
			writeError(w, http.StatusNotFound, "no outfit grid for this session")
			return
		}
		var req SaveLookRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		sel, msg := st.Matrix.Validate(session.Selection(req.Selection))
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		saved := buildSavedLook(st, sel, strings.TrimSpace(req.Note))
		if err := deps.Store.Update(r.Context(), st.ID, func(cur *session.State) error {
			kept := cur.SavedLooks[:0]
			for _, s := range cur.SavedLooks {
				if s.Key != saved.Key {
					kept = append(kept, s)
				}
			}
			cur.SavedLooks = append(kept, saved)
			return nil
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save look")
			return
		}
		writeJSON(w, http.StatusOK, saved)
	}
}

func savedLooksHandler(store session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, ok := loadSession(w, r, store)
		if !ok {
			return
		}
		looks := st.SavedLooks
		if looks == nil {
			looks = []session.SavedLook{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"session_id": st.ID, "saved_looks": looks})
	}
}

// --- helpers ---

func loadSession(w http.ResponseWriter, r *http.Request, store session.Store) (*session.State, bool) {
	sessionID := r.PathValue("id")
	if !sessionIDPattern.MatchString(sessionID) {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return nil, false
	}
	st, err := store.Get(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found or expired")
		return nil, false
	}
	return st, true
}

func buildMatrixResponse(st *session.State, tryonEnabled bool) MatrixResponse {
	resp := MatrixResponse{
		SessionID:    st.ID,
		Available:    tryonEnabled && st.Matrix != nil && st.ImageKey != "",
		BaseImageURL: st.ImageURL,
		Slots:        []MatrixSlotResponse{},
		Default:      map[string]string{},
		Renders:      []RenderResponse{},
		SavedLooks:   st.SavedLooks,
	}
	if resp.SavedLooks == nil {
		resp.SavedLooks = []session.SavedLook{}
	}
	if st.Matrix == nil {
		return resp
	}
	resp.Default = st.Matrix.Default
	for _, s := range st.Matrix.Slots {
		row := MatrixSlotResponse{Slot: s.Slot, Label: s.Label}
		for _, o := range s.Options {
			p, ok := st.Products[o.ProductID]
			if !ok {
				continue
			}
			row.Options = append(row.Options, MatrixOptionResponse{Product: p, Why: o.Why, Priority: o.Priority})
		}
		resp.Slots = append(resp.Slots, row)
	}
	for _, rd := range st.Renders {
		if len(rd.Pieces) != len(st.Matrix.Slots) {
			continue // prefixes are internal
		}
		resp.Renders = append(resp.Renders, renderResponse(st.Matrix, rd))
	}
	sort.Slice(resp.Renders, func(i, j int) bool { return resp.Renders[i].Key < resp.Renders[j].Key })
	return resp
}

func renderResponse(m *session.Matrix, rd session.Render) RenderResponse {
	sel := map[string]string{}
	for _, p := range rd.Pieces {
		sel[p.Slot] = p.ProductID
	}
	return RenderResponse{Key: rd.Key, Selection: sel, Status: string(rd.Status), ImageURL: rd.ImageURL, Error: rd.Error, GenerationMs: rd.GenerationMs}
}

func buildSavedLook(st *session.State, sel session.Selection, note string) session.SavedLook {
	pieces := st.Matrix.Combo(sel)
	key := session.ComboKey(pieces)
	saved := session.SavedLook{
		ID:        fmt.Sprintf("saved_%d", time.Now().UnixMilli()),
		Key:       key,
		Selection: sel,
		Note:      note,
		CreatedAt: time.Now(),
	}
	if rd, ok := st.Renders[key]; ok && rd.Status == session.LookStatusReady {
		saved.ImageURL = rd.ImageURL
	}
	whyFor := func(slot, id string) string {
		for _, s := range st.Matrix.Slots {
			if s.Slot != slot {
				continue
			}
			for _, o := range s.Options {
				if o.ProductID == id {
					return o.Why
				}
			}
		}
		return ""
	}
	storeSeen := map[string]bool{}
	for _, piece := range pieces {
		p, ok := st.Products[piece.ProductID]
		if !ok {
			continue
		}
		saved.Items = append(saved.Items, session.SavedItem{Slot: piece.Slot, Product: p, Why: whyFor(piece.Slot, piece.ProductID)})
		saved.TotalMXN += p.Price
		if p.NearbyStore != nil && !storeSeen[p.NearbyStore.PlaceID] {
			for _, s := range st.Stores {
				if s.PlaceID == p.NearbyStore.PlaceID {
					saved.Stores = append(saved.Stores, s)
					storeSeen[s.PlaceID] = true
					break
				}
			}
		}
	}
	if saved.Stores == nil {
		saved.Stores = []shopping.Store{}
	}
	sort.Slice(saved.Stores, func(i, j int) bool { return saved.Stores[i].DistanceM < saved.Stores[j].DistanceM })
	return saved
}
