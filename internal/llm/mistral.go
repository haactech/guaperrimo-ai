package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const mistralAPIURL = "https://api.mistral.ai/v1/chat/completions"

// MistralProvider implements Provider for the Mistral AI API.
type MistralProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewMistralProvider(apiKey, model string) *MistralProvider {
	return &MistralProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// mistral request/response types (OpenAI-compatible format)

type mistralRequest struct {
	Model          string           `json:"model"`
	Messages       []mistralMessage `json:"messages"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Temperature    *float64         `json:"temperature,omitempty"`
	Tools          []mistralTool    `json:"tools,omitempty"`
	ResponseFormat *ResponseFormat  `json:"response_format,omitempty"`
}

type mistralMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []mistralContentBlock
}

type mistralContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type mistralTool struct {
	Type     string             `json:"type"`
	Function mistralToolFunction `json:"function"`
}

type mistralToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type mistralResponse struct {
	Choices []mistralChoice `json:"choices"`
	Usage   mistralUsage    `json:"usage"`
}

type mistralChoice struct {
	Message mistralResponseMessage `json:"message"`
}

type mistralResponseMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []mistralToolCall   `json:"tool_calls,omitempty"`
}

type mistralToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function mistralToolCallFunc  `json:"function"`
}

type mistralToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type mistralUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (p *MistralProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	body, err := p.buildRequest(req)
	if err != nil {
		return nil, fmt.Errorf("mistral: build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, mistralAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mistral: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mistral: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mistral: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mistral: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var mResp mistralResponse
	if err := json.Unmarshal(respBody, &mResp); err != nil {
		return nil, fmt.Errorf("mistral: parse response: %w", err)
	}

	if len(mResp.Choices) == 0 {
		return nil, fmt.Errorf("mistral: empty response (no choices)")
	}

	choice := mResp.Choices[0]
	result := &CompletionResponse{
		Content: choice.Message.Content,
		Usage: Usage{
			InputTokens:  mResp.Usage.PromptTokens,
			OutputTokens: mResp.Usage.CompletionTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return result, nil
}

func (p *MistralProvider) StreamComplete(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("mistral: streaming not implemented")
}

func (p *MistralProvider) buildRequest(req CompletionRequest) ([]byte, error) {
	mReq := mistralRequest{
		Model:          p.model,
		MaxTokens:      req.MaxTokens,
		ResponseFormat: req.ResponseFormat,
	}

	if req.Temperature != 0 {
		t := req.Temperature
		mReq.Temperature = &t
	}

	for _, msg := range req.Messages {
		mMsg := mistralMessage{
			Role: string(msg.Role),
		}

		if len(msg.ContentBlocks) > 0 {
			blocks := make([]mistralContentBlock, 0, len(msg.ContentBlocks))
			for _, cb := range msg.ContentBlocks {
				blocks = append(blocks, mistralContentBlock{
					Type:     cb.Type,
					Text:     cb.Text,
					ImageURL: cb.ImageURL,
				})
			}
			mMsg.Content = blocks
		} else {
			mMsg.Content = msg.Content
		}

		mReq.Messages = append(mReq.Messages, mMsg)
	}

	for _, tool := range req.Tools {
		mReq.Tools = append(mReq.Tools, mistralTool{
			Type: "function",
			Function: mistralToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}

	return json.Marshal(mReq)
}
