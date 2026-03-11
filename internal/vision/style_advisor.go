package vision

import (
	"context"
	"encoding/json"
	"fmt"

	"stylerag/internal/llm"
	"stylerag/internal/rag"
	"stylerag/internal/session"
)

// GapProductContext associates a gap description with products found for it.
type GapProductContext struct {
	Gap      string        `json:"gap"`
	Products []rag.Product `json:"products"`
}

// compactProduct is a minimal product representation for the LLM prompt.
type compactProduct struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price,omitempty"`
	ImageURL string  `json:"image_url,omitempty"`
}

type compactGapProducts struct {
	Gap      string           `json:"gap"`
	Products []compactProduct `json:"products"`
}

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
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &advice); err != nil {
		return nil, fmt.Errorf("style advisor: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	return &advice, nil
}

func (a *StyleAdvisor) GenerateRecommendation(ctx context.Context, profile *session.UserStyleProfile) (*session.PersonalizedAdvice, error) {
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("recommendation: marshal profile: %w", err)
	}

	prompt := fmt.Sprintf(advisorPromptWithTheory, string(profileJSON))

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:   8192,
		Temperature: 0.7,
	}

	resp, err := a.router.Complete(ctx, llm.TurnTypeRecommendation, req)
	if err != nil {
		return nil, fmt.Errorf("recommendation: LLM call failed: %w", err)
	}

	var advice session.PersonalizedAdvice
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &advice); err != nil {
		return nil, fmt.Errorf("recommendation: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	return &advice, nil
}

// GenerateRecommendationWithProducts generates recommendations referencing real catalog products.
// Falls back to GenerateRecommendation if gapProducts is empty.
func (a *StyleAdvisor) GenerateRecommendationWithProducts(ctx context.Context, profile *session.UserStyleProfile, gapProducts []GapProductContext) (*session.PersonalizedAdvice, error) {
	if len(gapProducts) == 0 {
		return a.GenerateRecommendation(ctx, profile)
	}

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("recommendation: marshal profile: %w", err)
	}

	// Build compact product context to minimize tokens
	compact := make([]compactGapProducts, len(gapProducts))
	for i, gp := range gapProducts {
		prods := make([]compactProduct, len(gp.Products))
		for j, p := range gp.Products {
			prods[j] = compactProduct{
				ID:       p.ID,
				Name:     p.Name,
				Price:    p.Price,
				ImageURL: p.ImageURL,
			}
		}
		compact[i] = compactGapProducts{
			Gap:      gp.Gap,
			Products: prods,
		}
	}

	productsJSON, err := json.Marshal(compact)
	if err != nil {
		return nil, fmt.Errorf("recommendation: marshal products: %w", err)
	}

	prompt := fmt.Sprintf(advisorPromptWithProducts, string(profileJSON), string(productsJSON))

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:   8192,
		Temperature: 0.7,
	}

	resp, err := a.router.Complete(ctx, llm.TurnTypeRecommendation, req)
	if err != nil {
		return nil, fmt.Errorf("recommendation: LLM call failed: %w", err)
	}

	var advice session.PersonalizedAdvice
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &advice); err != nil {
		return nil, fmt.Errorf("recommendation: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	return &advice, nil
}
