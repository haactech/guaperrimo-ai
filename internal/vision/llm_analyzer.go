package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"stylerag/internal/llm"
)

// LLMAnalyzer implements Analyzer with a multimodal chat model.
type LLMAnalyzer struct {
	provider llm.Provider
}

// NewLLMAnalyzer wraps a vision-capable provider.
func NewLLMAnalyzer(p llm.Provider) *LLMAnalyzer {
	return &LLMAnalyzer{provider: p}
}

func (a *LLMAnalyzer) AnalyzeOutfit(ctx context.Context, imageData []byte) (*OutfitAnalysis, error) {
	mediaType := detectMediaType(imageData)
	if mediaType == "" {
		return nil, fmt.Errorf("vision: unsupported image format")
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(imageData))

	req := llm.CompletionRequest{
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: buildOutfitAnalysisPrompt()},
				{Type: "image_url", ImageURL: dataURI},
			},
		}},
		MaxTokens:   8192,
		Temperature: llm.Float(0.1),
		JSONMode:    true,
	}

	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("vision: LLM call failed: %w", err)
	}

	var analysis OutfitAnalysis
	if err := json.Unmarshal([]byte(CleanJSON(resp.Content)), &analysis); err != nil {
		return nil, fmt.Errorf("vision: parse analysis: %w", err)
	}
	return &analysis, nil
}

func detectMediaType(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	switch detected := http.DetectContentType(data); detected {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return detected
	}
	return ""
}
