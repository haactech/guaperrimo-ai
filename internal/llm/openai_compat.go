package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Flavor names the small request-format differences between OpenAI-compatible APIs.
type Flavor string

const (
	FlavorOpenAI   Flavor = "openai"
	FlavorMistral  Flavor = "mistral"
	FlavorMoonshot Flavor = "moonshot"
)

// OpenAICompatConfig configures a chat-completions client.
type OpenAICompatConfig struct {
	BaseURL    string // e.g. https://api.mistral.ai/v1
	APIKey     string
	Model      string
	Flavor     Flavor
	HTTPClient *http.Client
	MaxRetries int // retries on 429/5xx/network errors
}

// OpenAICompat talks to any /chat/completions endpoint (Mistral, Moonshot, OpenAI, ...).
type OpenAICompat struct {
	cfg OpenAICompatConfig
}

var tracer = otel.Tracer("guaperrimo/llm")

// NewOpenAICompat builds a client with sane defaults.
func NewOpenAICompat(cfg OpenAICompatConfig) *OpenAICompat {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &OpenAICompat{cfg: cfg}
}

func (p *OpenAICompat) Name() string { return fmt.Sprintf("%s:%s", p.cfg.Flavor, p.cfg.Model) }

// --- wire types (OpenAI chat-completions JSON) ---

type wireRequest struct {
	Model          string        `json:"model"`
	Messages       []wireMessage `json:"messages"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	Temperature    *float64      `json:"temperature,omitempty"`
	Tools          []wireTool    `json:"tools,omitempty"`
	ToolChoice     any           `json:"tool_choice,omitempty"`
	ResponseFormat *wireFormat   `json:"response_format,omitempty"`
}

type wireFormat struct {
	Type string `json:"type"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string      `json:"type"`
	Function wireToolDef `json:"function"`
}

type wireToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type wireResponse struct {
	Choices []struct {
		Message struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []wireToolCall  `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete sends the request, retrying transient failures.
func (p *OpenAICompat) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	ctx, span := tracer.Start(ctx, "llm.complete", trace.WithAttributes(
		attribute.String("llm.provider", string(p.cfg.Flavor)),
		attribute.String("llm.model", p.cfg.Model),
		attribute.Int("llm.tools", len(req.Tools)),
	))
	defer span.End()

	body, err := json.Marshal(p.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("%s: encode request: %w", p.Name(), err)
	}

	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, retry, err := p.do(ctx, body)
		if err == nil {
			span.SetAttributes(
				attribute.Int("llm.input_tokens", resp.Usage.InputTokens),
				attribute.Int("llm.output_tokens", resp.Usage.OutputTokens),
				attribute.Int("llm.tool_calls", len(resp.ToolCalls)),
			)
			return resp, nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			break
		}
		slog.WarnContext(ctx, "llm: transient failure, retrying", "provider", p.Name(), "attempt", attempt+1, "error", err)
	}
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, lastErr.Error())
	return nil, lastErr
}

// do performs one HTTP round trip. The bool reports whether a retry makes sense.
func (p *OpenAICompat) do(ctx context.Context, body []byte) (*CompletionResponse, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("%s: build request: %w", p.Name(), err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, true, fmt.Errorf("%s: request failed: %w", p.Name(), err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, fmt.Errorf("%s: read response: %w", p.Name(), err)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("%s: status %d: %s", p.Name(), resp.StatusCode, truncate(raw, 400))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("%s: status %d: %s", p.Name(), resp.StatusCode, truncate(raw, 400))
	}

	var wr wireResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return nil, false, fmt.Errorf("%s: parse response: %w", p.Name(), err)
	}
	if wr.Error != nil && wr.Error.Message != "" {
		return nil, false, fmt.Errorf("%s: api error: %s", p.Name(), wr.Error.Message)
	}
	if len(wr.Choices) == 0 {
		return nil, false, fmt.Errorf("%s: empty response (no choices)", p.Name())
	}

	choice := wr.Choices[0]
	out := &CompletionResponse{
		Content:      contentText(choice.Message.Content),
		FinishReason: choice.FinishReason,
		Usage: Usage{
			InputTokens:  wr.Usage.PromptTokens,
			OutputTokens: wr.Usage.CompletionTokens,
		},
	}
	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, false, nil
}

// contentText extracts text from a content field that may be a string, null,
// or an array of content parts.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, part := range parts {
			sb.WriteString(part.Text)
		}
		return sb.String()
	}
	return string(raw)
}

func (p *OpenAICompat) buildRequest(req CompletionRequest) wireRequest {
	wr := wireRequest{Model: p.cfg.Model, MaxTokens: req.MaxTokens}
	if req.Temperature != nil && p.cfg.Flavor != FlavorMoonshot {
		wr.Temperature = req.Temperature
	}
	if req.JSONMode && p.cfg.Flavor != FlavorMoonshot {
		wr.ResponseFormat = &wireFormat{Type: "json_object"}
	}
	for _, m := range req.Messages {
		wr.Messages = append(wr.Messages, p.wireMessage(m))
	}
	for _, t := range req.Tools {
		wr.Tools = append(wr.Tools, wireTool{
			Type:     "function",
			Function: wireToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}
	if len(req.Tools) > 0 && req.ToolChoice != "" {
		wr.ToolChoice = p.toolChoice(req.ToolChoice)
	}
	return wr
}

func (p *OpenAICompat) toolChoice(tc ToolChoice) any {
	switch tc {
	case ToolChoiceRequired:
		if p.cfg.Flavor == FlavorMistral {
			return "any"
		}
		return "required"
	case ToolChoiceNone:
		return "none"
	default:
		return "auto"
	}
}

func (p *OpenAICompat) wireMessage(m Message) wireMessage {
	wm := wireMessage{Role: string(m.Role), ToolCallID: m.ToolCallID, Name: m.Name}
	if len(m.ContentBlocks) > 0 {
		blocks := make([]any, 0, len(m.ContentBlocks))
		for _, b := range m.ContentBlocks {
			switch b.Type {
			case "image_url":
				if p.cfg.Flavor == FlavorMistral {
					blocks = append(blocks, map[string]any{"type": "image_url", "image_url": b.ImageURL})
				} else {
					blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]string{"url": b.ImageURL}})
				}
			default:
				blocks = append(blocks, map[string]any{"type": "text", "text": b.Text})
			}
		}
		wm.Content = blocks
	} else {
		wm.Content = m.Content
	}
	for _, tc := range m.ToolCalls {
		wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
			ID:       tc.ID,
			Type:     "function",
			Function: wireFunction{Name: tc.Name, Arguments: tc.Arguments},
		})
	}
	return wm
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
