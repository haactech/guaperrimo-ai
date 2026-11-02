package agent

// Live eval of the first questions with the real model and the production
// prompt. It catches what unit tests with a scripted model cannot: what the
// model actually asks, and whether the screen the server builds around that
// question still makes sense (the occasion question once came with only the
// budget slider and no way to answer by voice).
//
//	AGENT_EVAL=1 go test ./internal/agent -run TestEvalFirstQuestions -v
//	AGENT_EVAL_RUNS=5 … (default 3)
//
// Uses the LLM from .env (Mistral); products are fake, so it spends no
// SerpAPI searches and renders no images.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"stylerag/internal/config"
	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/ui"
	"stylerag/internal/vision"
)

// evalCheck is one property of a turn. Hard checks are guaranteed by the
// server and must always pass; soft checks depend on the model and must pass
// in most runs.
type evalCheck struct {
	name string
	hard bool
	ok   func(st *session.State, out *TurnOutput) bool
}

type evalScenario struct {
	name   string
	setup  func(t *testing.T, r *Runner, st *session.State) // turns before the one under test
	input  func(st *session.State) TurnInput
	checks []evalCheck
}

func hasOccasionChips(out *TurnOutput) bool {
	c := ui.FindChips(out.UI)
	return c != nil && c.ID == occasionChipsID && len(c.Options) >= 2
}

func hasBudgetSlider(out *TurnOutput) bool {
	s := ui.FindSlider(out.UI)
	return s != nil && s.ProfileField == ui.FieldBudgetMXN
}

func asksOccasion(msg string) bool {
	return mentions(msg, "ocasión", "ocasion", "para qué", "para que", "evento", "momento")
}

func asksBudget(msg string) bool {
	return mentions(msg, "presupuesto", "invertir", "gastar", "pesos", "cuánto", "cuanto")
}

func hasHeading(out *TurnOutput, text string) bool {
	for _, c := range out.UI {
		if c.Type == ui.TypeText && c.Role == ui.RoleHeading && c.Text == text {
			return true
		}
	}
	return false
}

// Shared invariants of a guided question: what the user sees can always be
// answered, and every control matches the question or carries its heading.
var guidedInvariants = onQuestions([]evalCheck{
	{"la pregunta va primero en pantalla", true, func(_ *session.State, o *TurnOutput) bool {
		return len(o.UI) > 0 && o.UI[0].Type == ui.TypeText && o.UI[0].Role == ui.RoleStylist
	}},
	{"slider de presupuesto con su pregunta", true, func(_ *session.State, o *TurnOutput) bool {
		return !hasBudgetSlider(o) || asksBudget(o.Message) || hasHeading(o, "¿Cuánto quieres invertir en total?")
	}},
	{"botones de ocasión con su pregunta", true, func(_ *session.State, o *TurnOutput) bool {
		return !hasOccasionChips(o) || asksOccasion(o.Message) || hasHeading(o, "¿Para qué ocasión?")
	}},
	{"si pregunta la ocasión, hay botones de ocasión", true, func(_ *session.State, o *TurnOutput) bool {
		return !asksOccasion(o.Message) || hasOccasionChips(o)
	}},
	// The app shows the mic on every question turn with controls; chips must
	// also accept a spoken answer.
	{"los botones aceptan respuesta por voz", true, func(_ *session.State, o *TurnOutput) bool {
		c := ui.FindChips(o.UI)
		return c == nil || c.AllowVoice
	}},
	{"sin productos ni precios en la pregunta", true, func(_ *session.State, o *TurnOutput) bool {
		return findType(o.UI, ui.TypeProductCard) == nil && findType(o.UI, ui.TypeBudgetBar) == nil
	}},
})

// onQuestions makes checks apply only to question turns: the preview and the
// final recommendation have their own screens.
func onQuestions(checks []evalCheck) []evalCheck {
	out := make([]evalCheck, len(checks))
	for i, c := range checks {
		ok := c.ok
		out[i] = evalCheck{c.name, c.hard, func(st *session.State, o *TurnOutput) bool {
			return o.InputMode == "preview" || o.IsFinal || ok(st, o)
		}}
	}
	return out
}

func TestEvalFirstQuestions(t *testing.T) {
	if os.Getenv("AGENT_EVAL") != "1" {
		t.Skip("live eval: set AGENT_EVAL=1 (uses the real LLM from .env)")
	}
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root) // config.Load reads .env from the working directory
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM key in .env")
	}
	flavor := llm.Flavor(cfg.LLMProvider)
	if cfg.LLMProvider == "kimi" {
		flavor = llm.FlavorMoonshot
	}
	model := llm.NewOpenAICompat(llm.OpenAICompatConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, Flavor: flavor, MaxRetries: 2})
	newRunner := func() *Runner {
		return NewRunner(model, Deps{Shopping: shopping.Fake{}, DefaultRadiusM: cfg.DefaultRadiusM, MaxSearchesPerTurn: cfg.AgentMaxSearches, RequirePreview: true}, cfg.AgentMaxSteps, cfg.AgentMaxQuestions)
	}
	runs := 3
	if n, err := strconv.Atoi(os.Getenv("AGENT_EVAL_RUNS")); err == nil && n > 0 {
		runs = n
	}

	firstTurn := func(t *testing.T, r *Runner, st *session.State) {
		if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "system"}); err != nil {
			t.Fatalf("first turn: %v", err)
		}
	}
	scenarios := []evalScenario{
		{
			name:  "primera pregunta tras la foto",
			input: func(*session.State) TurnInput { return TurnInput{Kind: "system"} },
			checks: append([]evalCheck{
				{"muestra la paleta de su colorimetría", true, func(_ *session.State, o *TurnOutput) bool { return findType(o.UI, ui.TypePalette) != nil }},
				{"botones de ocasión", true, func(_ *session.State, o *TurnOutput) bool { return hasOccasionChips(o) }},
				{"slider de presupuesto en la misma pantalla", true, func(_ *session.State, o *TurnOutput) bool { return hasBudgetSlider(o) }},
				{"el modelo pregunta la ocasión", false, func(_ *session.State, o *TurnOutput) bool { return asksOccasion(o.Message) }},
				{"no se guarda una ocasión antes de que conteste", true, func(st *session.State, _ *TurnOutput) bool { return strings.TrimSpace(st.Profile.Occasion) == "" }},
			}, guidedInvariants...),
		},
		{
			name:  "eligió la ocasión con un botón",
			setup: firstTurn,
			input: func(st *session.State) TurnInput {
				OccasionFromChip(st, "boda_dia")
				return TurnInput{Kind: "button", Text: "Boda de día"}
			},
			checks: append([]evalCheck{
				{"solo queda el slider de presupuesto", true, func(_ *session.State, o *TurnOutput) bool {
					return o.InputMode == "preview" || (hasBudgetSlider(o) && !hasOccasionChips(o))
				}},
				{"el modelo pregunta el presupuesto", false, func(_ *session.State, o *TurnOutput) bool { return o.InputMode == "preview" || asksBudget(o.Message) }},
			}, guidedInvariants...),
		},
		{
			name:  "contestó ocasión y presupuesto por voz",
			setup: firstTurn,
			input: func(*session.State) TurnInput {
				return TurnInput{Kind: "voice", Text: "Es para una boda de día el sábado y quiero gastar como 3500 pesos en total"}
			},
			checks: append([]evalCheck{
				{"el modelo guarda la ocasión", false, func(st *session.State, _ *TurnOutput) bool { return strings.TrimSpace(st.Profile.Occasion) != "" }},
				{"el modelo guarda el presupuesto", false, func(st *session.State, _ *TurnOutput) bool {
					return st.Profile.BudgetMXN >= 3000 && st.Profile.BudgetMXN <= 4000
				}},
				{"no vuelve a pedir lo que ya dijo", false, func(_ *session.State, o *TurnOutput) bool {
					return o.InputMode == "preview" || (!hasOccasionChips(o) && !hasBudgetSlider(o))
				}},
			}, guidedInvariants...),
		},
	}

	failed := false
	for _, sc := range scenarios {
		passes := map[string]int{}
		var samples []string
		for run := 0; run < runs; run++ {
			r := newRunner()
			st := newState()
			st.Analysis = &vision.OutfitAnalysis{
				DetectedStyles: []string{"casual"}, DetectedColors: []string{"negro", "azul"},
				ColorAnalysis: &vision.ColorAnalysis{EstimatedSeason: "Deep Autumn", Confidence: "medium"},
			}
			if sc.setup != nil {
				sc.setup(t, r, st)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			out, err := r.RunTurn(ctx, st, sc.input(st))
			cancel()
			if err != nil {
				t.Errorf("%s run %d: %v", sc.name, run+1, err)
				continue
			}
			samples = append(samples, fmt.Sprintf("[%s] %s → %v", out.InputMode, truncateEval(out.Message, 90), uiTypesEval(out.UI)))
			for _, c := range sc.checks {
				if c.ok(st, out) {
					passes[c.name]++
				}
			}
		}
		t.Logf("── %s (%d corridas)", sc.name, runs)
		for _, s := range samples {
			t.Logf("   %s", s)
		}
		for _, c := range sc.checks {
			mark, need := "ok ", runs
			if !c.hard {
				need = (runs*2 + 2) / 3 // two thirds
			}
			if passes[c.name] < need {
				mark, failed = "MAL", true
			}
			kind := "modelo"
			if c.hard {
				kind = "servidor"
			}
			t.Logf("   %s %d/%d  %-50s (%s)", mark, passes[c.name], runs, c.name, kind)
		}
	}
	if failed {
		t.Fatal("la eval no pasó: revisa las líneas MAL")
	}
}

func uiTypesEval(comps []ui.Component) []string {
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

func truncateEval(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
