package vision

import (
	"context"
	"encoding/json"
	"fmt"

	"stylerag/internal/llm"
)

// StyleAdvice is the conversational response generated from an OutfitAnalysis.
type StyleAdvice struct {
	Message string        `json:"message"`
	Options []StyleOption `json:"options"`
}

// StyleOption represents a single style direction the user can explore.
type StyleOption struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// StyleAdvisor transforms a technical OutfitAnalysis into conversational style advice.
type StyleAdvisor struct {
	router *llm.Router
}

func NewStyleAdvisor(router *llm.Router) *StyleAdvisor {
	return &StyleAdvisor{router: router}
}

func (a *StyleAdvisor) GenerateStyleAdvice(ctx context.Context, analysis *OutfitAnalysis) (*StyleAdvice, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("style advisor: marshal analysis: %w", err)
	}

	prompt := fmt.Sprintf(styleAdvisorPrompt, string(analysisJSON))

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:      8192,
		Temperature:    0.7,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}

	resp, err := a.router.Complete(ctx, llm.TurnTypePreferenceGather, req)
	if err != nil {
		return nil, fmt.Errorf("style advisor: LLM call failed: %w", err)
	}

	var advice StyleAdvice
	if err := json.Unmarshal([]byte(resp.Content), &advice); err != nil {
		return nil, fmt.Errorf("style advisor: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	return &advice, nil
}
