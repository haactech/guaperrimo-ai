package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"stylerag/internal/agent"
	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

// --- fakes ---

type memImages struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memImages) Upload(_ context.Context, in storage.UploadInput) (*storage.UploadOutput, error) {
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[in.Key] = data
	return &storage.UploadOutput{URL: m.URL(in.Key)}, nil
}

func (m *memImages) Download(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}

func (m *memImages) ListKeys(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *memImages) URL(key string) string { return "https://img.test/" + key }

type fakeAnalyzer struct{}

func (fakeAnalyzer) AnalyzeOutfit(_ context.Context, data []byte) (*vision.OutfitAnalysis, error) {
	if len(data) == 0 {
		return nil, errors.New("empty image")
	}
	return &vision.OutfitAnalysis{DetectedStyles: []string{"casual"}, Observations: "buena base"}, nil
}

type scripted struct {
	mu        sync.Mutex
	responses []*llm.CompletionResponse
}

func (s *scripted) Name() string { return "scripted" }

func (s *scripted) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.responses) == 0 {
		return nil, errors.New("script exhausted")
	}
	r := s.responses[0]
	s.responses = s.responses[1:]
	return r, nil
}

func call(id, name string, args any) llm.ToolCall {
	b, _ := json.Marshal(args)
	return llm.ToolCall{ID: id, Name: name, Arguments: string(b)}
}

func calls(tcs ...llm.ToolCall) *llm.CompletionResponse {
	return &llm.CompletionResponse{ToolCalls: tcs}
}

// --- helpers ---

func newTestServer(t *testing.T, model llm.Provider, apiKey string) (*httptest.Server, session.Store) {
	t.Helper()
	store := session.NewMemoryStore(time.Hour)
	t.Cleanup(store.Stop)
	images := &memImages{}
	runner := agent.NewRunner(model, agent.Deps{Shopping: shopping.Fake{}}, 8, 4)
	router := NewRouter(Deps{
		APIKey: apiKey,
		Images: images,
		Store:  store,
		Chat:   &ChatDeps{Store: store, Images: images, Analyzer: fakeAnalyzer{}, Runner: runner, TurnTimeout: 10 * time.Second},
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, store
}

func postJSON(t *testing.T, url string, body any, headers map[string]string) (int, map[string]any) {
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
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func uploadImage(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="image"; filename="outfit.jpg"`}
	h["Content-Type"] = []string{"image/jpeg"}
	part, _ := mw.CreatePart(h)
	_, _ = part.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3})
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// --- tests ---

func TestFullFlow(t *testing.T) {
	ctx := context.Background()
	fake := shopping.Fake{}
	shirts, _ := fake.SearchProducts(ctx, shopping.ProductQuery{Query: "camisa hombre", Limit: 6})
	pants, _ := fake.SearchProducts(ctx, shopping.ProductQuery{Query: "pantalón hombre", Limit: 6})
	shirtID, pantsID := shirts[0].ID, pants[1].ID // Zara MX 899, H&M México 549

	model := &scripted{responses: []*llm.CompletionResponse{
		// turn 1: photo ready → question with buttons
		calls(call("a1", "ask_user", map[string]any{
			"message": "Buena base. ¿Para qué ocasión?", "input_mode": "buttons",
			"options": []map[string]string{{"id": "boda", "label": "Boda de día"}, {"id": "cita", "label": "Cita"}},
		})),
		// turn 2: button → record, search, finish
		calls(
			call("u1", "update_profile", map[string]any{"occasion": "boda de día", "budget_mxn": 2000, "allow_shipping": true}),
			call("s1", "find_nearby_stores", map[string]any{}),
			call("s2", "search_products", map[string]any{"query": "camisa hombre"}),
			call("s3", "search_products", map[string]any{"query": "pantalón hombre"}),
		),
		calls(call("f1", "finish_recommendation", map[string]any{
			"summary":          "Con camisa de lino y chino claro vas perfecto para una boda de día.",
			"priority_actions": []map[string]any{{"id": "camisa_lino", "title": "Camisa de lino", "description": "Azul marino slim", "impact": "alto", "effort": "bajo", "product_ids": []string{shirtID}}},
			"shopping_list": []map[string]any{
				{"slot": "upper_body", "description": "Camisa de lino azul marino", "why": "alarga el torso", "product_ids": []string{shirtID}, "priority": 1},
				{"slot": "lower_body", "description": "Chino beige", "product_ids": []string{pantsID}, "priority": 2},
			},
			"looks": []map[string]any{{"name": "Boda de día", "description": "fresco y limpio", "pieces": []map[string]string{{"slot": "upper_body", "product_id": shirtID}, {"slot": "lower_body", "product_id": pantsID}}}},
		})),
		// turn 3: follow-up after the recommendation
		calls(call("a2", "ask_user", map[string]any{"message": "¿Quieres que busque zapatos también?", "input_mode": "voice"})),
	}}
	srv, _ := newTestServer(t, model, "")
	base := srv.URL + "/session/sess_flow_01"

	if code, body := postJSON(t, base+"/chat", map[string]any{"type": "voice_response", "transcript": "hola"}, nil); code != 404 {
		t.Fatalf("chat before photo: %d %v", code, body)
	}
	if code, body := uploadImage(t, base+"/image"); code != 200 || body["url"] == nil {
		t.Fatalf("upload: %d %v", code, body)
	}

	code, body := postJSON(t, base+"/chat", map[string]any{"type": "image", "image_url": "x"}, nil)
	if code != 200 || body["input_mode"] != "buttons" || body["is_final"] != false {
		t.Fatalf("image turn: %d %v", code, body)
	}
	if opts := body["options"].([]any); len(opts) != 2 {
		t.Fatalf("options: %v", body["options"])
	}
	if code, _ := postJSON(t, base+"/chat", map[string]any{"type": "image"}, nil); code != 409 {
		t.Fatalf("second image turn should be 409, got %d", code)
	}

	code, body = postJSON(t, base+"/chat", map[string]any{
		"type": "button_response", "option_id": "boda",
		"location": map[string]any{"lat": 19.4194, "lng": -99.1616, "label": "Roma Norte"},
	}, nil)
	if code != 200 || body["is_final"] != true || body["location_known"] != true {
		t.Fatalf("final turn: %d %v", code, body)
	}
	list := body["shopping_list"].([]any)
	if len(list) != 2 {
		t.Fatalf("shopping list: %v", list)
	}
	first := list[0].(map[string]any)
	products := first["products"].([]any)
	if len(products) != 1 || products[0].(map[string]any)["store"] != "Zara MX" {
		t.Fatalf("first item products: %v", products)
	}
	if products[0].(map[string]any)["nearby_store"] == nil {
		t.Errorf("Zara product should carry a nearby store")
	}
	if stores := body["stores"].([]any); len(stores) == 0 {
		t.Errorf("stores should list the nearby matches")
	}
	if body["total_mxn"] != float64(899+549) {
		t.Errorf("total_mxn = %v", body["total_mxn"])
	}
	if actions := body["priority_actions"].([]any); len(actions) != 1 {
		t.Errorf("priority actions: %v", actions)
	}
	if _, ok := body["looks_generating"]; ok {
		t.Errorf("looks_generating must be omitted when VTON is disabled")
	}

	code, body = getJSON(t, base+"/recommendation")
	if code != 200 || body["is_final"] != true || body["message"] == "" {
		t.Fatalf("recommendation: %d %v", code, body)
	}
	code, body = getJSON(t, base+"/looks")
	if code != 200 || body["status"] != "none" || len(body["looks"].([]any)) != 1 {
		t.Fatalf("looks: %d %v", code, body)
	}
	piece := body["looks"].([]any)[0].(map[string]any)["pieces"].([]any)[0].(map[string]any)
	if piece["product_image_url"] == "" || piece["product_name"] == "" {
		t.Errorf("look piece should resolve product fields: %v", piece)
	}

	code, body = postJSON(t, base+"/chat", map[string]any{"type": "voice_response", "transcript": "¿y zapatos?"}, nil)
	if code != 200 || body["is_final"] != false || body["input_mode"] != "voice" {
		t.Fatalf("follow-up after recommendation: %d %v", code, body)
	}
}

func TestValidationAndAuth(t *testing.T) {
	srv, _ := newTestServer(t, &scripted{}, "secret")

	if code, _ := getJSON(t, srv.URL+"/health"); code != 200 {
		t.Fatalf("health must not require auth, got %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/session/sess_auth_1/chat", map[string]any{"type": "image"}, nil); code != 401 {
		t.Fatalf("expected 401 without key, got %d", code)
	}
	code, body := postJSON(t, srv.URL+"/session/bad!id/chat", map[string]any{"type": "image"}, map[string]string{"X-API-Key": "secret"})
	if code != 400 {
		t.Fatalf("invalid id: %d %v", code, body)
	}
	code, _ = postJSON(t, srv.URL+"/session/sess_auth_1/chat", map[string]any{"type": "nope"}, map[string]string{"X-API-Key": "secret"})
	if code != 400 {
		t.Fatalf("invalid type: %d", code)
	}
	code, _ = postJSON(t, srv.URL+"/session/sess_auth_1/chat", map[string]any{"type": "image"}, map[string]string{"X-API-Key": "secret"})
	if code != 409 {
		t.Fatalf("image turn without an uploaded photo should be 409, got %d", code)
	}
	_ = fmt.Sprintf
}
