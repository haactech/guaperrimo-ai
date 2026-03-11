package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"stylerag/internal/llm"
	"stylerag/internal/session"
)

// DiscoveryResult is the parsed LLM response for a discovery turn.
type DiscoveryResult struct {
	Message            string           `json:"message"`
	InputMode          string           `json:"input_mode"`
	Options            []DiscOption     `json:"options"`
	AdvanceToDiagnosis bool             `json:"advance_to_diagnosis"`
	UpdatedFactMap     *session.FactMap `json:"updated_fact_map"`
	Reasoning          string           `json:"reasoning"`
}

type DiscOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// DiscoveryManager drives the discovery conversation phase.
type DiscoveryManager struct {
	router *llm.Router
}

func NewDiscoveryManager(router *llm.Router) *DiscoveryManager {
	return &DiscoveryManager{router: router}
}

func (d *DiscoveryManager) NextQuestion(ctx context.Context, analysis *OutfitAnalysis, responses []session.UserResponse, assistantMessages []string, factMap *session.FactMap, insights *ImageInsights, pendingCompensations []CompensationArea) (*DiscoveryResult, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("discovery: marshal analysis: %w", err)
	}

	factMapJSON, err := json.Marshal(factMap)
	if err != nil {
		return nil, fmt.Errorf("discovery: marshal fact_map: %w", err)
	}

	history := buildConversationHistory(assistantMessages, responses)

	insightsText := "(no hay insights de imagen)"
	if insights != nil {
		insightsText = FormatInsightsForPrompt(insights)
	}

	compensationsText := FormatCompensationsForPrompt(pendingCompensations)

	prompt := fmt.Sprintf(discoveryPrompt, string(analysisJSON), string(factMapJSON), string(factMapJSON), history, insightsText, compensationsText)

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	resp, err := d.router.Complete(ctx, llm.TurnTypeDiscoveryQuestion, req)
	if err != nil {
		return nil, fmt.Errorf("discovery: LLM call failed: %w", err)
	}

	cleaned := cleanJSON(resp.Content)

	// First pass: unmarshal into a raw structure so we can normalize the fact_map
	var raw struct {
		Message            string           `json:"message"`
		InputMode          string           `json:"input_mode"`
		Options            []DiscOption     `json:"options"`
		AdvanceToDiagnosis bool             `json:"advance_to_diagnosis"`
		UpdatedFactMap     json.RawMessage  `json:"updated_fact_map"`
		Reasoning          string           `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("discovery: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	result := &DiscoveryResult{
		Message:            raw.Message,
		InputMode:          raw.InputMode,
		Options:            raw.Options,
		AdvanceToDiagnosis: raw.AdvanceToDiagnosis,
		Reasoning:          raw.Reasoning,
	}

	// Normalize and parse the fact_map separately
	if len(raw.UpdatedFactMap) > 0 && string(raw.UpdatedFactMap) != "null" {
		normalized, err := normalizeFactMapJSON(raw.UpdatedFactMap)
		if err != nil {
			slog.WarnContext(ctx, "discovery: failed to normalize fact_map, using raw", "error", err)
			normalized = raw.UpdatedFactMap
		}
		var fm session.FactMap
		if err := json.Unmarshal(normalized, &fm); err != nil {
			return nil, fmt.Errorf("discovery: failed to parse fact_map: %w\nraw: %s", err, string(raw.UpdatedFactMap))
		}
		result.UpdatedFactMap = &fm
	}

	return result, nil
}

// normalizeFactMapJSON converts fields that should be arrays but the LLM
// returned as bare strings (e.g. constraints: "no formal" → ["no formal"]).
func normalizeFactMapJSON(raw json.RawMessage) (json.RawMessage, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, err
	}

	arrayFields := []string{"constraints", "pain_points"}
	for _, field := range arrayFields {
		val, exists := m[field]
		if !exists || val == nil {
			continue
		}
		if s, ok := val.(string); ok {
			if s == "" {
				m[field] = []string{}
			} else {
				m[field] = []string{s}
			}
		}
	}

	return json.Marshal(m)
}

// buildConversationHistory interleaves assistant messages and user responses
// into a readable chat transcript.
func buildConversationHistory(assistantMessages []string, responses []session.UserResponse) string {
	if len(assistantMessages) == 0 && len(responses) == 0 {
		return "(primera interacción, no hay historial)"
	}

	var b strings.Builder
	maxTurns := len(assistantMessages)
	if len(responses) > maxTurns {
		maxTurns = len(responses)
	}

	for i := 0; i < maxTurns; i++ {
		if i < len(assistantMessages) {
			fmt.Fprintf(&b, "Tú dijiste: %q\n", assistantMessages[i])
		}
		if i < len(responses) {
			r := responses[i]
			if r.InputMode == "button" {
				fmt.Fprintf(&b, "Usuario eligió botón: %s\n", r.Value)
			} else {
				fmt.Fprintf(&b, "Usuario dijo: %q\n", r.Value)
			}
		}
	}

	return b.String()
}
