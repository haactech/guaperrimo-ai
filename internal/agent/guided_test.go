package agent

import (
	"context"
	"strings"
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

func uiTypes(comps []ui.Component) []string {
	var out []string
	for _, c := range comps {
		t := c.Type
		if c.Type == ui.TypeText && c.Role == ui.RoleHeading {
			t = "heading"
		}
		out = append(out, t)
	}
	return out
}

func TestFirstQuestionShowsPaletteOccasionAndBudgetTogether(t *testing.T) {
	st := newState()
	st.Analysis = &vision.OutfitAnalysis{ColorAnalysis: &vision.ColorAnalysis{EstimatedSeason: "Deep Autumn", Confidence: "medium"}}

	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{"message": "¿Para qué ocasión quieres verte bien?", "input_mode": "voice"})
	want := "text heading palette chips heading slider"
	if got := strings.Join(uiTypes(out.UI), " "); got != want {
		t.Fatalf("first screen = %s, want %s", got, want)
	}
	if out.UI[0].Text != out.Message || out.UI[1].Text != "Tu colorimetría: Otoño profundo" || out.UI[4].Text != "¿Cuánto quieres invertir en total?" {
		t.Fatalf("texts: %+v", out.UI)
	}
	chips := out.UI[3]
	if chips.ID != occasionChipsID || !chips.AllowVoice || len(chips.Options) != len(occasionChips) {
		t.Fatalf("occasion chips must take voice too: %+v", chips)
	}
	if s := out.UI[5]; s.ProfileField != ui.FieldBudgetMXN || out.InputMode != "ui" {
		t.Fatalf("budget slider confirms the screen: %+v mode=%s", s, out.InputMode)
	}

	// Occasion answered, budget still unknown: only the slider.
	st.Profile.Occasion = "Boda de día"
	out = askTurn(t, st, TurnInput{Kind: "button", Text: "Boda de día"}, map[string]any{"message": "¿Cuánto quieres invertir?", "input_mode": "voice"})
	if got := strings.Join(uiTypes(out.UI), " "); got != "text slider" {
		t.Fatalf("budget only: %s", got)
	}

	// Both known: nothing is added.
	st.Profile.BudgetMXN = 3500
	out = askTurn(t, st, TurnInput{Kind: "ui", Text: "Presupuesto total: 3500 MXN"}, map[string]any{"message": "¿Algo que no te guste usar?", "input_mode": "voice"})
	if len(out.UI) != 0 || out.InputMode != "voice" {
		t.Fatalf("no guided controls once occasion and budget are known: %+v", out)
	}
}

// The bug seen on the phone: the model records an occasion it guessed from
// the photo, then asks for the occasion. The occasion chips must still show.
func TestOccasionQuestionAlwaysGetsOccasionChips(t *testing.T) {
	st := newState()
	st.Profile.Occasion = "día a día" // guessed by the model, not said by the user
	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{"message": "¿Para qué ocasión quieres verte más guapo?", "input_mode": "voice"})
	c := ui.FindChips(out.UI)
	if c == nil || c.ID != occasionChipsID || !c.AllowVoice {
		t.Fatalf("an occasion question needs the occasion chips: %v", uiTypes(out.UI))
	}
	if ui.FindSlider(out.UI) == nil {
		t.Fatalf("the budget is still unknown, its slider goes on the same screen: %v", uiTypes(out.UI))
	}
}

func TestGuidedQuestionsReuseTheModelChoicesAndSkipFollowUps(t *testing.T) {
	st := newState()
	st.Analysis = &vision.OutfitAnalysis{ColorAnalysis: &vision.ColorAnalysis{EstimatedSeason: "Soft Summer"}}
	modelChips := map[string]any{"type": "chips", "id": "ocasion", "options": []map[string]string{{"id": "oficina", "label": "Oficina"}, {"id": "cita", "label": "Cita"}}}
	modelPalette := map[string]any{"type": "palette", "swatches": []map[string]string{{"name": "Vino", "hex": "#5A1626"}}}
	modelSlider := map[string]any{"type": "slider", "id": "b", "label": "Presupuesto", "min": 500, "max": 9000, "step": 100, "default": 2000, "unit": "MXN", "profile_field": "budget_mxn"}
	out := askTurn(t, st, TurnInput{Kind: "system"}, map[string]any{"message": "¿Para qué ocasión y cuánto quieres gastar?", "input_mode": "voice", "ui": []any{modelPalette, modelSlider, modelChips}})
	if got := strings.Join(uiTypes(out.UI), " "); got != "text heading palette chips slider" {
		t.Fatalf("question asks both: no extra headings, controls last: %s", got)
	}
	c := ui.FindChips(out.UI)
	if c.ID != occasionChipsID || c.Options[0].Label != "Oficina" {
		t.Fatalf("the model's choices become the occasion chips: %+v", c)
	}
	if pal := findType(out.UI, ui.TypePalette); pal.Swatches[0].Name == "Vino" || out.UI[1].Text != "Tu colorimetría: Verano suave" {
		t.Fatalf("the server's palette replaces the model's on the first turn: %+v", out.UI)
	}
	if s := ui.FindSlider(out.UI); s.Max != 9000 {
		t.Fatalf("the model's budget slider is kept: %+v", s)
	}
	if !OccasionFromChip(st, "oficina") || st.Profile.Occasion != "Oficina" {
		t.Fatalf("the occasion chip must set the occasion: %q", st.Profile.Occasion)
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

func TestProfileGuessesBeforeTheFirstAnswerAreIgnored(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("u1", toolUpdateProfile, map[string]any{"occasion": "día a día", "budget_mxn": 2000, "style_goal": "verse pulcro"}),
			call("a1", toolAskUser, map[string]any{"message": "¿Para qué ocasión?", "input_mode": "voice"})),
		calls(call("u2", toolUpdateProfile, map[string]any{"occasion": "boda de día"}),
			call("a2", toolAskUser, map[string]any{"message": "¿Cuánto quieres invertir?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shopping.Fake{}, RequirePreview: true}, 5, 6)
	st := newState()
	out, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Profile.Occasion != "" || st.Profile.BudgetMXN != 0 || st.Profile.StyleGoal != "verse pulcro" {
		t.Fatalf("guesses from the photo must not reach the profile: %+v", st.Profile)
	}
	if !strings.Contains(toolMessage(st, "u1"), "ignorado") || !hasOccasionChips(out) {
		t.Fatalf("the model is told, and the occasion chips show: %s / %v", toolMessage(st, "u1"), uiTypes(out.UI))
	}
	// Once the user answers, the model records what he said.
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "voice", Text: "una boda de día"}); err != nil {
		t.Fatal(err)
	}
	if st.Profile.Occasion != "boda de día" {
		t.Fatalf("after an answer the occasion is recorded: %+v", st.Profile)
	}
}
