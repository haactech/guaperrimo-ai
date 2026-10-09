package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"stylerag/internal/agent"
	"stylerag/internal/session"
	"stylerag/internal/storage"
	"stylerag/internal/tryon"
	"stylerag/internal/vision"
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,80}$`)

var errNoPhoto = errors.New("no photo uploaded for this session")

// ChatDeps bundles what the chat handler needs.
type ChatDeps struct {
	Store       session.Store
	Images      storage.ImageStore
	Analyzer    vision.Analyzer
	Runner      *agent.Runner
	Looks       *tryon.LookGenerator // nil = try-on disabled
	TurnTimeout time.Duration
}

func chatHandler(deps *ChatDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if !sessionIDPattern.MatchString(sessionID) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		var req ChatRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		input, err := turnInput(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		timeout := deps.TurnTimeout
		if timeout <= 0 {
			timeout = 150 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		st, err := deps.Store.Get(ctx, sessionID)
		isNew := false
		if err != nil {
			if !errors.Is(err, session.ErrNotFound) {
				slog.ErrorContext(ctx, "chat: load session", "error", err, "session_id", sessionID)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if req.Type != "image" {
				writeError(w, http.StatusNotFound, "session not found or expired")
				return
			}
			st = session.New(sessionID)
			isNew = true
		}

		if req.Type == "image" {
			if st.Phase != session.PhaseCapture {
				writeError(w, http.StatusConflict, "session already initialized")
				return
			}
			if err := analyzePhoto(ctx, deps, st); err != nil {
				if errors.Is(err, errNoPhoto) {
					writeError(w, http.StatusConflict, "upload a photo first")
					return
				}
				if ctx.Err() != nil {
					writeError(w, http.StatusGatewayTimeout, "analysis timeout")
					return
				}
				slog.ErrorContext(ctx, "chat: photo analysis failed", "error", err, "session_id", sessionID)
				writeError(w, http.StatusBadGateway, "photo analysis failed")
				return
			}
		} else if st.Phase == session.PhaseCapture {
			writeError(w, http.StatusConflict, "send the photo turn first")
			return
		}

		applyTurnContext(st, req)
		if input.Kind == "button" {
			input.Text = optionLabel(st, req.OptionID)
		}

		out, err := deps.Runner.RunTurn(ctx, st, input)
		if err != nil {
			if ctx.Err() != nil {
				writeError(w, http.StatusGatewayTimeout, "the stylist took too long, try again")
				return
			}
			slog.ErrorContext(ctx, "chat: agent turn failed", "error", err, "session_id", sessionID)
			writeError(w, http.StatusBadGateway, "the stylist could not answer, try again")
			return
		}

		looksGenerating := false
		if out.IsFinal && deps.Looks != nil && len(st.Looks) > 0 && st.ImageKey != "" {
			st.LookResults = make([]session.LookResult, 0, len(st.Looks))
			for _, l := range st.Looks {
				st.LookResults = append(st.LookResults, session.LookResult{LookID: l.ID, Status: session.LookStatusPending, CreatedAt: time.Now()})
			}
			looksGenerating = true
		}

		if err := persist(ctx, deps.Store, st, isNew, out.IsFinal); err != nil {
			slog.ErrorContext(ctx, "chat: save session", "error", err, "session_id", sessionID)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		if looksGenerating {
			go deps.Looks.GenerateAll(context.WithoutCancel(ctx), sessionID)
		}

		slog.InfoContext(ctx, "chat: turn done",
			"session_id", sessionID, "turn", st.Turn, "phase", st.Phase,
			"steps", out.Steps, "tools", strings.Join(out.ToolsUsed, ","), "final", out.IsFinal)

		resp := buildChatResponse(st, out.Message, out.InputMode, out.Options, out.IsFinal)
		resp.LooksGenerating = looksGenerating
		writeJSON(w, http.StatusOK, resp)
	}
}

// recommendationHandler returns the last recommendation of a session.
func recommendationHandler(store session.Store) http.HandlerFunc {
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
		if st.Recommendation == nil {
			writeError(w, http.StatusNotFound, "no recommendation yet")
			return
		}
		resp := buildChatResponse(st, st.Recommendation.Summary, "none", nil, true)
		resp.LooksGenerating = len(st.Looks) > 0 && !allLooksDone(st)
		writeJSON(w, http.StatusOK, resp)
	}
}

func turnInput(req ChatRequest) (agent.TurnInput, error) {
	switch req.Type {
	case "image":
		return agent.TurnInput{Kind: "system"}, nil
	case "button_response":
		if strings.TrimSpace(req.OptionID) == "" {
			return agent.TurnInput{}, fmt.Errorf("missing field: option_id")
		}
		return agent.TurnInput{Kind: "button", Text: req.OptionID}, nil
	case "voice_response":
		if strings.TrimSpace(req.Transcript) == "" {
			return agent.TurnInput{}, fmt.Errorf("missing field: transcript")
		}
		return agent.TurnInput{Kind: "voice", Text: req.Transcript}, nil
	case "text":
		if strings.TrimSpace(req.Text) == "" {
			return agent.TurnInput{}, fmt.Errorf("missing field: text")
		}
		return agent.TurnInput{Kind: "text", Text: req.Text}, nil
	default:
		return agent.TurnInput{}, fmt.Errorf("type must be image, button_response, voice_response or text")
	}
}

func analyzePhoto(ctx context.Context, deps *ChatDeps, st *session.State) error {
	start := time.Now()
	keys, err := deps.Images.ListKeys(ctx, fmt.Sprintf("sessions/%s/welcome_", st.ID))
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}
	if len(keys) == 0 {
		return errNoPhoto
	}
	sort.Strings(keys)
	key := keys[len(keys)-1]
	data, err := deps.Images.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("download image: %w", err)
	}
	analysis, err := deps.Analyzer.AnalyzeOutfit(ctx, data)
	if err != nil {
		return err
	}
	st.ImageKey = key
	st.ImageURL = deps.Images.URL(key)
	st.Analysis = analysis
	st.Phase = session.PhaseChat
	slog.InfoContext(ctx, "chat: photo analyzed", "session_id", st.ID, "key", key, "items", len(analysis.DetectedItems), "elapsed", time.Since(start))
	return nil
}

func applyTurnContext(st *session.State, req ChatRequest) {
	if req.Location != nil && (req.Location.Lat != 0 || req.Location.Lng != 0) {
		label := strings.TrimSpace(req.Location.Label)
		if label == "" {
			label = fmt.Sprintf("%.4f, %.4f", req.Location.Lat, req.Location.Lng)
		}
		st.Location = &session.Location{Lat: req.Location.Lat, Lng: req.Location.Lng, Label: label, Source: "device"}
	}
	if req.RadiusM > 0 {
		st.Profile.RadiusM = req.RadiusM
	}
	if req.AllowShipping != nil {
		st.Profile.AllowShipping = req.AllowShipping
	}
}

func optionLabel(st *session.State, optionID string) string {
	for _, o := range st.LastAssistantOptions() {
		if o.ID == optionID {
			return o.Label
		}
	}
	return optionID
}

// persist writes the turn back. Fields owned by background jobs (look and
// try-on results) are preserved unless this turn produced new looks.
func persist(ctx context.Context, store session.Store, st *session.State, isNew, newLooks bool) error {
	if isNew {
		return store.Save(ctx, st)
	}
	err := store.Update(ctx, st.ID, func(cur *session.State) error {
		cur.Phase = st.Phase
		cur.Turn = st.Turn
		cur.ImageKey = st.ImageKey
		cur.ImageURL = st.ImageURL
		cur.Analysis = st.Analysis
		cur.Location = st.Location
		cur.Profile = st.Profile
		cur.Messages = st.Messages
		cur.Transcript = st.Transcript
		cur.Products = st.Products
		cur.Stores = st.Stores
		cur.Recommendation = st.Recommendation
		if newLooks {
			cur.Looks = st.Looks
			cur.LookResults = st.LookResults
		}
		return nil
	})
	if errors.Is(err, session.ErrNotFound) {
		return store.Save(ctx, st)
	}
	return err
}

func buildChatResponse(st *session.State, message, inputMode string, options []session.Option, isFinal bool) *ChatResponse {
	resp := &ChatResponse{
		SessionID:     st.ID,
		Phase:         string(st.Phase),
		Turn:          st.Turn,
		Message:       message,
		InputMode:     inputMode,
		Options:       []ChatOption{},
		IsFinal:       isFinal,
		LocationKnown: st.Location != nil,
	}
	for _, o := range options {
		resp.Options = append(resp.Options, ChatOption{ID: o.ID, Label: o.Label})
	}
	if !isFinal || st.Recommendation == nil {
		return resp
	}
	rec := st.Recommendation
	resp.TotalMXN = rec.TotalMXN
	seen := map[string]bool{}
	for _, a := range rec.PriorityActions {
		resp.PriorityActions = append(resp.PriorityActions, PriorityActionResponse{
			ID: a.ID, Title: a.Title, Description: a.Description, Impact: a.Impact, Effort: a.Effort, ProductIDs: a.ProductIDs,
		})
	}
	for _, item := range rec.ShoppingList {
		products := st.ProductsFor(item.ProductIDs)
		resp.ShoppingList = append(resp.ShoppingList, ShoppingItemResponse{
			Slot: item.Slot, Description: item.Description, Why: item.Why, Priority: item.Priority, Products: products,
		})
		for _, p := range products {
			if !seen[p.ID] {
				seen[p.ID] = true
				resp.Products = append(resp.Products, p)
			}
		}
	}
	for _, a := range rec.PriorityActions {
		for _, p := range st.ProductsFor(a.ProductIDs) {
			if !seen[p.ID] {
				seen[p.ID] = true
				resp.Products = append(resp.Products, p)
			}
		}
	}
	// Only stores that carry something on the list, closest first.
	storeSeen := map[string]bool{}
	for _, p := range resp.Products {
		if p.NearbyStore != nil && !storeSeen[p.NearbyStore.PlaceID] {
			for _, s := range st.Stores {
				if s.PlaceID == p.NearbyStore.PlaceID {
					resp.Stores = append(resp.Stores, s)
					storeSeen[s.PlaceID] = true
					break
				}
			}
		}
	}
	sort.Slice(resp.Stores, func(i, j int) bool { return resp.Stores[i].DistanceM < resp.Stores[j].DistanceM })
	return resp
}

func allLooksDone(st *session.State) bool {
	if len(st.LookResults) < len(st.Looks) {
		return false
	}
	for _, lr := range st.LookResults {
		if lr.Status != session.LookStatusReady && lr.Status != session.LookStatusFailed {
			return false
		}
	}
	return true
}
