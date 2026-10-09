package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveMistralToolRoundTrip hits the real Mistral API. It only runs when
// LLM_LIVE=1 and MISTRAL_API_KEY are set: `LLM_LIVE=1 go test ./internal/llm -run Live -v`.
func TestLiveMistralToolRoundTrip(t *testing.T) {
	key := os.Getenv("MISTRAL_API_KEY")
	if os.Getenv("LLM_LIVE") == "" || key == "" {
		t.Skip("set LLM_LIVE=1 and MISTRAL_API_KEY to run")
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "mistral-large-latest"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: "https://api.mistral.ai/v1", APIKey: key, Model: model, Flavor: FlavorMistral, MaxRetries: 1})
	tools := []Tool{{
		Name:        "get_weather",
		Description: "Devuelve el clima actual de una ciudad",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"city": map[string]any{"type": "string", "description": "Ciudad"}},
			"required":   []string{"city"},
		},
	}}
	msgs := []Message{
		Text(RoleSystem, "Eres un asistente. Usa herramientas cuando apliquen y responde en español."),
		Text(RoleUser, "¿Qué clima hay ahora en Monterrey?"),
	}

	resp, err := p.Complete(ctx, CompletionRequest{Messages: msgs, Tools: tools, ToolChoice: ToolChoiceRequired, MaxTokens: 300, Temperature: Float(0)})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(resp.ToolCalls) == 0 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("expected a get_weather tool call, got content=%q calls=%+v", resp.Content, resp.ToolCalls)
	}
	t.Logf("tool call: id=%s args=%s usage=%+v", resp.ToolCalls[0].ID, resp.ToolCalls[0].Arguments, resp.Usage)

	msgs = append(msgs,
		Message{Role: RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls},
		ToolResult(resp.ToolCalls[0].ID, "get_weather", `{"temp_c":31,"sky":"soleado"}`),
	)
	resp2, err := p.Complete(ctx, CompletionRequest{Messages: msgs, Tools: tools, ToolChoice: ToolChoiceAuto, MaxTokens: 300, Temperature: Float(0)})
	if err != nil {
		t.Fatalf("second call (tool result round trip): %v", err)
	}
	if strings.TrimSpace(resp2.Content) == "" {
		t.Fatalf("expected a text answer after the tool result, got %+v", resp2)
	}
	t.Logf("final answer: %s", resp2.Content)
}
