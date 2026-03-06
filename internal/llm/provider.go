package llm

import (
	"context"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentBlock represents a single piece of content in a multimodal message
type ContentBlock struct {
	Type     string `json:"type"`      // "text" or "image_url"
	Text     string `json:"text"`      // for type="text"
	ImageURL string `json:"image_url"` // for type="image_url": "data:image/jpeg;base64,..."
}

// Message represents a chat message. If ContentBlocks is non-empty, it takes
// precedence over Content for multimodal requests.
type Message struct {
	Role          Role           `json:"role"`
	Content       string         `json:"content"`
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"`
}

type CompletionRequest struct {
	Messages       []Message
	MaxTokens      int
	Temperature    float64
	Tools          []Tool
	ResponseFormat *ResponseFormat // optional: force structured output
}

// ResponseFormat controls the output format of the model.
type ResponseFormat struct {
	Type string `json:"type"` // "json_object" or "text"
}

type CompletionResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}

// Provider defines the interface for LLM providers (Anthropic, OpenAI, etc.)
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	StreamComplete(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)
}

// ModelTier represents the cost/capability tier for routing
type ModelTier string

const (
	ModelTierPotent  ModelTier = "potent"  // Sonnet, GPT-4o - for complex reasoning
	ModelTierEconomy ModelTier = "economy" // Haiku, 4o-mini - for simple tasks
)
