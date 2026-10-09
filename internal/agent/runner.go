// Package agent runs the stylist as a tool-using LLM loop. The model decides
// what to ask and what to search; Go executes tools, validates the final
// recommendation and persists everything in the session.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

var tracer = otel.Tracer("guaperrimo/agent")

// ErrMaxSteps is returned when the model never ends the turn with a terminal tool.
var ErrMaxSteps = errors.New("agent: max steps reached without a reply to the user")

// Deps are the external capabilities the tools need.
type Deps struct {
	Shopping           shopping.Provider
	DefaultRadiusM     int
	DefaultLocation    string        // free-text origin for product search when the user's is unknown
	MaxProducts        int           // per search_products call
	MaxStores          int           // per find_nearby_stores call
	MaxSearchesPerTurn int           // search_products calls allowed in one turn
	MinSearchTime      time.Duration // refuse a search when less than this remains before the turn deadline
}

// Runner executes one conversational turn.
type Runner struct {
	LLM          llm.Provider
	Deps         Deps
	MaxSteps     int // tool-call iterations per turn
	MaxQuestions int // questions before the agent is told to finish
	Now          func() time.Time
}

// NewRunner applies defaults.
func NewRunner(p llm.Provider, deps Deps, maxSteps, maxQuestions int) *Runner {
	if deps.DefaultRadiusM <= 0 {
		deps.DefaultRadiusM = 1500
	}
	if deps.MaxProducts <= 0 {
		deps.MaxProducts = 6
	}
	if deps.MaxStores <= 0 {
		deps.MaxStores = 8
	}
	if deps.MaxSearchesPerTurn <= 0 {
		deps.MaxSearchesPerTurn = 8
	}
	if deps.MinSearchTime <= 0 {
		deps.MinSearchTime = 25 * time.Second
	}
	if maxSteps <= 0 {
		maxSteps = 10
	}
	if maxQuestions <= 0 {
		maxQuestions = 6
	}
	return &Runner{LLM: p, Deps: deps, MaxSteps: maxSteps, MaxQuestions: maxQuestions, Now: time.Now}
}

// TurnInput is what the user just did.
type TurnInput struct {
	Kind string // "system" (photo ready) | "button" | "voice" | "text"
	Text string
}

// TurnOutput is what the app must show next.
type TurnOutput struct {
	Message   string
	InputMode string // buttons | voice | none
	Options   []session.Option
	IsFinal   bool
	Steps     int
	ToolsUsed []string
}

// turnState is shared by the tools of one turn. Tools run concurrently when
// the model requests several at once, so every access to st goes through mu.
type turnState struct {
	mu             sync.Mutex
	st             *session.State
	now            time.Time
	searches       int
	finishAttempts int
}

func isTerminal(name string) bool { return name == toolAskUser || name == toolFinish }

// RunTurn appends the user input, loops over tool calls until the model ends
// the turn, and mutates st in place (messages, profile, products, result).
func (r *Runner) RunTurn(ctx context.Context, st *session.State, in TurnInput) (*TurnOutput, error) {
	ctx, span := tracer.Start(ctx, "agent.turn", trace.WithAttributes(
		attribute.String("session.id", st.ID),
		attribute.String("turn.kind", in.Kind),
	))
	defer span.End()

	now := r.Now()
	if st.Products == nil {
		st.Products = map[string]shopping.Product{}
	}

	userText := strings.TrimSpace(in.Text)
	switch in.Kind {
	case "system":
		userText = "[sistema] La foto del usuario ya fue analizada. Inicia la conversación."
	case "button":
		userText = "[botón] " + userText
	}
	st.Messages = append(st.Messages, llm.Text(llm.RoleUser, userText))
	if in.Kind != "system" {
		st.Transcript = append(st.Transcript, session.Turn{Role: "user", Text: in.Text, InputMode: in.Kind, At: now})
	}
	st.Turn++

	out := &TurnOutput{}
	ts := &turnState{st: st, now: now}

	for step := 0; step < r.MaxSteps; step++ {
		msgs := make([]llm.Message, 0, len(st.Messages)+1)
		msgs = append(msgs, llm.Text(llm.RoleSystem, r.systemPrompt(st, now)))
		msgs = append(msgs, st.Messages...)

		resp, err := r.LLM.Complete(ctx, llm.CompletionRequest{
			Messages:    msgs,
			Tools:       toolDefinitions(),
			ToolChoice:  llm.ToolChoiceRequired,
			MaxTokens:   4096,
			Temperature: llm.Float(0.4),
		})
		if err != nil {
			return nil, fmt.Errorf("agent: llm call: %w", err)
		}
		out.Steps++
		st.Messages = append(st.Messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})

		if len(resp.ToolCalls) == 0 {
			text := strings.TrimSpace(resp.Content)
			if text == "" {
				st.Messages = append(st.Messages, llm.Text(llm.RoleUser, "[sistema] Responde llamando a ask_user o finish_recommendation."))
				continue
			}
			// The model answered in plain text: treat it as an open question.
			slog.WarnContext(ctx, "agent: model replied without tool call, treating as ask_user", "session_id", st.ID)
			r.recordAsk(st, text, "voice", nil, now)
			out.Message, out.InputMode = text, "voice"
			return out, nil
		}

		// Non-terminal tools (searches, lookups) run concurrently; terminal
		// tools run afterwards, in order, and only the first one counts.
		results := make([]string, len(resp.ToolCalls))
		var wg sync.WaitGroup
		for i, call := range resp.ToolCalls {
			if isTerminal(call.Name) {
				continue
			}
			wg.Add(1)
			go func(i int, call llm.ToolCall) {
				defer wg.Done()
				results[i], _ = r.execTool(ctx, ts, call)
			}(i, call)
		}
		wg.Wait()
		ts.annotateProducts()

		var terminal *TurnOutput
		for i, call := range resp.ToolCalls {
			if !isTerminal(call.Name) {
				continue
			}
			if terminal != nil {
				results[i] = errJSON("ignorado: el turno ya terminó con la herramienta anterior")
				continue
			}
			results[i], terminal = r.execTool(ctx, ts, call)
		}

		for i, call := range resp.ToolCalls {
			st.Messages = append(st.Messages, llm.ToolResult(call.ID, call.Name, results[i]))
			out.ToolsUsed = append(out.ToolsUsed, call.Name)
			if strings.HasPrefix(results[i], `{"error"`) {
				slog.WarnContext(ctx, "agent: tool error", "session_id", st.ID, "tool", call.Name, "result", truncate(results[i], 300))
			} else {
				slog.InfoContext(ctx, "agent: tool", "session_id", st.ID, "tool", call.Name, "terminal", isTerminal(call.Name))
			}
		}
		if terminal != nil {
			terminal.Steps = out.Steps
			terminal.ToolsUsed = out.ToolsUsed
			span.SetAttributes(attribute.Int("agent.steps", terminal.Steps), attribute.Bool("agent.final", terminal.IsFinal))
			return terminal, nil
		}
	}
	return nil, ErrMaxSteps
}

// annotateProducts re-links every product to the closest matching store. It
// runs after each batch so searches that finished before the store lookup
// still get their nearby_store.
func (ts *turnState) annotateProducts() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for id, p := range ts.st.Products {
		p.NearbyStore = shopping.MatchNearbyStore(p.Store, ts.st.Stores)
		ts.st.Products[id] = p
	}
}

// --- tool arguments ---

type updateProfileArgs struct {
	Occasion      string   `json:"occasion"`
	EventDate     string   `json:"event_date"`
	StyleGoal     string   `json:"style_goal"`
	BudgetMXN     float64  `json:"budget_mxn"`
	RadiusM       int      `json:"radius_m"`
	AllowShipping *bool    `json:"allow_shipping"`
	Constraints   []string `json:"constraints"`
	PainPoints    []string `json:"pain_points"`
	Notes         string   `json:"notes"`
}

type setLocationArgs struct {
	Query string `json:"query"`
}

type findStoresArgs struct {
	Query   string `json:"query"`
	RadiusM int    `json:"radius_m"`
}

type searchProductsArgs struct {
	Query       string  `json:"query"`
	MaxPriceMXN float64 `json:"max_price_mxn"`
	Store       string  `json:"store"`
	Limit       int     `json:"limit"`
}

type askUserArgs struct {
	Message   string           `json:"message"`
	InputMode string           `json:"input_mode"`
	Options   []session.Option `json:"options"`
}

type finishArgs struct {
	Summary         string                   `json:"summary"`
	PriorityActions []session.PriorityAction `json:"priority_actions"`
	ShoppingList    []session.ShoppingItem   `json:"shopping_list"`
	Looks           []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Vibe        string `json:"vibe"`
		Pieces      []struct {
			Slot      string `json:"slot"`
			ProductID string `json:"product_id"`
		} `json:"pieces"`
	} `json:"looks"`
}

// compact views keep tool results small for the model.

type compactProduct struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Store    string  `json:"store"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	Nearby   *struct {
		Name      string `json:"name"`
		DistanceM int    `json:"distance_m"`
	} `json:"nearby_store,omitempty"`
	Delivery string `json:"delivery,omitempty"`
	HasImage bool   `json:"has_image"`
}

type compactStore struct {
	PlaceID   string  `json:"place_id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	DistanceM int     `json:"distance_m"`
	Rating    float64 `json:"rating,omitempty"`
	OpenState string  `json:"open_state,omitempty"`
}

func compactProducts(ps []shopping.Product) []compactProduct {
	out := make([]compactProduct, 0, len(ps))
	for _, p := range ps {
		c := compactProduct{ID: p.ID, Title: p.Title, Store: p.Store, Price: p.Price, Currency: p.Currency, Delivery: p.Delivery, HasImage: p.Thumbnail != ""}
		if p.NearbyStore != nil {
			c.Nearby = &struct {
				Name      string `json:"name"`
				DistanceM int    `json:"distance_m"`
			}{Name: p.NearbyStore.Name, DistanceM: p.NearbyStore.DistanceM}
		}
		out = append(out, c)
	}
	return out
}

func compactStores(ss []shopping.Store) []compactStore {
	out := make([]compactStore, 0, len(ss))
	for _, s := range ss {
		out = append(out, compactStore{PlaceID: s.PlaceID, Name: s.Name, Address: s.Address, DistanceM: s.DistanceM, Rating: s.Rating, OpenState: s.OpenState})
	}
	return out
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"encode"}`
	}
	return string(b)
}

func errJSON(msg string) string { return toJSON(map[string]string{"error": msg}) }

// timeLeft reports how long the turn may still run; a huge value when unbounded.
func timeLeft(ctx context.Context) time.Duration {
	if d, ok := ctx.Deadline(); ok {
		return time.Until(d)
	}
	return 24 * time.Hour
}

// execTool runs one tool. The string is fed back to the model; a non-nil
// TurnOutput means the turn is over. Network calls happen outside the lock.
func (r *Runner) execTool(ctx context.Context, ts *turnState, call llm.ToolCall) (string, *TurnOutput) {
	args := strings.TrimSpace(call.Arguments)
	if args == "" {
		args = "{}"
	}
	switch call.Name {
	case toolUpdateProfile:
		var a updateProfileArgs
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return errJSON("argumentos inválidos: " + err.Error()), nil
		}
		ts.mu.Lock()
		applyProfile(&ts.st.Profile, a)
		res := toJSON(map[string]any{"ok": true, "profile": ts.st.Profile})
		ts.mu.Unlock()
		return res, nil

	case toolSetLocation:
		var a setLocationArgs
		if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Query) == "" {
			return errJSON("falta query"), nil
		}
		ts.mu.Lock()
		if ts.st.Location != nil && ts.st.Location.Source == "device" {
			res := toJSON(map[string]any{"ok": true, "location": ts.st.Location, "note": "ya conocida por el dispositivo"})
			ts.mu.Unlock()
			return res, nil
		}
		ts.mu.Unlock()
		geo, err := r.Deps.Shopping.Geocode(ctx, a.Query)
		if err != nil {
			slog.WarnContext(ctx, "agent: geocode failed", "query", a.Query, "error", err)
			return errJSON("no pude ubicar ese lugar; pide al usuario colonia y ciudad más específicas"), nil
		}
		ts.mu.Lock()
		ts.st.Location = &session.Location{Lat: geo.Lat, Lng: geo.Lng, Label: geo.Label, Source: "conversation"}
		res := toJSON(map[string]any{"ok": true, "location": ts.st.Location})
		ts.mu.Unlock()
		return res, nil

	case toolFindStores:
		var a findStoresArgs
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return errJSON("argumentos inválidos: " + err.Error()), nil
		}
		ts.mu.Lock()
		loc := ts.st.Location
		profileRadius := ts.st.Profile.RadiusM
		ts.mu.Unlock()
		if loc == nil {
			return errJSON("ubicación desconocida: pregunta colonia o zona y ciudad, luego llama set_location"), nil
		}
		query := strings.TrimSpace(a.Query)
		if query == "" {
			query = "tienda de ropa para hombre"
		}
		radius := a.RadiusM
		if radius <= 0 {
			radius = profileRadius
		}
		if radius <= 0 {
			radius = r.Deps.DefaultRadiusM
		}
		stores, err := r.Deps.Shopping.FindStores(ctx, shopping.StoreQuery{Query: query, Lat: loc.Lat, Lng: loc.Lng, RadiusM: radius, Limit: r.Deps.MaxStores})
		if err != nil {
			slog.WarnContext(ctx, "agent: find stores failed", "error", err)
			return errJSON("la búsqueda de tiendas falló; continúa con productos en línea"), nil
		}
		usedRadius := radius
		if len(stores) < 3 && radius < 5000 {
			usedRadius = 5000
			more, err := r.Deps.Shopping.FindStores(ctx, shopping.StoreQuery{Query: query, Lat: loc.Lat, Lng: loc.Lng, RadiusM: usedRadius, Limit: r.Deps.MaxStores})
			if err == nil {
				stores = mergeStores(stores, more)
			}
		}
		ts.mu.Lock()
		ts.st.AddStores(stores)
		ts.mu.Unlock()
		return toJSON(map[string]any{"radius_used_m": usedRadius, "count": len(stores), "stores": compactStores(stores)}), nil

	case toolSearchProducts:
		var a searchProductsArgs
		if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Query) == "" {
			return errJSON("falta query"), nil
		}
		ts.mu.Lock()
		if ts.searches >= r.Deps.MaxSearchesPerTurn {
			ts.mu.Unlock()
			return errJSON(fmt.Sprintf("límite de %d búsquedas por turno alcanzado; usa los productos que ya tienes y llama finish_recommendation", r.Deps.MaxSearchesPerTurn)), nil
		}
		if timeLeft(ctx) < r.Deps.MinSearchTime {
			ts.mu.Unlock()
			return errJSON("no queda tiempo para más búsquedas; llama finish_recommendation con los productos que ya tienes"), nil
		}
		ts.searches++
		ts.mu.Unlock()

		query := strings.TrimSpace(a.Query)
		if s := strings.TrimSpace(a.Store); s != "" {
			query = s + " " + query
		}
		limit := a.Limit
		if limit <= 0 || limit > r.Deps.MaxProducts {
			limit = r.Deps.MaxProducts
		}
		products, err := r.Deps.Shopping.SearchProducts(ctx, shopping.ProductQuery{Query: query, MaxPrice: a.MaxPriceMXN, Location: r.Deps.DefaultLocation, Limit: limit})
		if err != nil {
			slog.WarnContext(ctx, "agent: product search failed", "query", query, "error", err)
			return errJSON("la búsqueda falló o tardó demasiado; si ya tienes productos para esta prenda úsalos, si no intenta una sola vez con otra descripción"), nil
		}
		ts.mu.Lock()
		shopping.AnnotateNearby(products, ts.st.Stores)
		ts.st.AddProducts(products)
		ts.mu.Unlock()
		slog.InfoContext(ctx, "agent: products found", "session_id", ts.st.ID, "query", query, "max_price", a.MaxPriceMXN, "count", len(products))
		if len(products) == 0 {
			return toJSON(map[string]any{"count": 0, "products": []any{}, "hint": "sin resultados: simplifica la búsqueda (quita store, color o corte) y reintenta una vez; si sigue vacía, incluye el artículo sin product_ids"}), nil
		}
		return toJSON(map[string]any{"count": len(products), "products": compactProducts(products)}), nil

	case toolAskUser:
		var a askUserArgs
		if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Message) == "" {
			return errJSON("falta message"), nil
		}
		mode := a.InputMode
		opts := a.Options
		if mode != "buttons" || len(opts) < 2 {
			mode, opts = "voice", nil
		}
		for i := range opts {
			if strings.TrimSpace(opts[i].ID) == "" {
				opts[i].ID = fmt.Sprintf("opt_%d", i+1)
			}
		}
		ts.mu.Lock()
		r.recordAsk(ts.st, strings.TrimSpace(a.Message), mode, opts, ts.now)
		ts.mu.Unlock()
		return `{"ok":true}`, &TurnOutput{Message: strings.TrimSpace(a.Message), InputMode: mode, Options: opts}

	case toolFinish:
		var a finishArgs
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return errJSON("argumentos inválidos: " + err.Error()), nil
		}
		ts.mu.Lock()
		defer ts.mu.Unlock()
		return r.finish(ts.st, a, &ts.finishAttempts, ts.now)

	default:
		return errJSON("herramienta desconocida: " + call.Name), nil
	}
}

func (r *Runner) finish(st *session.State, a finishArgs, attempts *int, now time.Time) (string, *TurnOutput) {
	summary := strings.TrimSpace(a.Summary)
	if summary == "" {
		return errJSON("falta summary"), nil
	}
	if len(a.ShoppingList) == 0 {
		return errJSON("shopping_list vacía: busca productos con search_products y vuelve a intentar"), nil
	}

	// Drop empty ids (the model uses them when a search found nothing) and
	// reject ids that never came out of search_products.
	var unknown []string
	clean := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := st.Products[id]; !ok {
				unknown = append(unknown, id)
				continue
			}
			out = append(out, id)
		}
		return out
	}
	for i := range a.ShoppingList {
		a.ShoppingList[i].ProductIDs = clean(a.ShoppingList[i].ProductIDs)
	}
	for i := range a.PriorityActions {
		a.PriorityActions[i].ProductIDs = clean(a.PriorityActions[i].ProductIDs)
	}
	for li := range a.Looks {
		kept := a.Looks[li].Pieces[:0]
		for _, p := range a.Looks[li].Pieces {
			if ids := clean([]string{p.ProductID}); len(ids) == 1 {
				kept = append(kept, p)
			}
		}
		a.Looks[li].Pieces = kept
	}
	if len(unknown) > 0 {
		return errJSON("product_ids desconocidos: " + strings.Join(unknown, ", ") + ". Usa solo ids tal como los devolvió search_products. Si el usuario ya tiene la prenda, no la pongas en shopping_list; si no encontraste producto, deja product_ids vacío y di en qué tienda cercana buscarlo."), nil
	}

	total := 0.0
	withProducts := 0
	for _, item := range a.ShoppingList {
		if len(item.ProductIDs) > 0 {
			withProducts++
			total += st.Products[item.ProductIDs[0]].Price
		}
	}
	if withProducts == 0 && len(st.Products) > 0 {
		return errJSON("ningún artículo tiene product_ids aunque search_products devolvió productos; asigna los ids que sirvan antes de terminar"), nil
	}
	if b := st.Profile.BudgetMXN; b > 0 && total > b*1.1 && *attempts == 0 {
		*attempts++
		return errJSON(fmt.Sprintf("la lista suma $%.0f MXN y el presupuesto es $%.0f MXN; busca alternativas más baratas o quita artículos", total, b)), nil
	}

	rec := &session.Recommendation{
		Summary:         summary,
		PriorityActions: a.PriorityActions,
		ShoppingList:    a.ShoppingList,
		TotalMXN:        total,
		CreatedAt:       now,
	}
	for i := range rec.PriorityActions {
		if rec.PriorityActions[i].ID == "" {
			rec.PriorityActions[i].ID = fmt.Sprintf("action_%d", i+1)
		}
	}

	var looks []session.Look
	for i, l := range a.Looks {
		var pieces []session.LookPiece
		for _, p := range l.Pieces {
			prod := st.Products[p.ProductID]
			pieces = append(pieces, session.LookPiece{Slot: p.Slot, Description: prod.Title, ProductID: p.ProductID})
		}
		if len(pieces) == 0 {
			continue
		}
		looks = append(looks, session.Look{
			ID:          fmt.Sprintf("look_%d", i+1),
			Name:        strings.TrimSpace(l.Name),
			Description: strings.TrimSpace(l.Description),
			Vibe:        strings.TrimSpace(l.Vibe),
			Pieces:      pieces,
		})
	}

	st.Recommendation = rec
	st.Looks = looks
	st.Phase = session.PhaseDone
	st.Transcript = append(st.Transcript, session.Turn{Role: "assistant", Text: summary, InputMode: "none", At: now})
	return `{"ok":true}`, &TurnOutput{Message: summary, InputMode: "none", IsFinal: true}
}

func (r *Runner) recordAsk(st *session.State, message, mode string, opts []session.Option, now time.Time) {
	st.Phase = session.PhaseChat
	st.Transcript = append(st.Transcript, session.Turn{Role: "assistant", Text: message, InputMode: mode, Options: opts, At: now})
}

func applyProfile(p *session.Profile, a updateProfileArgs) {
	if s := strings.TrimSpace(a.Occasion); s != "" {
		p.Occasion = s
	}
	if s := strings.TrimSpace(a.EventDate); s != "" {
		p.EventDate = s
	}
	if s := strings.TrimSpace(a.StyleGoal); s != "" {
		p.StyleGoal = s
	}
	if a.BudgetMXN > 0 {
		p.BudgetMXN = a.BudgetMXN
	}
	if a.RadiusM > 0 {
		p.RadiusM = a.RadiusM
	}
	if a.AllowShipping != nil {
		p.AllowShipping = a.AllowShipping
	}
	if len(a.Constraints) > 0 {
		p.Constraints = appendUnique(p.Constraints, a.Constraints)
	}
	if len(a.PainPoints) > 0 {
		p.PainPoints = appendUnique(p.PainPoints, a.PainPoints)
	}
	if s := strings.TrimSpace(a.Notes); s != "" {
		if p.Notes == "" {
			p.Notes = s
		} else if !strings.Contains(p.Notes, s) {
			p.Notes += " | " + s
		}
	}
}

func appendUnique(dst, src []string) []string {
	seen := map[string]bool{}
	for _, s := range dst {
		seen[strings.ToLower(s)] = true
	}
	for _, s := range src {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		dst = append(dst, s)
		seen[strings.ToLower(s)] = true
	}
	return dst
}

func mergeStores(a, b []shopping.Store) []shopping.Store {
	seen := map[string]bool{}
	out := make([]shopping.Store, 0, len(a)+len(b))
	for _, s := range a {
		seen[s.PlaceID] = true
		out = append(out, s)
	}
	for _, s := range b {
		if !seen[s.PlaceID] {
			out = append(out, s)
			seen[s.PlaceID] = true
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
