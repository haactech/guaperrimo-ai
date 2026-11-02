package agent

import (
	"fmt"
	"strings"

	"stylerag/internal/session"
	"stylerag/internal/ui"
	"stylerag/internal/vision"
)

// Guided questions.
//
// Before the style preview the stylist only needs the occasion and the
// budget. The user should tap, not talk: the server attaches the controls to
// the model's question on its own instead of trusting the model to send them,
// and opens the session with what the photo already told us, his colour
// season and palette, so the first screen shows something useful instead of
// only his photo.

const (
	occasionChipsID = "occasion"
	budgetSliderID  = "budget"
)

var occasionChips = []ui.Option{
	{ID: "boda_dia", Label: "Boda de día"},
	{ID: "boda_noche", Label: "Boda de noche"},
	{ID: "entrevista", Label: "Entrevista de trabajo"},
	{ID: "cita", Label: "Una cita"},
	{ID: "trabajo", Label: "Junta o evento de trabajo"},
	{ID: "diario", Label: "Verme mejor a diario"},
}

func budgetSlider() ui.Component {
	return ui.Slider(budgetSliderID, "Presupuesto total", 500, 10000, 250, 2500, "MXN", ui.FieldBudgetMXN)
}

// withGuidedUI makes the server, not the model, decide the control of the
// turns before the preview: the colour palette from the photo analysis on
// the first question (replacing any the model sent), occasion chips while the
// occasion is unknown, then the budget slider. A control for something else
// is dropped, and when the model's question is about something else the
// server adds the right heading above the control, so question and control
// always match.
func withGuidedUI(st *session.State, message string, opts []session.Option, comps []ui.Component) []ui.Component {
	if st.Recommendation != nil || (st.Direction != nil && st.Direction.ChosenAt != nil) {
		return comps // follow-ups after the preview keep the model's layout
	}
	occasionUnknown := strings.TrimSpace(st.Profile.Occasion) == ""
	budgetUnknown := st.Profile.BudgetMXN <= 0
	var head []ui.Component
	if firstAssistantTurn(st) {
		// The palette comes from the photo analysis, not from the model: it is
		// the same every time and explains his season.
		if pal := paletteComponents(st); len(pal) > 0 {
			comps = withoutType(comps, ui.TypePalette)
			head = append(head, pal...)
		}
	}
	var control *ui.Component
	switch {
	case occasionUnknown:
		comps = withoutType(comps, ui.TypeSlider)
		if c := ui.FindChips(comps); c != nil {
			c.ID = occasionChipsID // the model's own occasion chips also set the occasion
		} else {
			chips := ui.Chips(occasionChipsID, true, occasionChips)
			if len(opts) >= 2 { // the model's buttons become the occasion chips
				chips.Options = nil
				for _, o := range opts {
					chips.Options = append(chips.Options, ui.Option{ID: o.ID, Label: o.Label})
				}
			}
			control = &chips
		}
		if !mentions(message, "ocasión", "ocasion", "para qué", "para que", "evento", "momento") {
			head = append(head, ui.Text(ui.RoleHeading, "¿Para qué ocasión?"))
		}
	case budgetUnknown:
		comps = withoutType(comps, ui.TypeChips)
		if c := ui.FindSlider(comps); c == nil || c.ProfileField != ui.FieldBudgetMXN {
			comps = withoutType(comps, ui.TypeSlider)
			slider := budgetSlider()
			control = &slider
		}
		if !mentions(message, "presupuesto", "invertir", "gastar", "pesos", "cuánto", "cuanto") {
			head = append(head, ui.Text(ui.RoleHeading, "¿Cuánto quieres invertir en total?"))
		}
	}
	if len(head) == 0 && control == nil {
		return comps
	}
	// The canvas replaces the plain message on screen, so it must carry the
	// question itself. Order: question, palette and heading, the model's
	// other components, then the control.
	if findType(comps, ui.TypeText) == nil {
		comps = append([]ui.Component{ui.Text(ui.RoleStylist, message)}, comps...)
	}
	at := 0
	for i, c := range comps {
		if c.Type == ui.TypeText {
			at = i + 1
			break
		}
	}
	out := append(append(append([]ui.Component{}, comps[:at]...), head...), comps[at:]...)
	// Model controls go last too.
	var rest, controls []ui.Component
	for _, c := range out {
		if c.Type == ui.TypeChips || c.Type == ui.TypeSlider {
			controls = append(controls, c)
		} else {
			rest = append(rest, c)
		}
	}
	if control != nil {
		controls = append(controls, *control)
	}
	return append(rest, controls...)
}

func mentions(text string, words ...string) bool {
	lower := strings.ToLower(text)
	for _, w := range words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func withoutType(comps []ui.Component, typ string) []ui.Component {
	out := comps[:0:0]
	for _, c := range comps {
		if c.Type != typ {
			out = append(out, c)
		}
	}
	return out
}

// paletteComponents shows his colour season: a heading and the palette with
// what it means. Nothing when the analysis has no usable season.
func paletteComponents(st *session.State) []ui.Component {
	if st.Analysis == nil || st.Analysis.ColorAnalysis == nil {
		return nil
	}
	p, ok := vision.PaletteFor(st.Analysis.ColorAnalysis.EstimatedSeason)
	if !ok {
		return nil
	}
	note := p.Why
	if strings.EqualFold(st.Analysis.ColorAnalysis.Confidence, "low") {
		note += " Es una estimación: con mejor luz la afinamos."
	}
	return []ui.Component{
		ui.Text(ui.RoleHeading, fmt.Sprintf("Tu colorimetría: %s", p.Name)),
		ui.Palette(swatches(p.Best), swatches(p.Avoid), note),
	}
}

func swatches(in []vision.Swatch) []ui.Swatch {
	out := make([]ui.Swatch, 0, len(in))
	for _, s := range in {
		out = append(out, ui.Swatch{Name: s.Name, Hex: s.Hex})
	}
	return out
}

func firstAssistantTurn(st *session.State) bool {
	for _, t := range st.Transcript {
		if t.Role == "assistant" {
			return false
		}
	}
	return true
}

func findType(comps []ui.Component, typ string) *ui.Component {
	for i := range comps {
		if comps[i].Type == typ {
			return &comps[i]
		}
	}
	return nil
}

// OccasionFromChip records the occasion when the user taps one of the guided
// occasion chips, so it never depends on the model calling update_profile.
func OccasionFromChip(st *session.State, optionID string) bool {
	c := ui.FindByID(st.LastAssistantUI(), occasionChipsID)
	if c == nil || c.Type != ui.TypeChips {
		return false
	}
	for _, o := range c.Options {
		if o.ID == optionID {
			st.Profile.Occasion = o.Label
			return true
		}
	}
	return false
}

// withQuestionText puts the spoken question on screen: the app shows only the
// components when a turn has any, so a lone slider or chips would otherwise
// appear without the question they answer.
func withQuestionText(message string, comps []ui.Component) []ui.Component {
	if len(comps) == 0 || findType(comps, ui.TypeText) != nil {
		return comps
	}
	return append([]ui.Component{ui.Text(ui.RoleStylist, message)}, comps...)
}
