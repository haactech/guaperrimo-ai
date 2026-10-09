// Package llm defines a minimal chat-completion abstraction with tool calling.
package llm

import "context"

// Role identifies who authored a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentBlock is one part of a multimodal message.
type ContentBlock struct {
	Type     string `json:"type"`                // "text" | "image_url"
	Text     string `json:"text,omitempty"`      // for type=text
	ImageURL string `json:"image_url,omitempty"` // data URI or https URL
}

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON object encoded as a string
}

// Message is one entry of a conversation, including tool traffic. It is
// JSON-serialisable so a session can persist its full agent memory.
type Message struct {
	Role          Role           `json:"role"`
	Content       string         `json:"content,omitempty"`
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"`
	ToolCalls     []ToolCall     `json:"tool_calls,omitempty"`   // assistant only
	ToolCallID    string         `json:"tool_call_id,omitempty"` // tool only
	Name          string         `json:"name,omitempty"`         // tool only
}

// Text builds a plain text message.
func Text(role Role, text string) Message { return Message{Role: role, Content: text} }

// ToolResult builds the message that answers a tool call.
func ToolResult(callID, name, content string) Message {
	return Message{Role: RoleTool, ToolCallID: callID, Name: name, Content: content}
}

// Tool describes a function the model may call.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON schema
}

// ToolChoice controls whether the model must call a tool.
type ToolChoice string

const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
	ToolChoiceNone     ToolChoice = "none"
)

// CompletionRequest is a provider-agnostic chat request.
type CompletionRequest struct {
	Messages    []Message
	Tools       []Tool
	ToolChoice  ToolChoice
	MaxTokens   int
	Temperature *float64
	JSONMode    bool // ask for a JSON object response when the provider supports it
}

// Usage reports token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// CompletionResponse is the model's answer.
type CompletionResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
}

// Provider is a chat-completion backend.
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Name() string
}

// Float returns a pointer to v, handy for optional temperatures.
func Float(v float64) *float64 { return &v }
