package vision

import (
	"context"
	"encoding/json"
	"fmt"

	"stylerag/internal/llm"
	"stylerag/internal/session"
)

// DiagnosisGenerator runs the Phase 3 diagnosis LLM call.
type DiagnosisGenerator struct {
	router *llm.Router
}

func NewDiagnosisGenerator(router *llm.Router) *DiagnosisGenerator {
	return &DiagnosisGenerator{router: router}
}

func (d *DiagnosisGenerator) Generate(ctx context.Context, analysis *OutfitAnalysis, responses []session.UserResponse) (*session.StyleDiagnosis, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: marshal analysis: %w", err)
	}

	responsesJSON, err := json.Marshal(responses)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: marshal responses: %w", err)
	}

	prompt := fmt.Sprintf(diagnosisPrompt, string(analysisJSON), string(responsesJSON))

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:   8192,
		Temperature: 0.3,
	}

	resp, err := d.router.Complete(ctx, llm.TurnTypeDiagnosis, req)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: LLM call failed: %w", err)
	}

	var diagnosis session.StyleDiagnosis
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &diagnosis); err != nil {
		return nil, fmt.Errorf("diagnosis: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	return &diagnosis, nil
}
