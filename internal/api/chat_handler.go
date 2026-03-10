package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

// ChatDeps bundles dependencies for the chat handler.
type ChatDeps struct {
	Store      session.SessionStore
	ImageStore storage.ImageStore
	Analyzer   vision.Analyzer
	Discovery  *vision.DiscoveryManager
	Diagnosis  *vision.DiagnosisGenerator
	Advisor    *vision.StyleAdvisor
}

func chatHandler(deps *ChatDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if sessionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
			return
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}

		// Validate request type and required fields
		switch req.Type {
		case "image":
			// image_url is optional — we fetch from R2
		case "button_response":
			if req.OptionID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing field: option_id"})
				return
			}
		case "voice_response":
			if req.Transcript == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing field: transcript"})
				return
			}
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be image, button_response, or voice_response"})
			return
		}

		ctx := r.Context()

		// Load or create session
		state, err := deps.Store.Get(sessionID)
		if err != nil {
			if req.Type != "image" {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found or expired"})
				return
			}
			state = &session.SessionState{
				ID:           sessionID,
				Phase:        session.PhaseCapture,
				Turn:         0,
				Responses:    []session.UserResponse{},
				CoveredFacts: map[string]string{},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}
		}

		// Phase gates
		if req.Type == "image" && state.Phase != session.PhaseCapture {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "session already initialized"})
			return
		}
		if state.Phase == session.PhaseRecommendation {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "session already completed"})
			return
		}

		var resp *ChatResponse

		switch state.Phase {
		case session.PhaseCapture:
			resp, err = handleCapture(ctx, deps, state, sessionID)
		case session.PhaseDiscovery:
			resp, err = handleDiscovery(ctx, deps, state, sessionID, &req)
		default:
			writeJSON(w, http.StatusConflict, map[string]string{"error": "unexpected session phase"})
			return
		}

		if err != nil {
			if ctx.Err() != nil {
				writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "analysis timeout"})
				return
			}
			slog.Error("chat: processing failed", "error", err, "session_id", sessionID, "phase", state.Phase)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error", "detail": err.Error()})
			return
		}

		if err := deps.Store.Save(state); err != nil {
			slog.Error("chat: failed to save session", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleCapture(ctx context.Context, deps *ChatDeps, state *session.SessionState, sessionID string) (*ChatResponse, error) {
	start := time.Now()
	prefix := fmt.Sprintf("sessions/%s/", sessionID)

	keys, err := deps.ImageStore.ListKeys(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no image found for session %s", sessionID)
	}

	sort.Strings(keys)
	latestKey := keys[len(keys)-1]

	imageData, err := deps.ImageStore.Download(ctx, latestKey)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	slog.Info("chat: image downloaded", "key", latestKey, "size_bytes", len(imageData), "elapsed", time.Since(start))

	analysis, err := deps.Analyzer.AnalyzeOutfit(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("outfit analysis failed: %w", err)
	}
	slog.Info("chat: outfit analysis done", "elapsed", time.Since(start))

	state.OutfitAnalysis = analysis
	state.Turn = 1
	state.Phase = session.PhaseDiscovery

	discovery, err := deps.Discovery.NextQuestion(ctx, analysis, state.Responses, state.AssistantMessages, state.Turn, state.CoveredFacts)
	if err != nil {
		return nil, fmt.Errorf("first discovery question failed: %w", err)
	}

	state.AssistantMessages = append(state.AssistantMessages, discovery.Message)

	options := make([]ChatOption, len(discovery.Options))
	for i, opt := range discovery.Options {
		options[i] = ChatOption{ID: opt.ID, Label: opt.Label}
	}

	slog.Info("chat: capture complete", "session_id", sessionID, "total_elapsed", time.Since(start))

	return &ChatResponse{
		SessionID: sessionID,
		Phase:     string(session.PhaseCapture),
		Turn:      state.Turn,
		Message:   discovery.Message,
		InputMode: discovery.InputMode,
		Options:   options,
		IsFinal:   false,
	}, nil
}

func handleDiscovery(ctx context.Context, deps *ChatDeps, state *session.SessionState, sessionID string, req *ChatRequest) (*ChatResponse, error) {
	// Record user response
	userResp := session.UserResponse{
		Timestamp: time.Now(),
	}
	switch req.Type {
	case "button_response":
		userResp.InputMode = "button"
		userResp.Value = req.OptionID
	case "voice_response":
		userResp.InputMode = "voice"
		userResp.Value = req.Transcript
	}

	state.Responses = append(state.Responses, userResp)
	state.Turn++

	analysis, ok := state.OutfitAnalysis.(*vision.OutfitAnalysis)
	if !ok {
		return nil, fmt.Errorf("invalid outfit analysis in session state")
	}

	// Ask the LLM for the next question + extract facts from the user's latest answer
	discovery, err := deps.Discovery.NextQuestion(ctx, analysis, state.Responses, state.AssistantMessages, state.Turn, state.CoveredFacts)
	if err != nil {
		return nil, fmt.Errorf("discovery question failed: %w", err)
	}

	state.AssistantMessages = append(state.AssistantMessages, discovery.Message)

	// Accumulate extracted facts into session state
	for cat, fact := range discovery.ExtractedFacts {
		if fact != "" {
			state.CoveredFacts[cat] = fact
		}
	}

	slog.Info("chat: discovery turn",
		"session_id", sessionID,
		"turn", state.Turn,
		"covered_facts", state.CoveredFacts,
		"new_facts", discovery.ExtractedFacts,
	)

	// Deterministic exit: backend decides when we have enough info
	shouldAdvance := state.ReadyForDiagnosis() || state.Turn >= session.MaxDiscoveryTurns
	if shouldAdvance {
		return handleDiagnosisAndRecommendation(ctx, deps, state, sessionID, analysis)
	}

	options := make([]ChatOption, len(discovery.Options))
	for i, opt := range discovery.Options {
		options[i] = ChatOption{ID: opt.ID, Label: opt.Label}
	}

	return &ChatResponse{
		SessionID: sessionID,
		Phase:     string(session.PhaseDiscovery),
		Turn:      state.Turn,
		Message:   discovery.Message,
		InputMode: discovery.InputMode,
		Options:   options,
		IsFinal:   false,
	}, nil
}

func handleDiagnosisAndRecommendation(ctx context.Context, deps *ChatDeps, state *session.SessionState, sessionID string, analysis *vision.OutfitAnalysis) (*ChatResponse, error) {
	start := time.Now()

	// Phase 3: Diagnosis
	state.Phase = session.PhaseDiagnosis
	diagnosis, err := deps.Diagnosis.Generate(ctx, analysis, state.Responses)
	if err != nil {
		return nil, fmt.Errorf("diagnosis failed: %w", err)
	}
	state.Diagnosis = diagnosis
	slog.Info("chat: diagnosis done", "session_id", sessionID, "elapsed", time.Since(start))

	// Phase 4: Recommendation
	state.Phase = session.PhaseRecommendation
	advice, err := deps.Advisor.GenerateRecommendation(ctx, diagnosis)
	if err != nil {
		return nil, fmt.Errorf("recommendation failed: %w", err)
	}
	slog.Info("chat: recommendation done", "session_id", sessionID, "total_elapsed", time.Since(start))

	actions := make([]PriorityActionResponse, len(advice.PriorityActions))
	for i, a := range advice.PriorityActions {
		actions[i] = PriorityActionResponse{
			ID:          a.ID,
			Title:       a.Title,
			Description: a.Description,
			Impact:      a.Impact,
			Effort:      a.Effort,
		}
	}

	return &ChatResponse{
		SessionID:       sessionID,
		Phase:           string(session.PhaseRecommendation),
		Turn:            state.Turn,
		Message:         advice.SpokenSummary,
		InputMode:       "none",
		Options:         nil,
		IsFinal:         true,
		PriorityActions: actions,
	}, nil
}
