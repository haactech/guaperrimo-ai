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
	Message        string            `json:"message"`
	InputMode      string            `json:"input_mode"`
	Options        []DiscOption      `json:"options"`
	ExtractedFacts map[string]string `json:"extracted_facts"`
	Reasoning      string            `json:"reasoning"`
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

func (d *DiscoveryManager) NextQuestion(ctx context.Context, analysis *OutfitAnalysis, responses []session.UserResponse, assistantMessages []string, turn int, coveredFacts map[string]string) (*DiscoveryResult, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("discovery: marshal analysis: %w", err)
	}

	history := buildConversationHistory(assistantMessages, responses)
	knownSection := buildKnownFacts(coveredFacts)
	pendingSection := buildPendingCategories(coveredFacts)

	prompt := fmt.Sprintf(discoveryPrompt, string(analysisJSON), history, knownSection, pendingSection, turn)

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

var categoryLabels = map[string]string{
	"occasion":    "Ocasión",
	"intention":   "Intención / qué quiere proyectar",
	"exploration": "Enfoque (refinar vs explorar)",
	"pain_points": "Puntos de dolor con su look actual",
	"aspirational": "Referente de estilo",
	"constraints": "Restricciones / qué evitar",
	"budget":      "Presupuesto / abierto a piezas nuevas",
}

func buildKnownFacts(facts map[string]string) string {
	if len(facts) == 0 {
		return "(nada aún)"
	}
	var b strings.Builder
	for cat, fact := range facts {
		label := categoryLabels[cat]
		if label == "" {
			label = cat
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, fact)
	}
	return b.String()
}

func buildPendingCategories(coveredFacts map[string]string) string {
	var pending []string
	for _, cat := range session.AllCategories {
		if _, ok := coveredFacts[cat]; !ok {
			label := categoryLabels[cat]
			if label == "" {
				label = cat
			}
			pending = append(pending, fmt.Sprintf("- %s (%s)", cat, label))
		}
	}
	if len(pending) == 0 {
		return "(todas cubiertas)"
	}
	return strings.Join(pending, "\n")
}
