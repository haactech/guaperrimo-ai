package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/ui"
)

// Server-authored questions.
//
// The model is not trusted to ask where the user is before it needs it: with
// the style preview in the flow it jumps to the proposal as soon as it knows
// the occasion and the budget. So the runner asks on its own, right before the
// first tool that needs the location (a product search or the preview), and
// resolves the answer itself: it geocodes the place, looks up nearby stores and
// then offers the closest ones as buttons so searches can be steered to the
// store the user prefers. The app can skip the first question entirely by
// sending the device location with any turn.

const (
	maxLocationAsks = 2

	locationQuestion      = "Para buscar tiendas que te queden cerca, ¿en qué colonia o zona y ciudad estás?"
	locationRetryQuestion = "No ubiqué ese lugar. Dime la colonia y la ciudad, por ejemplo «Roma Norte, Ciudad de México». Si prefieres comprar en línea, dímelo."
	storeQuestion         = "Tengo estas tiendas cerca de ti. ¿A cuál te queda mejor ir?"
	anyStoreOptionID      = "any_store"
	storeOptionCount      = 3
)

// refusalHints are phrases that mean the user would rather not share where he
// is, or prefers to shop online; either way the agent assumes delivery.
var refusalHints = []string{
	"no quiero", "prefiero no", "no te voy a decir", "no lo voy a decir", "no te digo",
	"mejor no", "en línea", "en linea", "a domicilio", "no importa", "da igual",
}

func looksLikeRefusal(text string) bool {
	lower := strings.ToLower(text)
	for _, h := range refusalHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

// pauseForServerQuestion is called by tools that need the location (holding
// ts.mu). When the server has something to ask, it records the question as the
// turn's output and returns the JSON the model sees instead of the tool result;
// "" means the tool may proceed.
func (r *Runner) pauseForServerQuestion(ts *turnState) string {
	if ts.serverAsk == nil {
		ts.serverAsk = r.serverQuestion(ts.st, ts.now)
	}
	if ts.serverAsk == nil {
		return ""
	}
	what := "su ubicación"
	if ts.st.Pending == "store" {
		what = "a qué tienda cercana prefiere ir"
	}
	return toJSON(map[string]any{"paused": "el servidor le preguntó al usuario " + what + "; no preguntes tú, repite esta llamada en el siguiente turno"})
}

// serverQuestion returns the question the server must ask before the agent
// can search or propose, or nil when nothing is missing. Follow-ups after a
// recommendation are never gated.
func (r *Runner) serverQuestion(st *session.State, now time.Time) *TurnOutput {
	if st.Recommendation != nil {
		return nil
	}
	if st.Location == nil && !st.Profile.LocationDeclined {
		if st.LocationAsks >= maxLocationAsks {
			st.Profile.LocationDeclined = true
			return nil
		}
		st.LocationAsks++
		msg := locationQuestion
		if st.LocationAsks > 1 {
			msg = locationRetryQuestion
		}
		st.Pending = "location"
		comps := []ui.Component{ui.Text(ui.RoleStylist, msg)}
		r.recordServerAsk(st, msg, "voice", nil, comps, now)
		return &TurnOutput{Message: msg, InputMode: "voice", UI: comps}
	}
	if st.Location != nil && !st.StoreAsked && st.Profile.PreferredStore == nil && len(st.Stores) >= 2 {
		st.StoreAsked = true
		st.Pending = "store"
		opts := storeOptions(st.Stores, storeOptionCount)
		chips := make([]ui.Option, 0, len(opts))
		for _, o := range opts {
			chips = append(chips, ui.Option{ID: o.ID, Label: o.Label})
		}
		comps := []ui.Component{ui.Text(ui.RoleStylist, storeQuestion), ui.Chips("store", true, chips)}
		r.recordServerAsk(st, storeQuestion, "buttons", opts, comps, now)
		return &TurnOutput{Message: storeQuestion, InputMode: "buttons", Options: opts, UI: comps}
	}
	return nil
}

// recordServerAsk stores a server question so the app shows it and the model
// remembers it, without counting it as one of the model's questions.
func (r *Runner) recordServerAsk(st *session.State, message, mode string, opts []session.Option, comps []ui.Component, now time.Time) {
	st.Phase = session.PhaseChat
	st.Transcript = append(st.Transcript, session.Turn{Role: "assistant", Text: message, InputMode: mode, Options: opts, UI: comps, Server: true, At: now})
	st.Messages = append(st.Messages, llm.Text(llm.RoleAssistant, "[servidor] "+message))
}

// storeOptions offers the n closest stores plus "any of them".
func storeOptions(stores []shopping.Store, n int) []session.Option {
	sorted := make([]shopping.Store, len(stores))
	copy(sorted, stores)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].DistanceM < sorted[j].DistanceM })
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	opts := make([]session.Option, 0, len(sorted)+1)
	for _, s := range sorted {
		opts = append(opts, session.Option{ID: s.PlaceID, Label: fmt.Sprintf("%s · %s", s.Name, distanceLabel(s.DistanceM))})
	}
	return append(opts, session.Option{ID: anyStoreOptionID, Label: "Cualquiera"})
}

func distanceLabel(m int) string {
	if m < 1000 {
		return fmt.Sprintf("%d m", m)
	}
	return fmt.Sprintf("%.1f km", float64(m)/1000)
}

// brand is the chain name of a store ("Liverpool Insurgentes" → "Liverpool"),
// good enough to steer a product search.
func brand(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return name
	}
	return fields[0]
}

// resolvePending consumes the user's answer to a server question and returns
// a note for the model ("" when there was nothing pending). The location is
// geocoded and stores are looked up here, so the agent never has to.
func (r *Runner) resolvePending(ctx context.Context, st *session.State, in TurnInput) string {
	pending := st.Pending
	if pending == "" || in.Kind == "system" || in.Kind == "selection" || in.Kind == "ui" {
		return ""
	}
	st.Pending = ""
	text := strings.TrimSpace(in.Text)
	switch pending {
	case "location":
		if looksLikeRefusal(text) {
			st.Profile.LocationDeclined = true
			return "El usuario no quiere dar su ubicación. No la preguntes; asume compras en línea con envío a domicilio."
		}
		geo, err := r.Deps.Shopping.Geocode(ctx, text)
		if err != nil {
			slog.WarnContext(ctx, "agent: could not geocode the user's answer", "session_id", st.ID, "text", text, "error", err)
			return fmt.Sprintf("No se pudo ubicar «%s». El servidor volverá a preguntar; tú no preguntes la ubicación.", text)
		}
		st.Location = &session.Location{Lat: geo.Lat, Lng: geo.Lng, Label: geo.Label, Source: "conversation"}
		st.AddStores(r.NearbyStores(ctx, st))
		return fmt.Sprintf("Ubicación registrada: %s (%d tiendas cercanas encontradas). No la vuelvas a preguntar.", geo.Label, len(st.Stores))
	case "store":
		return resolveStoreChoice(st, text)
	}
	return ""
}

// resolveStoreChoice matches the answer (a button label or id, or a spoken
// store name) against the offered stores.
func resolveStoreChoice(st *session.State, text string) string {
	id := ""
	for _, o := range st.LastAssistantOptions() {
		if o.ID == text || strings.EqualFold(o.Label, text) {
			id = o.ID
			break
		}
	}
	if id == "" {
		lower := strings.ToLower(text)
		for _, s := range st.Stores {
			if b := strings.ToLower(brand(s.Name)); len(b) >= 2 && strings.Contains(lower, b) {
				id = s.PlaceID
				break
			}
		}
	}
	for _, s := range st.Stores {
		if id != "" && id != anyStoreOptionID && s.PlaceID == id {
			st.Profile.PreferredStore = &shopping.StoreRef{PlaceID: s.PlaceID, Name: s.Name, DistanceM: s.DistanceM}
			return fmt.Sprintf("Tienda preferida: %s (%d m). Dirige search_products con store=%q primero y prioriza productos con nearby_store ahí.", s.Name, s.DistanceM, brand(s.Name))
		}
	}
	return "El usuario no eligió una tienda en particular; usa las tiendas cercanas encontradas."
}
