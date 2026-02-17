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

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Messages    []Message
	MaxTokens   int
	Temperature float64
	Tools       []Tool
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
