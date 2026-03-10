package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"stylerag/internal/llm"
)

// LLMAnalyzer implements Analyzer using an LLM provider via the Router.
type LLMAnalyzer struct {
	router *llm.Router
}

func NewLLMAnalyzer(router *llm.Router) *LLMAnalyzer {
	return &LLMAnalyzer{router: router}
}

func (a *LLMAnalyzer) AnalyzeOutfit(ctx context.Context, imageData []byte) (*OutfitAnalysis, error) {
	mediaType := detectMediaType(imageData)
	if mediaType == "" {
		return nil, fmt.Errorf("vision: unsupported image format")
	}

	encoded := base64.StdEncoding.EncodeToString(imageData)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mediaType, encoded)

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				ContentBlocks: []llm.ContentBlock{
					{Type: "text", Text: outfitAnalysisPrompt},
					{Type: "image_url", ImageURL: dataURI},
				},
			},
		},
		MaxTokens:      4096,
		Temperature:    0.1,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}

	resp, err := a.router.Complete(ctx, llm.TurnTypeImageAnalysis, req)
	if err != nil {
		return nil, fmt.Errorf("vision: LLM call failed: %w", err)
	}

	var analysis OutfitAnalysis
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &analysis); err != nil {
		return nil, fmt.Errorf("vision: failed to parse LLM response as OutfitAnalysis: %w\nraw response: %s", err, resp.Content)
	}

	return &analysis, nil
}

// detectMediaType returns the MIME type based on magic bytes, or empty string if unknown.
func detectMediaType(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	// Use net/http's built-in detection as primary
	detected := http.DetectContentType(data)
	switch detected {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return detected
	}
	return ""
}
