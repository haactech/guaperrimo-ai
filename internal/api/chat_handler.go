package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"stylerag/internal/rag"
	"stylerag/internal/session"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

var chatTracer = otel.Tracer("stylerag/chat")

// ChatDeps bundles dependencies for the chat handler.
type ChatDeps struct {
	Store      session.SessionStore
	ImageStore storage.ImageStore
	Analyzer   vision.Analyzer
	Discovery  *vision.DiscoveryManager
	Diagnosis  *vision.DiagnosisGenerator
	Advisor    *vision.StyleAdvisor
	RAGEngine  rag.Engine // nil = RAG disabled
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
				ID:        sessionID,
				Phase:     session.PhaseCapture,
				Turn:      0,
				Responses: []session.UserResponse{},
				FactMap:   &session.FactMap{},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
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
			slog.ErrorContext(ctx, "chat: processing failed", "error", err, "session_id", sessionID, "phase", state.Phase)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error", "detail": err.Error()})
			return
		}

		if err := deps.Store.Save(state); err != nil {
			slog.ErrorContext(ctx, "chat: failed to save session", "error", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleCapture(ctx context.Context, deps *ChatDeps, state *session.SessionState, sessionID string) (*ChatResponse, error) {
	ctx, span := chatTracer.Start(ctx, "chat.capture", trace.WithAttributes(attribute.String("session.id", sessionID)))
	defer span.End()

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
	slog.InfoContext(ctx, "chat: image downloaded", "key", latestKey, "size_bytes", len(imageData), "elapsed", time.Since(start))

	analysis, err := deps.Analyzer.AnalyzeOutfit(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("outfit analysis failed: %w", err)
	}
	slog.InfoContext(ctx, "chat: outfit analysis done",
		"session_id", sessionID,
		"items_detected", len(analysis.DetectedItems),
		"styles", analysis.DetectedStyles,
		"colors", analysis.DetectedColors,
		"overall_fit", analysis.OverallFit,
		"elapsed", time.Since(start),
	)
	slog.InfoContext(ctx, "chat: outfit observations",
		"session_id", sessionID,
		"observations", analysis.Observations,
	)

	// Log whether the LLM produced the new style theory fields
	hasColor := analysis.ColorAnalysis != nil
	hasSilhouette := analysis.SilhouetteAnalysis != nil
	hasArchetype := analysis.ArchetypeAnalysis != nil
	slog.InfoContext(ctx, "chat: style theory fields",
		"session_id", sessionID,
		"has_color_analysis", hasColor,
		"has_silhouette_analysis", hasSilhouette,
		"has_archetype_analysis", hasArchetype,
	)
	if hasColor {
		slog.InfoContext(ctx, "chat: color analysis",
			"session_id", sessionID,
			"season", analysis.ColorAnalysis.EstimatedSeason,
			"confidence", analysis.ColorAnalysis.Confidence,
			"harmony_score", analysis.ColorAnalysis.ColorHarmonyScore,
			"harmonious_pieces", analysis.ColorAnalysis.HarmoniousPieces,
			"conflicting_pieces", analysis.ColorAnalysis.ConflictingPieces,
		)
	}
	if hasSilhouette {
		slog.InfoContext(ctx, "chat: silhouette analysis",
			"session_id", sessionID,
			"kibbe_family", analysis.SilhouetteAnalysis.EstimatedKibbeFamily,
			"confidence", analysis.SilhouetteAnalysis.Confidence,
			"fit_score", analysis.SilhouetteAnalysis.FitScore,
			"proportion_score", analysis.SilhouetteAnalysis.ProportionScore,
			"line_harmony_score", analysis.SilhouetteAnalysis.LineHarmonyScore,
		)
	}
	if hasArchetype {
		slog.InfoContext(ctx, "chat: archetype analysis",
			"session_id", sessionID,
			"current", analysis.ArchetypeAnalysis.CurrentArchetype,
			"secondary", analysis.ArchetypeAnalysis.SecondaryArchetype,
		)
	}

	state.OutfitAnalysis = analysis
	state.StyleProfile = &session.UserStyleProfile{}
	if hasColor {
		state.StyleProfile.ColorSeason = analysis.ColorAnalysis.EstimatedSeason
		state.StyleProfile.ColorSeasonConf = analysis.ColorAnalysis.Confidence
	}
	if hasSilhouette {
		state.StyleProfile.KibbeFamily = analysis.SilhouetteAnalysis.EstimatedKibbeFamily
		state.StyleProfile.KibbeFamilyConf = analysis.SilhouetteAnalysis.Confidence
	}
	if hasArchetype {
		state.StyleProfile.CurrentArchetype = analysis.ArchetypeAnalysis.CurrentArchetype
	}

	// Generate image insights and pre-populate fact_map
	insights := vision.GenerateInsights(analysis)
	state.ImageInsights = insights
	prepopulateFactMap(state.FactMap, insights)

	if analysis.ImageQuality != nil {
		slog.InfoContext(ctx, "chat: image quality",
			"session_id", sessionID,
			"overall", analysis.ImageQuality.Overall,
			"lighting", analysis.ImageQuality.Lighting,
			"body_coverage", analysis.ImageQuality.BodyCoverage,
			"focus", analysis.ImageQuality.Focus,
			"blind_spots", analysis.ImageQuality.BlindSpots,
			"compensation_needed", len(insights.CompensationNeeded),
		)
	}
	slog.InfoContext(ctx, "chat: image insights",
		"session_id", sessionID,
		"issues", len(insights.DetectedIssues),
		"strengths", len(insights.DetectedStrengths),
		"high_severity", insights.HighSeverityCount(),
		"color_needs_work", insights.InferredFacts.ColorNeedsWork,
		"fit_needs_work", insights.InferredFacts.FitNeedsWork,
		"too_informal_for", insights.InferredFacts.TooInformalFor,
	)

	state.Turn = 1
	state.Phase = session.PhaseDiscovery

	pendingCompensations := getPendingCompensations(state, insights)
	discovery, err := deps.Discovery.NextQuestion(ctx, analysis, state.Responses, state.AssistantMessages, state.FactMap, insights, pendingCompensations)
	if err != nil {
		return nil, fmt.Errorf("first discovery question failed: %w", err)
	}

	if discovery.UpdatedFactMap != nil {
		state.FactMap = discovery.UpdatedFactMap
	}
	state.AssistantMessages = append(state.AssistantMessages, discovery.Message)

	options := make([]ChatOption, len(discovery.Options))
	for i, opt := range discovery.Options {
		options[i] = ChatOption{ID: opt.ID, Label: opt.Label}
	}

	optionLabels := make([]string, len(options))
	for i, o := range options {
		optionLabels[i] = o.Label
	}
	slog.InfoContext(ctx, "chat: capture complete",
		"session_id", sessionID,
		"bot_message", discovery.Message,
		"input_mode", discovery.InputMode,
		"options", optionLabels,
		"total_elapsed", time.Since(start),
	)

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
	ctx, span := chatTracer.Start(ctx, "chat.discovery", trace.WithAttributes(attribute.String("session.id", sessionID)))
	defer span.End()

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

	slog.InfoContext(ctx, "chat: user input",
		"session_id", sessionID,
		"turn", state.Turn+1,
		"input_mode", userResp.InputMode,
		"value", userResp.Value,
	)

	state.Responses = append(state.Responses, userResp)
	state.Turn++

	// Update conversation metrics
	wordCount := len(strings.Fields(userResp.Value))
	state.Metrics.LastResponseWordCount = wordCount
	if wordCount < 5 {
		state.Metrics.ConsecutiveShortResponses++
	} else {
		state.Metrics.ConsecutiveShortResponses = 0
	}
	if vision.ContainsExitSignal(userResp.Value) {
		state.Metrics.ExitSignalCount++
	}

	analysis, ok := state.OutfitAnalysis.(*vision.OutfitAnalysis)
	if !ok {
		return nil, fmt.Errorf("invalid outfit analysis in session state")
	}

	// ALWAYS extract facts from user input before any advance decision
	vision.ExtractFactsFromInput(state.FactMap, userResp.Value)

	// Pre-LLM guardrail: check if Go should force advance
	forceResult := vision.ShouldForceAdvance(state, userResp.Value)
	if forceResult.ShouldForce {
		slog.InfoContext(ctx, "chat: Go forcing advance after fact extraction",
			"session_id", sessionID,
			"turn", state.Turn,
			"reason", forceResult.Reason,
			"exit_signals", state.Metrics.ExitSignalCount,
			"consecutive_short", state.Metrics.ConsecutiveShortResponses,
			"facts_covered", session.CountCoveredFacts(state.FactMap),
		)
		transitionMsg := vision.PickTransitionMessage()
		state.AssistantMessages = append(state.AssistantMessages, transitionMsg)

		return handleDiagnosisAndRecommendation(ctx, deps, state, sessionID, analysis)
	}

	// Recover insights for discovery prompt
	var insights *vision.ImageInsights
	if ins, ok := state.ImageInsights.(*vision.ImageInsights); ok {
		insights = ins
	}
	pendingCompensations := getPendingCompensations(state, insights)

	// Ask the LLM for the next question + extract facts from the user's latest answer
	discovery, err := deps.Discovery.NextQuestion(ctx, analysis, state.Responses, state.AssistantMessages, state.FactMap, insights, pendingCompensations)
	if err != nil {
		return nil, fmt.Errorf("discovery question failed: %w", err)
	}

	// FactMap: wholesale replacement (LLM returns the complete map)
	if discovery.UpdatedFactMap != nil {
		state.FactMap = discovery.UpdatedFactMap
	}

	// Post-LLM guardrail: detect repeated questions
	forcedByRepetition := false
	if vision.IsSimilarQuestion(discovery.Message, state.LastBotMessage) {
		slog.WarnContext(ctx, "chat: repeated question detected, forcing advance",
			"session_id", sessionID,
			"turn", state.Turn,
			"current_msg", discovery.Message,
			"previous_msg", state.LastBotMessage,
		)
		state.Metrics.RepeatedQuestionDetected = true
		forcedByRepetition = true
	}

	state.AssistantMessages = append(state.AssistantMessages, discovery.Message)
	state.LastBotMessage = discovery.Message

	slog.InfoContext(ctx, "chat: discovery state",
		"session_id", sessionID,
		"turn", state.Turn,
		"bot_message", discovery.Message,
		"input_mode", discovery.InputMode,
		"reasoning", discovery.Reasoning,
		"facts_covered", session.CountCoveredFacts(state.FactMap),
		"missing_critical", session.ListMissingCriticalFacts(state.FactMap),
		"exit_signal_count", state.Metrics.ExitSignalCount,
		"consecutive_short", state.Metrics.ConsecutiveShortResponses,
		"last_word_count", state.Metrics.LastResponseWordCount,
		"llm_wants_advance", discovery.AdvanceToDiagnosis,
		"repeated_question", forcedByRepetition,
	)

	// Advance decision
	shouldAdvance := discovery.AdvanceToDiagnosis || forcedByRepetition

	// Guardrail: if LLM says advance but also sends options → block
	if shouldAdvance && len(discovery.Options) > 0 && !forcedByRepetition {
		slog.WarnContext(ctx, "chat: blocking premature advance — LLM sent advance=true with options",
			"session_id", sessionID,
			"turn", state.Turn,
		)
		shouldAdvance = false
	}

	// Safety net: force advance at turn cap
	forcedByCap := false
	if state.Turn >= session.MaxAgenticTurns && !shouldAdvance {
		slog.WarnContext(ctx, "chat: forcing advance at turn cap",
			"session_id", sessionID,
			"turn", state.Turn,
		)
		shouldAdvance = true
		forcedByCap = true
	}

	slog.InfoContext(ctx, "chat: advance decision",
		"session_id", sessionID,
		"turn", state.Turn,
		"llm_wants_advance", discovery.AdvanceToDiagnosis,
		"forced_by_repetition", forcedByRepetition,
		"forced_by_cap", forcedByCap,
		"advancing", shouldAdvance,
	)

	if shouldAdvance {
		return handleDiagnosisAndRecommendation(ctx, deps, state, sessionID, analysis)
	}

	options := make([]ChatOption, len(discovery.Options))
	for i, opt := range discovery.Options {
		options[i] = ChatOption{ID: opt.ID, Label: opt.Label}
	}
	discOptionLabels := make([]string, len(options))
	for i, o := range options {
		discOptionLabels[i] = o.Label
	}
	slog.InfoContext(ctx, "chat: discovery options presented",
		"session_id", sessionID,
		"turn", state.Turn,
		"options", discOptionLabels,
	)

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
	ctx, span := chatTracer.Start(ctx, "chat.diagnosis_and_recommendation", trace.WithAttributes(attribute.String("session.id", sessionID)))
	defer span.End()

	start := time.Now()

	// Bridge FactMap → Profile
	profile := state.StyleProfile
	if profile == nil {
		profile = &session.UserStyleProfile{}
	}
	fm := state.FactMap
	if fm != nil {
		if fm.Occasion != nil {
			profile.Occasion = *fm.Occasion
		}
		if fm.Intention != nil {
			profile.DesiredProjection = *fm.Intention
		}
		if fm.Approach != nil {
			profile.Approach = *fm.Approach
		}
		if len(fm.PainPoints) > 0 {
			profile.PainPoints = fm.PainPoints
		}
		if fm.AspirationalRef != nil {
			profile.AspirationalRef = *fm.AspirationalRef
		}
		if len(fm.Constraints) > 0 {
			profile.Constraints = fm.Constraints
		}
	}

	// Log profile state going into diagnosis
	slog.InfoContext(ctx, "chat: profile entering diagnosis",
		"session_id", sessionID,
		"color_season", profile.ColorSeason,
		"color_conf", profile.ColorSeasonConf,
		"kibbe", profile.KibbeFamily,
		"kibbe_conf", profile.KibbeFamilyConf,
		"archetype", profile.CurrentArchetype,
		"occasion", profile.Occasion,
		"desired_projection", profile.DesiredProjection,
		"approach", profile.Approach,
	)

	// Phase 3: Diagnosis
	state.Phase = session.PhaseDiagnosis
	diagnosis, err := deps.Diagnosis.Generate(ctx, analysis, state.Responses, profile)
	if err != nil {
		return nil, fmt.Errorf("diagnosis failed: %w", err)
	}
	state.Diagnosis = diagnosis
	state.StyleProfile = &diagnosis.Profile

	// Log diagnosis scoring results
	p := &diagnosis.Profile
	slog.InfoContext(ctx, "chat: diagnosis profile",
		"session_id", sessionID,
		"strengths", p.Strengths,
		"gaps", p.Gaps,
		"style_distance", p.StyleDistance,
		"desired_archetype", p.DesiredArchetype,
	)
	slog.InfoContext(ctx, "chat: diagnosis scores",
		"session_id", sessionID,
		"color_harmony", p.Scores.ColorHarmony,
		"fit", p.Scores.Fit,
		"proportion", p.Scores.Proportion,
		"line_harmony", p.Scores.LineHarmony,
		"style_coherence", p.Scores.StyleCoherence,
		"occasion_match", p.Scores.OccasionMatch,
		"overall_score", fmt.Sprintf("%.2f", p.OverallScore),
		"overall_grade", p.OverallGrade,
		"gap_count", len(p.GapAnalysis),
	)
	for i, g := range p.GapAnalysis {
		slog.InfoContext(ctx, "chat: gap item",
			"session_id", sessionID,
			"index", i,
			"dimension", g.Dimension,
			"current", g.Current,
			"target", g.Target,
			"gap", g.Gap,
			"priority", g.Priority,
			"actionable", g.Actionable,
		)
	}
	slog.InfoContext(ctx, "chat: diagnosis done", "session_id", sessionID, "elapsed", time.Since(start))

	// Phase 3.5: RAG Search — find real products for top gaps
	var gapProducts []vision.GapProductContext
	if deps.RAGEngine != nil && len(diagnosis.Profile.GapAnalysis) > 0 {
		topGaps := selectTopGaps(diagnosis.Profile.GapAnalysis, 3)
		queries := rag.BuildSearchQueries(topGaps, state.StyleProfile, 3)

		ragStart := time.Now()
		g, gctx := errgroup.WithContext(ctx)
		results := make([][]rag.Product, len(queries))
		for i, q := range queries {
			g.Go(func() error {
				prods, err := deps.RAGEngine.Search(gctx, q)
				if err != nil {
					slog.WarnContext(gctx, "rag: search failed", "gap", q.Text, "error", err)
					return nil // non-fatal
				}
				results[i] = prods
				return nil
			})
		}
		_ = g.Wait()

		for i, gap := range topGaps {
			if i < len(results) && len(results[i]) > 0 {
				gapProducts = append(gapProducts, vision.GapProductContext{
					Gap:      gap.Actionable,
					Products: results[i],
				})
			}
		}
		slog.InfoContext(ctx, "chat: RAG search done",
			"session_id", sessionID,
			"queries", len(queries),
			"gaps_with_products", len(gapProducts),
			"elapsed", time.Since(ragStart),
		)
	}

	// Phase 4: Recommendation (with products if available)
	state.Phase = session.PhaseRecommendation
	advice, err := deps.Advisor.GenerateRecommendationWithProducts(ctx, state.StyleProfile, gapProducts)
	if err != nil {
		return nil, fmt.Errorf("recommendation failed: %w", err)
	}

	// Log recommendation output
	slog.InfoContext(ctx, "chat: recommendation done",
		"session_id", sessionID,
		"spoken_summary", advice.SpokenSummary,
		"action_count", len(advice.PriorityActions),
		"total_elapsed", time.Since(start),
	)
	for i, a := range advice.PriorityActions {
		slog.InfoContext(ctx, "chat: priority action",
			"session_id", sessionID,
			"index", i,
			"id", a.ID,
			"title", a.Title,
			"impact", a.Impact,
			"effort", a.Effort,
			"description", a.Description,
			"product_ids", a.ProductIDs,
		)
	}

	allProducts := collectUniqueProducts(gapProducts)

	actions := make([]PriorityActionResponse, len(advice.PriorityActions))
	for i, a := range advice.PriorityActions {
		actions[i] = PriorityActionResponse{
			ID:          a.ID,
			Title:       a.Title,
			Description: a.Description,
			Impact:      a.Impact,
			Effort:      a.Effort,
			ProductIDs:  a.ProductIDs,
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
		Products:        allProducts,
	}, nil
}

// selectTopGaps returns the top N gaps sorted by priority (highest first).
func selectTopGaps(gaps []session.GapItem, n int) []session.GapItem {
	sorted := make([]session.GapItem, len(gaps))
	copy(sorted, gaps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// prepopulateFactMap fills the fact_map with observations derived from image analysis.
func prepopulateFactMap(fm *session.FactMap, insights *vision.ImageInsights) {
	if fm == nil || insights == nil {
		return
	}
	for _, issue := range insights.DetectedIssues {
		if issue.Severity == "high" {
			fm.PainPoints = append(fm.PainPoints,
				fmt.Sprintf("[detectado en foto] %s", issue.Observation))
		}
	}
	// Note: TooInformalFor is NOT injected into the fact_map.
	// It's available in the prompt as context, but the LLM should
	// only use it if the user confirms the occasion.
}

// getPendingCompensations returns compensation areas not yet covered.
func getPendingCompensations(state *session.SessionState, insights *vision.ImageInsights) []vision.CompensationArea {
	if insights == nil || len(insights.CompensationNeeded) == 0 {
		return nil
	}
	var pending []vision.CompensationArea
	for _, comp := range insights.CompensationNeeded {
		if !state.IsCompensationCovered(comp.BlindSpot) {
			pending = append(pending, comp)
		}
	}
	return pending
}

// collectUniqueProducts deduplicates products across all gap results.
func collectUniqueProducts(gapProducts []vision.GapProductContext) []rag.Product {
	seen := make(map[string]bool)
	var unique []rag.Product
	for _, gp := range gapProducts {
		for _, p := range gp.Products {
			if !seen[p.ID] {
				seen[p.ID] = true
				unique = append(unique, p)
			}
		}
	}
	return unique
}
