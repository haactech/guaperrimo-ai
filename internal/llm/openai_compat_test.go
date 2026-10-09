package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const toolCallResponse = `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"abc123def","type":"function","function":{"name":"ask_user","arguments":"{\"message\":\"hola\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`

type capture struct {
	mu   sync.Mutex
	reqs []map[string]any
}

func newServer(t *testing.T, cap *capture, handler func(n int) (int, string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		cap.mu.Lock()
		cap.reqs = append(cap.reqs, body)
		n := len(cap.reqs)
		cap.mu.Unlock()
		code, resp := handler(n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(resp))
	}))
}

func sampleRequest() CompletionRequest {
	return CompletionRequest{
		Messages: []Message{
			Text(RoleSystem, "sys"),
			{Role: RoleUser, ContentBlocks: []ContentBlock{{Type: "text", Text: "hi"}, {Type: "image_url", ImageURL: "data:image/jpeg;base64,AAAA"}}},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "abc123def", Name: "search", Arguments: "{}"}}},
			ToolResult("abc123def", "search", `{"ok":true}`),
		},
		Tools:       []Tool{{Name: "ask_user", Description: "ask", Parameters: map[string]any{"type": "object"}}},
		ToolChoice:  ToolChoiceRequired,
		Temperature: Float(0.4),
		MaxTokens:   100,
	}
}

func TestMistralWireFormat(t *testing.T) {
	cap := &capture{}
	srv := newServer(t, cap, func(int) (int, string) { return 200, toolCallResponse })
	defer srv.Close()

	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", Flavor: FlavorMistral})
	resp, err := p.Complete(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "ask_user" || resp.ToolCalls[0].ID != "abc123def" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}

	req := cap.reqs[0]
	if req["tool_choice"] != "any" {
		t.Errorf("mistral tool_choice = %v, want any", req["tool_choice"])
	}
	if req["temperature"] != 0.4 {
		t.Errorf("temperature = %v", req["temperature"])
	}
	msgs := req["messages"].([]any)
	user := msgs[1].(map[string]any)
	blocks := user["content"].([]any)
	img := blocks[1].(map[string]any)
	if img["image_url"] != "data:image/jpeg;base64,AAAA" {
		t.Errorf("mistral image_url should be a flat string, got %v", img["image_url"])
	}
	assistant := msgs[2].(map[string]any)
	tc := assistant["tool_calls"].([]any)[0].(map[string]any)
	if tc["function"].(map[string]any)["name"] != "search" || tc["type"] != "function" {
		t.Errorf("assistant tool call not serialised: %v", tc)
	}
	tool := msgs[3].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "abc123def" || tool["name"] != "search" {
		t.Errorf("tool message malformed: %v", tool)
	}
	tools := req["tools"].([]any)
	if tools[0].(map[string]any)["function"].(map[string]any)["name"] != "ask_user" {
		t.Errorf("tools not serialised: %v", tools)
	}
}

func TestMoonshotQuirks(t *testing.T) {
	cap := &capture{}
	srv := newServer(t, cap, func(int) (int, string) { return 200, toolCallResponse })
	defer srv.Close()

	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", Flavor: FlavorMoonshot})
	if _, err := p.Complete(context.Background(), sampleRequest()); err != nil {
		t.Fatalf("complete: %v", err)
	}
	req := cap.reqs[0]
	if _, ok := req["temperature"]; ok {
		t.Errorf("moonshot must omit temperature")
	}
	if req["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v, want required", req["tool_choice"])
	}
	msgs := req["messages"].([]any)
	img := msgs[1].(map[string]any)["content"].([]any)[1].(map[string]any)
	if _, ok := img["image_url"].(map[string]any); !ok {
		t.Errorf("openai-style image_url must be an object, got %v", img["image_url"])
	}
}

func TestRetriesTransientErrors(t *testing.T) {
	cap := &capture{}
	srv := newServer(t, cap, func(n int) (int, string) {
		if n == 1 {
			return 500, `{"error":{"message":"boom"}}`
		}
		return 200, `{"choices":[{"message":{"role":"assistant","content":"hola"},"finish_reason":"stop"}]}`
	})
	defer srv.Close()

	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", Flavor: FlavorOpenAI, MaxRetries: 1})
	resp, err := p.Complete(context.Background(), CompletionRequest{Messages: []Message{Text(RoleUser, "hi")}})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if resp.Content != "hola" || len(cap.reqs) != 2 {
		t.Fatalf("content=%q calls=%d", resp.Content, len(cap.reqs))
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	cap := &capture{}
	srv := newServer(t, cap, func(int) (int, string) { return 400, `{"error":{"message":"bad"}}` })
	defer srv.Close()

	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", Flavor: FlavorOpenAI, MaxRetries: 2})
	if _, err := p.Complete(context.Background(), CompletionRequest{Messages: []Message{Text(RoleUser, "hi")}}); err == nil {
		t.Fatal("expected error")
	}
	if len(cap.reqs) != 1 {
		t.Fatalf("expected a single call, got %d", len(cap.reqs))
	}
}

func TestContentTextShapes(t *testing.T) {
	if got := contentText(json.RawMessage(`"hola"`)); got != "hola" {
		t.Errorf("string: %q", got)
	}
	if got := contentText(json.RawMessage(`null`)); got != "" {
		t.Errorf("null: %q", got)
	}
	if got := contentText(json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`)); got != "ab" {
		t.Errorf("array: %q", got)
	}
}
