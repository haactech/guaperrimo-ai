package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"stylerag/internal/agent"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
	"stylerag/internal/tryon"
	"stylerag/internal/ui"
	"stylerag/internal/vision"
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,80}$`)

var errNoPhoto = errors.New("no photo uploaded for this session")

// ChatDeps bundles what the chat handler needs.
type ChatDeps struct {
	Store            session.Store
	Images           storage.ImageStore
	Analyzer         vision.Analyzer
	Runner           *agent.Runner
	Looks            *tryon.LookGenerator // nil = try-on disabled
	Renderer         *tryon.Renderer      // nil = try-on disabled
	AutoLooks        bool                 // render the agent's looks automatically after the recommendation (billed)
	MatrixMaxPerSlot int
	TurnTimeout      time.Duration
}

// apiError is a failure of a chat turn: the HTTP status /chat answers with and
// the message the client sees.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }

func newAPIError(status int, msg string) *apiError { return &apiError{Status: status, Message: msg} }

func chatHandler(deps *ChatDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, req, aerr := parseChatRequest(w, r)
		if aerr != nil {
			writeError(w, aerr.Status, aerr.Message)
			return
		}
		resp, aerr := runChatTurn(r.Context(), deps, sessionID, req, nil)
		if aerr != nil {
			writeError(w, aerr.Status, aerr.Message)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// parseChatRequest validates the session id and decodes the body, the part of
// the validation shared by /chat and /chat/stream.
func parseChatRequest(w http.ResponseWriter, r *http.Request) (string, ChatRequest, *apiError) {
	var req ChatRequest
	sessionID := r.PathValue("id")
	if !sessionIDPattern.MatchString(sessionID) {
		return "", req, newAPIError(http.StatusBadRequest, "invalid session id")
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return "", req, newAPIError(http.StatusBadRequest, "invalid JSON body")
	}
	return sessionID, req, nil
}

// runChatTurn runs one conversation turn and returns the response /chat sends.
// report (may be nil) is told which stage the turn is in.
func runChatTurn(ctx context.Context, deps *ChatDeps, sessionID string, req ChatRequest, report func(stage, detail string)) (*ChatResponse, *apiError) {
	if report == nil {
		report = func(string, string) {}
	}
	input, err := turnInput(req)
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, err.Error())
	}

	timeout := deps.TurnTimeout
	if timeout <= 0 {
		timeout = 150 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = agent.WithProgress(ctx, report)

	st, err := deps.Store.Get(ctx, sessionID)
	isNew := false
	if err != nil {
		if !errors.Is(err, session.ErrNotFound) {
			slog.ErrorContext(ctx, "chat: load session", "error", err, "session_id", sessionID)
			return nil, newAPIError(http.StatusInternalServerError, "internal error")
		}
		if req.Type != "image" {
			return nil, newAPIError(http.StatusNotFound, "session not found or expired")
		}
		st = session.New(sessionID)
		isNew = true
	}

	applyTurnContext(st, req)

	// Cheap request checks come before any work starts (and before any
	// progress is reported), so a rejected turn fails before a stream opens.
	if req.Type == "image" {
		if st.Phase != session.PhaseCapture {
			return nil, newAPIError(http.StatusConflict, "session already initialized")
		}
	} else if st.Phase == session.PhaseCapture {
		return nil, newAPIError(http.StatusConflict, "send the photo turn first")
	}
	if req.Type == "idea_selection" && (st.Phase != session.PhasePreview || st.Direction == nil) {
		return nil, newAPIError(http.StatusConflict, "no style preview is waiting for a selection")
	}
	if input.Kind == "ui" {
		text, err := applyUIAction(st, req)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, err.Error())
		}
		input.Text = text
	}

	// Nearby stores are looked up as soon as the location is known, in
	// parallel with the photo analysis on the first turn, so the agent never
	// has to ask for them and the server can offer them as options.
	var stores []shopping.Store
	storesDone := make(chan struct{})
	if st.Location != nil && len(st.Stores) == 0 {
		report(agent.StageFindingStores, "")
		go func() {
			defer close(storesDone)
			stores = deps.Runner.NearbyStores(ctx, st)
		}()
	} else {
		close(storesDone)
	}

	if req.Type == "image" {
		report(agent.StageAnalyzingPhoto, "")
		if err := analyzePhoto(ctx, deps, st); err != nil {
			<-storesDone
			if errors.Is(err, errNoPhoto) {
				return nil, newAPIError(http.StatusConflict, "upload a photo first")
			}
			if ctx.Err() != nil {
				return nil, newAPIError(http.StatusGatewayTimeout, "analysis timeout")
			}
			slog.ErrorContext(ctx, "chat: photo analysis failed", "error", err, "session_id", sessionID)
			return nil, newAPIError(http.StatusBadGateway, "photo analysis failed")
		}
	}

	<-storesDone
	st.AddStores(stores)
	if input.Kind == "button" {
		input.Text = optionLabel(st, req.OptionID)
	}

	out, err := deps.Runner.RunTurn(ctx, st, input)
	if err != nil {
		if ctx.Err() != nil {
			return nil, newAPIError(http.StatusGatewayTimeout, "the stylist took too long, try again")
		}
		slog.ErrorContext(ctx, "chat: agent turn failed", "error", err, "session_id", sessionID)
		return nil, newAPIError(http.StatusBadGateway, "the stylist could not answer, try again")
	}

	if out.IsFinal {
		st.Matrix = session.BuildMatrix(st, deps.MatrixMaxPerSlot)
	}
	looksGenerating := false
	if out.IsFinal && deps.AutoLooks && deps.Looks != nil && len(st.Looks) > 0 && st.ImageKey != "" {
		st.LookResults = make([]session.LookResult, 0, len(st.Looks))
		for _, l := range st.Looks {
			st.LookResults = append(st.LookResults, session.LookResult{LookID: l.ID, Status: session.LookStatusPending, CreatedAt: time.Now()})
		}
		looksGenerating = true
	}

	if err := persist(ctx, deps.Store, st, isNew, out.IsFinal); err != nil {
		slog.ErrorContext(ctx, "chat: save session", "error", err, "session_id", sessionID)
		return nil, newAPIError(http.StatusInternalServerError, "internal error")
	}

	if looksGenerating {
		go deps.Looks.GenerateAll(context.WithoutCancel(ctx), sessionID)
	}
	// No prewarm: every try-on image is billed (~1 USD on Vertex), so
	// images are generated only when the user asks for one.

	slog.InfoContext(ctx, "chat: turn done",
		"session_id", sessionID, "turn", st.Turn, "phase", st.Phase,
		"steps", out.Steps, "tools", strings.Join(out.ToolsUsed, ","), "final", out.IsFinal)

	resp := buildChatResponse(st, out.Message, out.InputMode, out.Options, out.UI, out.IsFinal)
	resp.LooksGenerating = looksGenerating
	resp.MatrixAvailable = st.Matrix != nil
	return resp, nil
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
		resp := buildChatResponse(st, st.Recommendation.Summary, "none", nil, nil, true)
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
	case "ui_action":
		if strings.TrimSpace(req.ComponentID) == "" {
			return agent.TurnInput{}, fmt.Errorf("missing field: component_id")
		}
		if strings.TrimSpace(req.Value) == "" {
			return agent.TurnInput{}, fmt.Errorf("missing field: value")
		}
		return agent.TurnInput{Kind: "ui", Text: req.Value}, nil // text is completed from the component
	case "idea_selection":
		if len(req.SelectedIdeas) == 0 && strings.TrimSpace(req.Text) == "" {
			return agent.TurnInput{}, fmt.Errorf("idea_selection needs selected_ideas or text")
		}
		return agent.TurnInput{Kind: "selection", Selected: req.SelectedIdeas, Feedback: req.Text}, nil
	default:
		return agent.TurnInput{}, fmt.Errorf("type must be image, button_response, voice_response, text, idea_selection or ui_action")
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

// applyUIAction resolves a slider action against the components of the last
// assistant turn. When the slider is bound to a profile field the value is
// written to the profile right here, so it never depends on the model. It
// returns the text the agent sees after "[ui] ".
func applyUIAction(st *session.State, req ChatRequest) (string, error) {
	c := ui.FindByID(st.LastAssistantUI(), strings.TrimSpace(req.ComponentID))
	if c == nil || c.Type != ui.TypeSlider {
		return "", fmt.Errorf("unknown component_id: %s", req.ComponentID)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(req.Value), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return "", fmt.Errorf("value must be a number")
	}
	v = math.Min(math.Max(v, c.Min), c.Max)
	switch c.ProfileField {
	case ui.FieldBudgetMXN:
		st.Profile.BudgetMXN = v
	case ui.FieldRadiusM:
		st.Profile.RadiusM = int(math.Round(v))
	}
	label := c.Label
	if label == "" {
		label = c.ID
	}
	text := label + ": " + strconv.FormatFloat(v, 'f', -1, 64)
	if c.Unit != "" {
		text += " " + c.Unit
	}
	return text, nil
}

func optionLabel(st *session.State, optionID string) string {
	for _, o := range st.LastAssistantOptions() {
		if o.ID == optionID {
			return o.Label
		}
	}
	// A chip the model did not repeat in options still resolves.
	if c := ui.FindChips(st.LastAssistantUI()); c != nil {
		for _, o := range c.Options {
			if o.ID == optionID {
				return o.Label
			}
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
		cur.Pending = st.Pending
		cur.LocationAsks = st.LocationAsks
		cur.StoreAsked = st.StoreAsked
		cur.Messages = st.Messages
		cur.Transcript = st.Transcript
		cur.Products = st.Products
		cur.Stores = st.Stores
		cur.Direction = st.Direction
		cur.Recommendation = st.Recommendation
		if newLooks {
			cur.Looks = st.Looks
			cur.LookResults = st.LookResults
			cur.Matrix = st.Matrix
		}
		return nil
	})
	if errors.Is(err, session.ErrNotFound) {
		return store.Save(ctx, st)
	}
	return err
}

func buildChatResponse(st *session.State, message, inputMode string, options []session.Option, components []ui.Component, isFinal bool) *ChatResponse {
	resp := &ChatResponse{
		SessionID:     st.ID,
		Phase:         string(st.Phase),
		Turn:          st.Turn,
		Message:       message,
		InputMode:     inputMode,
		UIVersion:     ui.Version,
		Speech:        message,
		UI:            components,
		Options:       []ChatOption{},
		IsFinal:       isFinal,
		LocationKnown: st.Location != nil,
	}
	for _, o := range options {
		resp.Options = append(resp.Options, ChatOption{ID: o.ID, Label: o.Label})
	}
	if inputMode == "preview" {
		resp.Direction = st.Direction
	}
	if !isFinal || st.Recommendation == nil {
		return resp
	}
	rec := st.Recommendation
	resp.UI = composeFinalUI(st) // by code, whatever the model attached
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
