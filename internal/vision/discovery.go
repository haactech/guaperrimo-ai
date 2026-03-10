package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"stylerag/internal/llm"
	"stylerag/internal/session"
)

// DiscoveryResult is the parsed LLM response for a discovery turn.
type DiscoveryResult struct {
	Message             string       `json:"message"`
	InputMode           string       `json:"input_mode"`
	Options             []DiscOption `json:"options"`
	AdvanceToDiagnosis  bool         `json:"advance_to_diagnosis"`
	CoveredCategories   []string     `json:"covered_categories"`
	Reasoning           string       `json:"reasoning"`
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

func (d *DiscoveryManager) NextQuestion(ctx context.Context, analysis *OutfitAnalysis, responses []session.UserResponse, assistantMessages []string, turn int, coveredCategories []string) (*DiscoveryResult, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("discovery: marshal analysis: %w", err)
	}

	history := buildConversationHistory(assistantMessages, responses)

	minReached := "false"
	if turn >= 3 {
		minReached = "true"
	}

	covered := "ninguna"
	if len(coveredCategories) > 0 {
		covered = strings.Join(coveredCategories, ", ")
	}

	prompt := fmt.Sprintf(discoveryPrompt, string(analysisJSON), history, turn, minReached, covered)

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

	var result DiscoveryResult
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &result); err != nil {
		return nil, fmt.Errorf("discovery: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	return &result, nil
}

// buildConversationHistory interleaves assistant messages and user responses
// into a readable chat transcript so the LLM can see the full flow and vary its tone.
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
