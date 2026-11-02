package agent

import (
	"context"
	"testing"
	"time"

	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/ui"
	"stylerag/internal/vision"
)

func askTurn(t *testing.T, st *session.State, in TurnInput, args map[string]any) *TurnOutput {
	t.Helper()
	model := &scripted{responses: []*llm.CompletionResponse{calls(call("a1", toolAskUser, args))}}
	out, err := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 6).RunTurn(context.Background(), st, in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGuidedQuestionsOpenWithThePaletteThenOccasionThenBudget(t *testing.T) {
	st := newState()
	st.Analysis = &vision.OutfitAnalysis{ColorAnalysis: &vision.ColorAnalysis{EstimatedSeason: "Deep Autumn", Confidence: "medium"}}

	// Turn 1: the model only asks; the server adds his palette and the occasion chips.
	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{"message": "¿Para qué ocasión quieres verte bien?", "input_mode": "voice"})
	types := []string{}
	for _, c := range out.UI {
		types = append(types, c.Type)
	}
	if len(out.UI) != 4 || out.UI[0].Type != ui.TypeText || out.UI[0].Text != out.Message || out.UI[1].Role != ui.RoleHeading ||
		out.UI[2].Type != ui.TypePalette || out.UI[3].Type != ui.TypeChips || out.UI[3].ID != occasionChipsID {
		t.Fatalf("first turn should be question, heading, palette, occasion chips; got %v", types)
	}
	if out.UI[1].Text != "Tu colorimetría: Otoño profundo" || len(out.UI[2].Swatches) < 5 || len(out.UI[2].Avoid) < 2 || out.UI[2].Note == "" {
		t.Fatalf("palette: %+v / %+v", out.UI[1], out.UI[2])
	}
	if out.InputMode != "buttons" || len(out.Options) != len(occasionChips) {
		t.Fatalf("occasion chips should be the turn's buttons: %+v", out)
	}

	// Tapping a chip records the occasion without the model.
	if !OccasionFromChip(st, "boda_dia") || st.Profile.Occasion != "Boda de día" {
		t.Fatalf("occasion not recorded: %q", st.Profile.Occasion)
	}

	// Turn 2: budget slider, and no palette again.
	out = askTurn(t, st, TurnInput{Kind: "button", Text: "Boda de día"}, map[string]any{"message": "¿Cuánto quieres invertir?", "input_mode": "voice"})
	if out.InputMode != "ui" || len(out.UI) != 2 || out.UI[1].Type != ui.TypeSlider || out.UI[1].ProfileField != ui.FieldBudgetMXN {
		t.Fatalf("second turn should be the budget slider: %+v", out.UI)
	}

	// With both known nothing is added.
	st.Profile.BudgetMXN = 3500
	out = askTurn(t, st, TurnInput{Kind: "ui", Text: "Presupuesto total: 3500 MXN"}, map[string]any{"message": "¿Algo que no te guste usar?", "input_mode": "voice"})
	if len(out.UI) != 0 || out.InputMode != "voice" {
		t.Fatalf("no guided controls once occasion and budget are known: %+v", out)
	}
}

func TestGuidedQuestionsRespectTheModelAndTheFollowUps(t *testing.T) {
	st := newState()
	st.Analysis = &vision.OutfitAnalysis{ColorAnalysis: &vision.ColorAnalysis{EstimatedSeason: "Soft Summer"}}
	modelChips := map[string]any{"type": "chips", "id": "ocasion", "options": []map[string]string{{"id": "oficina", "label": "Oficina"}, {"id": "cita", "label": "Cita"}}}
	modelPalette := map[string]any{"type": "palette", "swatches": []map[string]string{{"name": "Vino", "hex": "#5A1626"}}}
	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{"message": "¿Para qué ocasión?", "input_mode": "voice", "ui": []any{modelPalette, modelChips}})
	c := ui.FindChips(out.UI)
	if c == nil || len(c.Options) != 2 || c.Options[0].Label != "Oficina" || ui.FindSlider(out.UI) != nil {
		t.Fatalf("the model's own chips stay: %+v", out.UI)
	}
	if pal := findType(out.UI, ui.TypePalette); pal == nil || pal.Swatches[0].Name == "Vino" || out.UI[1].Text != "Tu colorimetría: Verano suave" {
		t.Fatalf("the server's palette replaces the model's on the first turn: %+v", out.UI)
	}
	if out.UI[len(out.UI)-1].Type != ui.TypeChips {
		t.Errorf("the control goes last: %+v", out.UI)
	}
	// While the occasion is unknown the model's chips count as occasion chips.
	if !OccasionFromChip(st, "oficina") || st.Profile.Occasion != "Oficina" {
		t.Fatalf("the model's occasion chip must set the occasion: %q", st.Profile.Occasion)
	}

	// After the user chose in the preview, follow-up questions keep the model's layout.
	st = newState()
	now := time.Now()
	st.Direction = &session.Direction{ChosenAt: &now}
	out = askTurn(t, st, TurnInput{Kind: "voice", Text: "hola"}, map[string]any{"message": "¿Te late?", "input_mode": "voice"})
	if len(out.UI) != 0 {
		t.Fatalf("no guided controls after the preview: %+v", out.UI)
	}
}

func TestUnknownSeasonSkipsThePalette(t *testing.T) {
	st := newState()
	st.Analysis = &vision.OutfitAnalysis{ColorAnalysis: &vision.ColorAnalysis{EstimatedSeason: "no determinable"}}
	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{"message": "¿Para qué ocasión?", "input_mode": "voice"})
	if findType(out.UI, ui.TypePalette) != nil || ui.FindChips(out.UI) == nil {
		t.Fatalf("no palette without a season, chips still there: %+v", out.UI)
	}
}

func TestGuidedControlsAlwaysMatchTheStep(t *testing.T) {
	st := newState()
	// The model asks the occasion but sends a budget slider and plain buttons.
	slider := map[string]any{"type": "slider", "id": "budget", "label": "Presupuesto", "min": 500, "max": 10000, "step": 100, "default": 2500, "unit": "MXN", "profile_field": "budget_mxn"}
	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{
		"message": "Qué buena base. ¿Para qué ocasión quieres verte bien?", "input_mode": "buttons",
		"options": []map[string]string{{"id": "dia", "label": "Día a día"}, {"id": "cita", "label": "Cita"}},
		"ui":      []any{slider},
	})
	if ui.FindSlider(out.UI) != nil {
		t.Fatalf("no budget slider while the occasion is unknown: %+v", out.UI)
	}
	c := ui.FindChips(out.UI)
	if c == nil || c.ID != occasionChipsID || len(c.Options) != 2 || c.Options[0].ID != "dia" || out.InputMode != "buttons" {
		t.Fatalf("the model's buttons should become the occasion chips: %+v / %+v", out.UI, out)
	}

	// Occasion known; the model asks something else: the slider comes with the budget heading.
	st.Profile.Occasion = "Día a día"
	out = askTurn(t, st, TurnInput{Kind: "button", Text: "Día a día"}, map[string]any{"message": "¿Te pondrías camisa en lugar de playera?", "input_mode": "voice"})
	if s := ui.FindSlider(out.UI); s == nil || s.ProfileField != ui.FieldBudgetMXN || out.InputMode != "ui" {
		t.Fatalf("budget slider expected: %+v", out.UI)
	}
	if out.UI[1].Role != ui.RoleHeading || out.UI[1].Text != "¿Cuánto quieres invertir en total?" {
		t.Fatalf("an off-topic question gets the budget heading: %+v", out.UI)
	}
}
