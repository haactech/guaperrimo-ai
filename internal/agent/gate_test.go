package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"stylerag/internal/llm"
	"stylerag/internal/shopping"
)

func fakeStores(t *testing.T) []shopping.Store {
	t.Helper()
	stores, err := shopping.Fake{}.FindStores(context.Background(), shopping.StoreQuery{Lat: 19.4194, Lng: -99.1616, RadiusM: 1500, Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	return stores
}

func TestServerAsksLocationBeforeThePreview(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, proposal())),
		calls(call("p2", toolPropose, proposal())),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 6)
	st := newState()
	st.Location = nil
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000
	ctx := context.Background()

	out, err := r.RunTurn(ctx, st, TurnInput{Kind: "text", Text: "una boda, 2000 pesos"})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputMode != "voice" || !strings.Contains(out.Message, "colonia") || st.Pending != "location" || st.Direction != nil {
		t.Fatalf("expected the server's location question, got %+v (pending=%q)", out, st.Pending)
	}
	if msg := toolMessage(st, "p1"); !strings.Contains(msg, "paused") {
		t.Fatalf("the proposal should be reported as paused: %s", msg)
	}
	if st.QuestionsAsked() != 0 {
		t.Errorf("server questions must not count toward the model's cap")
	}

	out, err = r.RunTurn(ctx, st, TurnInput{Kind: "voice", Text: "Roma Norte, Ciudad de México"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Location == nil || st.Location.Source != "conversation" {
		t.Fatalf("the answer should have been geocoded: %+v", st.Location)
	}
	if len(st.Stores) == 0 {
		t.Fatal("stores should be looked up right after geocoding")
	}
	// No store question: the user picks garments, and each one says where it is.
	if out.InputMode != "preview" || st.Direction == nil || st.Pending != "" || st.StoreAsked {
		t.Fatalf("expected the preview right after the location, got %+v", out)
	}
}

func TestLocationRefusalAssumesShipping(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, proposal())),
		calls(call("p2", toolPropose, proposal())),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 6)
	st := newState()
	st.Location = nil
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000
	ctx := context.Background()

	if _, err := r.RunTurn(ctx, st, TurnInput{Kind: "text", Text: "una boda"}); err != nil {
		t.Fatal(err)
	}
	out, err := r.RunTurn(ctx, st, TurnInput{Kind: "voice", Text: "prefiero no decirlo, cómpralo en línea"})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Profile.LocationDeclined || st.Location != nil {
		t.Fatalf("refusal should mark the location as declined: %+v", st.Profile)
	}
	if out.InputMode != "preview" || st.Direction == nil {
		t.Fatalf("the preview should proceed without a location, got %+v", out)
	}
	if !strings.Contains(r.systemPrompt(st, r.Now()), "no quiso darla") {
		t.Errorf("prompt should tell the model not to ask again")
	}
}

type noGeo struct{ shopping.Fake }

func (noGeo) Geocode(context.Context, string) (*shopping.Geo, error) {
	return nil, errors.New("place not found")
}

func TestLocationGateGivesUpAfterTwoUnresolvedAnswers(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, proposal())),
		calls(call("p2", toolPropose, proposal())),
		calls(call("p3", toolPropose, proposal())),
	}}
	r := NewRunner(model, Deps{Shopping: noGeo{}, RequirePreview: true}, 5, 6)
	st := newState()
	st.Location = nil
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000
	ctx := context.Background()

	if _, err := r.RunTurn(ctx, st, TurnInput{Kind: "text", Text: "una boda"}); err != nil {
		t.Fatal(err)
	}
	out, err := r.RunTurn(ctx, st, TurnInput{Kind: "voice", Text: "mmm por allá"})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputMode != "voice" || !strings.Contains(out.Message, "No ubiqué") || st.LocationAsks != 2 {
		t.Fatalf("expected the retry question, got %+v (asks=%d)", out, st.LocationAsks)
	}
	out, err = r.RunTurn(ctx, st, TurnInput{Kind: "voice", Text: "por el centro"})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Profile.LocationDeclined || out.InputMode != "preview" || st.Direction == nil {
		t.Fatalf("after two failed answers the flow must continue assuming delivery, got %+v", out)
	}
}

func TestDeviceLocationSkipsTheLocationQuestion(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, proposal())),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 6)
	st := newState() // device location
	st.AddStores(fakeStores(t))
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000

	out, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "text", Text: "una boda"})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputMode != "preview" || st.Pending != "" || st.LocationAsks != 0 {
		t.Fatalf("with a device location the preview comes without server questions, got %+v", out)
	}
}

func TestServerQuestionPausesEverySearchInTheBatch(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(
			call("s1", toolSearchProducts, map[string]any{"query": "camisa hombre"}),
			call("s2", toolSearchProducts, map[string]any{"query": "pantalón hombre"}),
			call("a1", toolAskUser, map[string]any{"message": "¿Te late?", "input_mode": "voice"}),
		),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}}, 5, 6)
	st := newState()
	st.Location = nil

	out, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "text", Text: "hola"})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputMode != "voice" || out.Message != locationQuestion {
		t.Fatalf("the location question should end the turn, got %+v", out)
	}
	if len(st.Products) != 0 {
		t.Errorf("no search should run while the server question is pending")
	}
	for _, id := range []string{"s1", "s2"} {
		if !strings.Contains(toolMessage(st, id), "paused") {
			t.Errorf("%s should be paused: %s", id, toolMessage(st, id))
		}
	}
	if !strings.Contains(toolMessage(st, "a1"), "ignorado") {
		t.Errorf("the model's own question must be ignored: %s", toolMessage(st, "a1"))
	}
}

func TestServerQuestionsCarryUI(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, proposal())),
		calls(call("p2", toolPropose, proposal())),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 6)
	st := newState()
	st.Location = nil
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000
	ctx := context.Background()

	out, err := r.RunTurn(ctx, st, TurnInput{Kind: "text", Text: "una boda"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.UI) != 1 || out.UI[0].Type != "text" || out.UI[0].Role != "stylist" || out.UI[0].Text != out.Message {
		t.Fatalf("location question ui: %+v", out.UI)
	}
	if last := st.Transcript[len(st.Transcript)-1]; len(last.UI) != 1 {
		t.Errorf("server question ui must be recorded: %+v", last)
	}
}

func TestUIInputIsReportedToTheModelAndSkipsPending(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Y el envío?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}}, 5, 6)
	st := newState()
	st.Pending = "location"
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "ui", Text: "Presupuesto: 2500 MXN"}); err != nil {
		t.Fatal(err)
	}
	if st.Messages[0].Content != "[ui] Presupuesto: 2500 MXN" || st.Transcript[0].InputMode != "ui" {
		t.Errorf("messages/transcript: %+v %+v", st.Messages[0], st.Transcript[0])
	}
	if st.Pending != "location" || st.Location == nil {
		t.Errorf("a slider must not consume a pending server question: pending=%q", st.Pending)
	}
}
