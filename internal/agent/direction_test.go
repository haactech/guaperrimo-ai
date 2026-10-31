package agent

import (
	"context"
	"strings"
	"testing"

	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

func proposal() map[string]any {
	idea := func(slot, garment, color, hex string) map[string]any {
		return map[string]any{
			"slot": slot, "garment": garment, "why": "te favorece",
			"colors": []map[string]string{{"name": color, "hex": hex}}, "textures": []string{"algodón"},
			"price_min_mxn": 400, "price_max_mxn": 900,
		}
	}
	return map[string]any{
		"message": "Elige las prendas que te llamen la atención",
		"brief": map[string]any{
			"summary":      "Subtono cálido: tierras y azul marino.",
			"color_season": "Otoño suave",
			"palette":      []map[string]string{{"name": "Azul marino", "hex": "#1f2a44"}, {"name": "Olivo", "hex": "verde"}},
			"avoid":        []map[string]string{{"name": "Negro puro", "hex": "#000000", "why": "endurece"}},
		},
		"ideas": []map[string]any{
			idea("upper_body", "Camisa oxford manga larga", "Azul cielo", "#9DB8D9"),
			idea("outerwear", "Saco desestructurado", "Camel", "#B5895A"),
			idea("lower_body", "Chino recto", "Beige", "#D8C7A5"),
			idea("footwear", "Mocasines", "Café", "#5A3A22"),
		},
	}
}

func TestPreviewGatesSearchUntilTheUserChooses(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		// Turn 1: tries to search first (refused), then proposes.
		calls(call("s0", toolSearchProducts, map[string]any{"query": "camisa hombre"})),
		calls(call("p1", toolPropose, proposal())),
		// Turn 2: after the pick, searching is allowed.
		calls(call("s1", toolSearchProducts, map[string]any{"query": "camisa oxford azul cielo hombre"})),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Te late?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 4)
	st := newState()
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000

	out, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "text", Text: "boda el sábado, 2000 pesos"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolMessage(st, "s0"), "propose_direction") {
		t.Fatalf("search before the preview must be refused: %s", toolMessage(st, "s0"))
	}
	if out.InputMode != "preview" || st.Phase != session.PhasePreview || st.Direction == nil {
		t.Fatalf("expected preview turn: %+v phase=%s", out, st.Phase)
	}
	d := st.Direction
	if len(d.Ideas) != 3 || d.Ideas[0].ID != "idea_1" || d.Round != 1 {
		t.Fatalf("footwear must be dropped and ids assigned: %+v", d.Ideas)
	}
	if d.Brief.Palette[0].Hex != "#1F2A44" || d.Brief.Palette[1].Hex != "" || d.Brief.Palette[1].Name != "Olivo" {
		t.Fatalf("swatches not cleaned: %+v", d.Brief.Palette)
	}
	for _, i := range d.Ideas {
		if i.Reference == nil || i.Reference.Currency != "MXN" {
			t.Fatalf("every idea needs a peso reference photo: %+v", i)
		}
		if _, ok := st.Products[i.Reference.ID]; !ok {
			t.Fatalf("reference %s must be a known product so the agent can recommend it", i.Reference.ID)
		}
	}

	_, err = r.RunTurn(context.Background(), st, TurnInput{Kind: "selection", Selected: []string{"idea_1", "idea_3", "nope"}, Feedback: "algo no tan formal"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(toolMessage(st, "s1"), "error") {
		t.Fatalf("search after the pick must run: %s", toolMessage(st, "s1"))
	}
	if strings.Join(d.Selected, ",") != "idea_1,idea_3" || strings.Join(d.Rejected, ",") != "idea_2" || d.ChosenAt == nil {
		t.Fatalf("selection: %+v", d)
	}
	var sel string
	for _, m := range st.Messages {
		if m.Role == llm.RoleUser && strings.HasPrefix(m.Content, "[selección]") {
			sel = m.Content
		}
	}
	for _, want := range []string{"Camisa oxford", "Chino recto", "No le llamaron la atención: idea_2", "algo no tan formal", "referencia p_"} {
		if !strings.Contains(sel, want) {
			t.Errorf("selection message missing %q: %s", want, sel)
		}
	}
}

func TestProposeNeedsEnoughIdeas(t *testing.T) {
	p := proposal()
	p["ideas"] = p["ideas"].([]map[string]any)[:2]
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, p)),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Algo más?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 4)
	st := newState()
	st.Profile.Occasion, st.Profile.BudgetMXN = "boda", 2000
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "text", Text: "hola"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolMessage(st, "p1"), "entre 3 y 6") || st.Direction != nil {
		t.Fatalf("two ideas must be refused: %s", toolMessage(st, "p1"))
	}
}

func TestSelectionWithoutDirectionFails(t *testing.T) {
	r := NewRunner(&scripted{}, Deps{Shopping: shopping.Fake{}}, 5, 4)
	if _, err := r.RunTurn(context.Background(), newState(), TurnInput{Kind: "selection", Selected: []string{"idea_1"}}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestProposeWaitsForOccasionAndBudget(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("p1", toolPropose, proposal())),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Cuánto quieres gastar?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 4)
	st := newState()
	st.Profile.Occasion = "boda"
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "text", Text: "una boda"}); err != nil {
		t.Fatal(err)
	}
	if msg := toolMessage(st, "p1"); !strings.Contains(msg, "presupuesto") || strings.Contains(msg, "ocasión") || st.Direction != nil {
		t.Fatalf("propose without budget must be refused naming only the budget: %s", msg)
	}
}

func TestBestReferencePrefersTheIdeaColor(t *testing.T) {
	idea := session.GarmentIdea{Colors: []session.Swatch{{Name: "Verde botella"}}, Textures: []string{"lino"}}
	ps := []shopping.Product{
		{ID: "azul", Title: "Saco azul de lino", Currency: "MXN", Thumbnail: "x"},
		{ID: "usd", Title: "Saco verde botella", Currency: "USD", Thumbnail: "x"},
		{ID: "verde", Title: "Saco Verde Botella slim", Currency: "MXN", Thumbnail: "x"},
		{ID: "verde_lino", Title: "Saco verde botella de lino", Currency: "MXN", Thumbnail: "x"},
	}
	if got := bestReference(idea, ps); got == nil || got.ID != "verde_lino" {
		t.Fatalf("got %+v, want verde_lino", got)
	}
	if got := bestReference(idea, ps[:1]); got == nil || got.ID != "azul" {
		t.Fatalf("without a color match the first peso listing is still a reference: %+v", got)
	}
	if bestReference(idea, ps[1:2]) != nil {
		t.Fatal("non-peso listings are never references")
	}
}
