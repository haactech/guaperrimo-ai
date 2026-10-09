package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/vision"
)

// scripted replays canned model responses in order.
type scripted struct {
	responses []*llm.CompletionResponse
	calls     []llm.CompletionRequest
}

func (s *scripted) Name() string { return "scripted" }

func (s *scripted) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.calls = append(s.calls, req)
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

func newState() *session.State {
	st := session.New("sess_test_1")
	st.Phase = session.PhaseChat
	st.Analysis = &vision.OutfitAnalysis{DetectedStyles: []string{"casual"}, Observations: "outfit sencillo"}
	return st
}

func toolMessage(st *session.State, callID string) string {
	for _, m := range st.Messages {
		if m.Role == llm.RoleTool && m.ToolCallID == callID {
			return m.Content
		}
	}
	return ""
}

func TestFirstTurnRecordsProfileAndAsks(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(
			call("c1", toolUpdateProfile, map[string]any{"style_goal": "verse más arreglado", "constraints": []string{"nada de corbata"}}),
			call("c2", toolAskUser, map[string]any{
				"message": "¿Para qué ocasión te quieres vestir?", "input_mode": "buttons",
				"options": []map[string]string{{"id": "boda", "label": "Boda"}, {"id": "cita", "label": "Cita"}},
			}),
		),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}}, 5, 4)
	st := newState()

	out, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "system"})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if out.InputMode != "buttons" || len(out.Options) != 2 || out.IsFinal {
		t.Fatalf("unexpected output: %+v", out)
	}
	if st.Profile.StyleGoal != "verse más arreglado" || len(st.Profile.Constraints) != 1 {
		t.Errorf("profile not updated: %+v", st.Profile)
	}
	if len(st.Transcript) != 1 || st.Transcript[0].Role != "assistant" || st.Transcript[0].InputMode != "buttons" {
		t.Errorf("transcript: %+v", st.Transcript)
	}
	if st.Phase != session.PhaseChat || st.Turn != 1 {
		t.Errorf("phase=%s turn=%d", st.Phase, st.Turn)
	}
	// user, assistant, tool, tool
	if len(st.Messages) != 4 || st.Messages[2].Role != llm.RoleTool || st.Messages[3].ToolCallID != "c2" {
		t.Errorf("messages: %+v", st.Messages)
	}
	sys := model.calls[0].Messages[0]
	if sys.Role != llm.RoleSystem || !strings.Contains(sys.Content, "Preguntas hechas: 0") || !strings.Contains(sys.Content, "outfit sencillo") {
		t.Errorf("system prompt missing context: %s", sys.Content[:200])
	}
	if model.calls[0].ToolChoice != llm.ToolChoiceRequired || len(model.calls[0].Tools) != 6 {
		t.Errorf("tools not offered: choice=%s n=%d", model.calls[0].ToolChoice, len(model.calls[0].Tools))
	}
}

func TestFinishValidatesProductsAndBudget(t *testing.T) {
	ctx := context.Background()
	fake := shopping.Fake{}
	shirts, _ := fake.SearchProducts(ctx, shopping.ProductQuery{Query: "camisa hombre", Limit: 6})
	pants, _ := fake.SearchProducts(ctx, shopping.ProductQuery{Query: "pantalón hombre", Limit: 6})
	expensiveShirt, cheapShirt := shirts[2].ID, shirts[4].ID // 1299, 459
	pantsPick := pants[1].ID                                 // 549

	finish := func(id, shirtID, pantsID string) llm.ToolCall {
		return call(id, toolFinish, map[string]any{
			"summary":          "Tu base está bien; con una camisa de lino y un chino claro subes mucho.",
			"priority_actions": []map[string]any{{"id": "camisa", "title": "Camisa de lino", "description": "Azul marino, slim", "impact": "alto", "effort": "bajo", "product_ids": []string{shirtID}}},
			"shopping_list": []map[string]any{
				{"slot": "upper_body", "description": "Camisa de lino azul", "product_ids": []string{shirtID}, "priority": 1},
				{"slot": "lower_body", "description": "Chino beige", "product_ids": []string{pantsID}, "priority": 2},
			},
			"looks": []map[string]any{{"name": "Casual elevado", "pieces": []map[string]string{{"slot": "upper_body", "product_id": shirtID}, {"slot": "lower_body", "product_id": pantsID}}}},
		})
	}

	model := &scripted{responses: []*llm.CompletionResponse{
		calls(finish("f1", "p_invented", pantsPick)),
		calls(
			call("s1", toolFindStores, map[string]any{}),
			call("s2", toolSearchProducts, map[string]any{"query": "camisa hombre"}),
			call("s3", toolSearchProducts, map[string]any{"query": "pantalón hombre"}),
		),
		calls(finish("f2", expensiveShirt, pantsPick)),
		calls(finish("f3", cheapShirt, pantsPick)),
	}}
	r := NewRunner(model, Deps{Shopping: fake}, 8, 4)
	st := newState()
	st.Profile.BudgetMXN = 1000
	st.Location = &session.Location{Lat: 19.4194, Lng: -99.1616, Label: "Roma Norte", Source: "device"}

	out, err := r.RunTurn(ctx, st, TurnInput{Kind: "voice", Text: "ya, dame la recomendación"})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if !out.IsFinal || out.InputMode != "none" {
		t.Fatalf("expected final turn, got %+v", out)
	}
	if msg := toolMessage(st, "f1"); !strings.Contains(msg, "desconocidos") {
		t.Errorf("unknown product id should be rejected, got %s", msg)
	}
	if msg := toolMessage(st, "f2"); !strings.Contains(msg, "presupuesto") {
		t.Errorf("over-budget list should be rejected once, got %s", msg)
	}
	if msg := toolMessage(st, "s1"); !strings.Contains(msg, `"count"`) {
		t.Errorf("store search result: %s", msg)
	}
	if st.Recommendation == nil || st.Recommendation.TotalMXN != 459+549 {
		t.Fatalf("recommendation: %+v", st.Recommendation)
	}
	if st.Phase != session.PhaseDone || len(st.Looks) != 1 || len(st.Looks[0].Pieces) != 2 || st.Looks[0].ID != "look_1" {
		t.Errorf("looks/phase: %s %+v", st.Phase, st.Looks)
	}
	if len(st.Stores) == 0 || st.Products[cheapShirt].Title == "" {
		t.Errorf("state should keep stores and products")
	}
	if p := st.Products[shirts[0].ID]; p.NearbyStore == nil {
		t.Errorf("Zara MX product should be annotated with the nearby Zara store")
	}
}

func TestPlainTextFallbackBecomesQuestion(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{{Content: "¿Qué tipo de evento es?"}}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}}, 3, 4)
	st := newState()
	out, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputMode != "voice" || out.Message != "¿Qué tipo de evento es?" || len(st.Transcript) != 1 {
		t.Fatalf("fallback: %+v", out)
	}
}

func TestMaxStepsWithoutTerminalTool(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("u1", toolUpdateProfile, map[string]any{"notes": "a"})),
		calls(call("u2", toolUpdateProfile, map[string]any{"notes": "b"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}}, 2, 4)
	if _, err := r.RunTurn(context.Background(), newState(), TurnInput{Kind: "system"}); !errors.Is(err, ErrMaxSteps) {
		t.Fatalf("expected ErrMaxSteps, got %v", err)
	}
}

func TestFindStoresRequiresLocation(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("s1", toolFindStores, map[string]any{})),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿En qué colonia estás?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}}, 4, 4)
	st := newState()
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "system"}); err != nil {
		t.Fatal(err)
	}
	if msg := toolMessage(st, "s1"); !strings.Contains(msg, "ubicación desconocida") {
		t.Fatalf("expected location error, got %s", msg)
	}
	if !strings.Contains(r.systemPrompt(st, r.Now()), "DESCONOCIDA") {
		t.Errorf("system prompt should flag unknown location")
	}
}

func TestQuestionCapNudgesToFinish(t *testing.T) {
	r := NewRunner(&scripted{}, Deps{Shopping: shopping.Fake{}}, 4, 2)
	st := newState()
	for i := 0; i < 2; i++ {
		st.Transcript = append(st.Transcript, session.Turn{Role: "assistant", InputMode: "voice", Text: "?"})
	}
	if !strings.Contains(r.systemPrompt(st, r.Now()), "LÍMITE ALCANZADO") {
		t.Fatal("expected finish nudge after the question cap")
	}
}

// slowShopping delays searches and counts them, to prove they run concurrently.
type slowShopping struct {
	shopping.Fake
	delay time.Duration
	mu    sync.Mutex
	calls int
}

func (s *slowShopping) SearchProducts(ctx context.Context, q shopping.ProductQuery) ([]shopping.Product, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	time.Sleep(s.delay)
	return s.Fake.SearchProducts(ctx, q)
}

func TestSearchesRunInParallelAndAreCapped(t *testing.T) {
	shop := &slowShopping{delay: 80 * time.Millisecond}
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(
			call("s1", toolSearchProducts, map[string]any{"query": "camisa hombre"}),
			call("s2", toolSearchProducts, map[string]any{"query": "pantalón hombre"}),
			call("s3", toolSearchProducts, map[string]any{"query": "zapatos hombre"}),
			call("s4", toolSearchProducts, map[string]any{"query": "cinturón hombre"}),
		),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Listo?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shop, MaxSearchesPerTurn: 3}, 4, 4)
	st := newState()

	start := time.Now()
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "voice", Text: "busca todo"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("searches should run concurrently, took %v", elapsed)
	}
	if shop.calls != 3 {
		t.Fatalf("cap of 3 searches not enforced, provider called %d times", shop.calls)
	}
	refused := 0
	for _, id := range []string{"s1", "s2", "s3", "s4"} {
		if strings.Contains(toolMessage(st, id), "límite de 3") {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("exactly one of four concurrent searches should be refused, got %d", refused)
	}
	// tool results are appended in call order right after the assistant message
	var order []string
	for _, m := range st.Messages {
		if m.Role == llm.RoleTool {
			order = append(order, m.ToolCallID)
		}
	}
	if strings.Join(order, ",") != "s1,s2,s3,s4,a1" {
		t.Fatalf("tool results out of order: %v", order)
	}
}

func TestSearchRefusedNearDeadline(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("s1", toolSearchProducts, map[string]any{"query": "camisa hombre"})),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Seguimos?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, MinSearchTime: time.Minute}, 4, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st := newState()
	if _, err := r.RunTurn(ctx, st, TurnInput{Kind: "voice", Text: "busca"}); err != nil {
		t.Fatal(err)
	}
	if msg := toolMessage(st, "s1"); !strings.Contains(msg, "no queda tiempo") {
		t.Fatalf("search near the deadline should be refused, got %s", msg)
	}
}
