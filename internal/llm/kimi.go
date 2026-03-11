package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

const kimiAPIURL = "https://api.moonshot.ai/v1/chat/completions"

// KimiProvider implements Provider for the Moonshot/Kimi API (OpenAI-compatible).
type KimiProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewKimiProvider(apiKey, model string) *KimiProvider {
	return &KimiProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// OpenAI-compatible request/response types

type kimiRequest struct {
	Model          string        `json:"model"`
	Messages       []kimiMessage `json:"messages"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	Temperature    *float64      `json:"temperature,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

type kimiMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []kimiContentBlock
}

// kimiContentBlock uses the OpenAI format where image_url is a nested object.
type kimiContentBlock struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *kimiImageURL `json:"image_url,omitempty"`
}

type kimiImageURL struct {
	URL string `json:"url"`
}

type kimiResponse struct {
	Choices []kimiChoice `json:"choices"`
	Usage   kimiUsage    `json:"usage"`
}

type kimiChoice struct {
	Message kimiResponseMessage `json:"message"`
}

type kimiResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type kimiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (p *KimiProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	body, err := p.buildRequest(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, kimiAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("kimi: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("kimi: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: read response: %w", err)
	}

	slog.DebugContext(ctx, "kimi: raw API response", "status", resp.StatusCode, "body", string(respBody))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var kResp kimiResponse
	if err := json.Unmarshal(respBody, &kResp); err != nil {
		return nil, fmt.Errorf("kimi: parse response: %w\nraw body: %s", err, string(respBody))
	}

	if len(kResp.Choices) == 0 {
		return nil, fmt.Errorf("kimi: empty response (no choices)\nraw body: %s", string(respBody))
	}

	choice := kResp.Choices[0]
	if choice.Message.Content == "" {
		slog.WarnContext(ctx, "kimi: empty content in response", "raw_body", string(respBody))
	}
	return &CompletionResponse{
		Content: choice.Message.Content,
		Usage: Usage{
			InputTokens:  kResp.Usage.PromptTokens,
			OutputTokens: kResp.Usage.CompletionTokens,
		},
	}, nil
}

func (p *KimiProvider) StreamComplete(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("kimi: streaming not implemented")
}

func (p *KimiProvider) buildRequest(req CompletionRequest) ([]byte, error) {
	kReq := kimiRequest{
		Model:     p.model,
		MaxTokens: req.MaxTokens,
		// Omit ResponseFormat — Kimi may return empty content with json_object mode;
		// our prompts already request JSON output explicitly.
	}

	// Kimi models only accept temperature=1; omit to use their default


	for _, msg := range req.Messages {
		kMsg := kimiMessage{
			Role: string(msg.Role),
		}

		if len(msg.ContentBlocks) > 0 {
			blocks := make([]kimiContentBlock, 0, len(msg.ContentBlocks))
			for _, cb := range msg.ContentBlocks {
				block := kimiContentBlock{Type: cb.Type}
				if cb.Type == "text" {
					block.Text = cb.Text
				} else if cb.Type == "image_url" {
					block.ImageURL = &kimiImageURL{URL: cb.ImageURL}
				}
				blocks = append(blocks, block)
			}
			kMsg.Content = blocks
		} else {
			kMsg.Content = msg.Content
		}

		kReq.Messages = append(kReq.Messages, kMsg)
	}

	return json.Marshal(kReq)
}
