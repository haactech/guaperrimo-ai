package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"stylerag/internal/llm"
	"stylerag/internal/shopping"
)

type sseEvent struct {
	Name string
	Data map[string]any
}

// readSSE posts body to url and returns the response plus its parsed events
// (comment lines are skipped) and the raw body.
func readSSE(t *testing.T, url string, body any, headers map[string]string) (*http.Response, []sseEvent, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return resp, nil, string(raw)
	}
	var events []sseEvent
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cur *sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event: "):
			cur = &sseEvent{Name: strings.TrimPrefix(line, "event: ")}
		case strings.HasPrefix(line, "data: "):
			if cur == nil {
				t.Fatalf("data without event: %q", line)
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.Data); err != nil {
				t.Fatalf("bad event data %q: %v", line, err)
			}
		case line == "":
			if cur != nil {
				events = append(events, *cur)
				cur = nil
			}
		}
	}
	return resp, events, string(raw)
}

// names renders events as "status:thinking", "turn", ...
func names(evs []sseEvent) []string {
	var out []string
	for _, e := range evs {
		n := e.Name
		if e.Name == "status" {
			n += ":" + e.Data["stage"].(string)
		}
		out = append(out, n)
	}
	return out
}

func contains(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

// streamFlowModel scripts the same four-turn conversation as TestFullFlow
// minus the last follow-up.
func streamFlowModel() *scripted {
	fake := shopping.Fake{}
	ctx := context.Background()
	shirts, _ := fake.SearchProducts(ctx, shopping.ProductQuery{Query: "camisa hombre", Limit: 6})
	pants, _ := fake.SearchProducts(ctx, shopping.ProductQuery{Query: "pantalón hombre", Limit: 6})
	shirtID, pantsID := shirts[0].ID, pants[1].ID
	searches := func(a, b string) []llm.ToolCall {
		return []llm.ToolCall{
			call(a, "search_products", map[string]any{"query": "camisa hombre"}),
			call(b, "search_products", map[string]any{"query": "pantalón hombre"}),
		}
	}
	return &scripted{responses: []*llm.CompletionResponse{
		calls(call("a1", "ask_user", map[string]any{
			"message": "Buena base. ¿Para qué ocasión?", "input_mode": "buttons",
			"options": []map[string]string{{"id": "boda", "label": "Boda de día"}, {"id": "cita", "label": "Cita"}},
		})),
		calls(append([]llm.ToolCall{
			call("u1", "update_profile", map[string]any{"occasion": "boda de día", "budget_mxn": 2000, "allow_shipping": true}),
		}, searches("s2", "s3")...)...),
		calls(searches("s4", "s5")...),
		calls(call("f1", "finish_recommendation", map[string]any{
			"summary": "Con camisa de lino y chino claro vas perfecto para una boda de día.",
			"shopping_list": []map[string]any{
				{"slot": "upper_body", "description": "Camisa de lino azul marino", "product_ids": []string{shirtID}, "priority": 1},
				{"slot": "lower_body", "description": "Chino beige", "product_ids": []string{pantsID}, "priority": 2},
			},
			"looks": []map[string]any{{"name": "Boda de día", "pieces": []map[string]string{{"slot": "upper_body", "product_id": shirtID}, {"slot": "lower_body", "product_id": pantsID}}}},
		})),
	}}
}

func TestChatStreamFullFlow(t *testing.T) {
	srv, _ := newTestServer(t, streamFlowModel(), "")
	base := srv.URL + "/session/sess_stream_01"
	ref, _ := newTestServer(t, streamFlowModel(), "")
	refBase := ref.URL + "/session/sess_stream_01"

	for _, b := range []string{base, refBase} {
		if code, body := uploadImage(t, b+"/image"); code != 200 {
			t.Fatalf("upload: %d %v", code, body)
		}
	}

	// image turn
	resp, evs, _ := readSSE(t, base+"/chat/stream", map[string]any{"type": "image"}, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers: %d %v", resp.StatusCode, resp.Header)
	}
	want := []string{"status:analyzing_photo", "status:thinking", "turn"}
	if got := names(evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("image turn events = %v, want %v", got, want)
	}
	refCode, refBody := postJSON(t, refBase+"/chat", map[string]any{"type": "image"}, nil)
	if refCode != 200 || !reflect.DeepEqual(evs[2].Data, refBody) {
		t.Fatalf("turn event differs from /chat:\n%v\n%v", evs[2].Data, refBody)
	}

	// button turn with a location: the server asks which store
	loc := map[string]any{"lat": 19.4194, "lng": -99.1616, "label": "Roma Norte"}
	button := map[string]any{"type": "button_response", "option_id": "boda", "location": loc}
	_, evs, _ = readSSE(t, base+"/chat/stream", button, nil)
	got := names(evs)
	if !contains(got, "status:finding_stores") || !contains(got, "status:thinking") || got[len(got)-1] != "turn" {
		t.Fatalf("button turn events = %v", got)
	}
	_, refBody = postJSON(t, refBase+"/chat", button, nil)
	if !reflect.DeepEqual(evs[len(evs)-1].Data, refBody) {
		t.Fatalf("button turn differs from /chat")
	}
	storeID := refBody["options"].([]any)[0].(map[string]any)["id"]

	// final turn
	choice := map[string]any{"type": "button_response", "option_id": storeID}
	_, evs, _ = readSSE(t, base+"/chat/stream", choice, nil)
	got = names(evs)
	wantFinal := []string{"status:thinking", "status:searching_products", "status:searching_products", "status:thinking", "status:composing", "turn"}
	if !reflect.DeepEqual(got, wantFinal) {
		t.Fatalf("final turn events = %v, want %v", got, wantFinal)
	}
	details := map[any]bool{evs[1].Data["detail"]: true, evs[2].Data["detail"]: true}
	if !details["camisa hombre"] || !details["pantalón hombre"] {
		t.Errorf("search details: %v", details)
	}
	turn := evs[len(evs)-1].Data
	if turn["is_final"] != true || turn["session_id"] != "sess_stream_01" || len(turn["shopping_list"].([]any)) != 2 {
		t.Fatalf("final turn: %v", turn)
	}
	_, refBody = postJSON(t, refBase+"/chat", choice, nil)
	if !reflect.DeepEqual(turn, refBody) {
		t.Errorf("final turn differs from /chat:\n%s\n%s", mustMarshal(t, turn), mustMarshal(t, refBody))
	}
}

func TestChatStreamErrors(t *testing.T) {
	srv, _ := newTestServer(t, &scripted{}, "k")
	h := map[string]string{"X-API-Key": "k"}
	url := srv.URL + "/session/sess_stream_02"

	// rejected before the stream starts: same plain JSON status and message as /chat
	for _, body := range []any{
		map[string]any{"type": "voice_response", "transcript": "hola"}, // 404 session
		map[string]any{"type": "bogus"},                                // 400
	} {
		cCode, cBody := postJSON(t, url+"/chat", body, h)
		resp, evs, raw := readSSE(t, url+"/chat/stream", body, h)
		var sBody map[string]any
		_ = json.Unmarshal([]byte(raw), &sBody)
		if evs != nil || resp.StatusCode != cCode || !reflect.DeepEqual(sBody, cBody) || cCode < 400 {
			t.Errorf("%v: stream %d %q, chat %d %v", body, resp.StatusCode, raw, cCode, cBody)
		}
	}
	if resp, _, _ := readSSE(t, url+"/chat/stream", map[string]any{"type": "image"}, nil); resp.StatusCode != 401 {
		t.Errorf("missing key: %d", resp.StatusCode)
	}

	// failing after the stream opened: an error event with the /chat status and message
	cCode, cBody := postJSON(t, url+"/chat", map[string]any{"type": "image"}, h) // no photo uploaded
	resp, evs, _ := readSSE(t, url+"/chat/stream", map[string]any{"type": "image"}, h)
	if cCode != 409 || resp.StatusCode != 200 || len(evs) == 0 {
		t.Fatalf("chat %d, stream %d %v", cCode, resp.StatusCode, evs)
	}
	last := evs[len(evs)-1]
	if last.Name != "error" || last.Data["status"] != float64(cCode) || last.Data["message"] != cBody["error"] {
		t.Errorf("error event = %+v, chat = %v", last, cBody)
	}
	for _, e := range evs[:len(evs)-1] {
		if e.Name != "status" {
			t.Errorf("unexpected event before error: %+v", e)
		}
	}
}

type slowModel struct {
	llm.Provider
	delay time.Duration
}

func (m slowModel) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	time.Sleep(m.delay)
	return m.Provider.Complete(ctx, req)
}

func TestChatStreamPing(t *testing.T) {
	old := streamPingInterval
	streamPingInterval = 20 * time.Millisecond
	t.Cleanup(func() { streamPingInterval = old })

	model := slowModel{Provider: streamFlowModel(), delay: 150 * time.Millisecond}
	srv, _ := newTestServer(t, model, "")
	base := srv.URL + "/session/sess_stream_03"
	uploadImage(t, base+"/image")
	_, evs, raw := readSSE(t, base+"/chat/stream", map[string]any{"type": "image"}, nil)
	if !strings.Contains(raw, ": ping\n\n") {
		t.Errorf("no ping in %q", raw)
	}
	if got := names(evs); got[len(got)-1] != "turn" || strings.HasSuffix(raw, ": ping\n\n") {
		t.Errorf("events %v, raw ends with ping: %v", got, strings.HasSuffix(raw, ": ping\n\n"))
	}
}
