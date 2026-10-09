// Package session holds the per-user conversation state. Everything in State
// is plain JSON so it can live in memory or in Postgres interchangeably.
package session

import (
	"time"

	"stylerag/internal/llm"
	"stylerag/internal/shopping"
	"stylerag/internal/vision"
)

// Phase is the coarse stage of a session.
type Phase string

const (
	PhaseCapture Phase = "capture" // waiting for the photo turn
	PhaseChat    Phase = "chat"    // agent is asking questions
	PhaseDone    Phase = "done"    // a recommendation has been delivered
)

// Location is where the user is, either from the device or from conversation.
type Location struct {
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Label  string  `json:"label,omitempty"`
	Source string  `json:"source"` // "device" | "conversation"
}

// Profile holds the facts the agent has recorded about the user.
type Profile struct {
	Occasion      string   `json:"occasion,omitempty"`
	EventDate     string   `json:"event_date,omitempty"`
	StyleGoal     string   `json:"style_goal,omitempty"`
	BudgetMXN     float64  `json:"budget_mxn,omitempty"`
	RadiusM       int      `json:"radius_m,omitempty"`
	AllowShipping *bool    `json:"allow_shipping,omitempty"`
	Constraints   []string `json:"constraints,omitempty"`
	PainPoints    []string `json:"pain_points,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// Option is a button the user can tap.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Turn is one user-visible exchange (not the raw LLM traffic).
type Turn struct {
	Role      string    `json:"role"` // "user" | "assistant"
	Text      string    `json:"text"`
	InputMode string    `json:"input_mode,omitempty"` // assistant: buttons|voice|none; user: button|voice|text
	Options   []Option  `json:"options,omitempty"`
	At        time.Time `json:"at"`
}

// PriorityAction is one concrete improvement, shown as a card in the app.
type PriorityAction struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Impact      string   `json:"impact"` // alto | medio | bajo
	Effort      string   `json:"effort"` // alto | medio | bajo
	ProductIDs  []string `json:"product_ids,omitempty"`
}

// ShoppingItem is one line of the shopping list, with real product options.
type ShoppingItem struct {
	Slot        string   `json:"slot"` // upper_body | lower_body | footwear | outerwear | accessory
	Description string   `json:"description"`
	Why         string   `json:"why,omitempty"`
	ProductIDs  []string `json:"product_ids"`
	Priority    int      `json:"priority,omitempty"`
}

// Recommendation is the agent's final deliverable.
type Recommendation struct {
	Summary         string           `json:"summary"`
	PriorityActions []PriorityAction `json:"priority_actions"`
	ShoppingList    []ShoppingItem   `json:"shopping_list"`
	TotalMXN        float64          `json:"total_mxn"`
	CreatedAt       time.Time        `json:"created_at"`
}

// LookPiece is one garment of a look, bound to a real product.
type LookPiece struct {
	Slot        string `json:"slot"` // upper_body | lower_body | footwear | outerwear
	Description string `json:"description,omitempty"`
	ProductID   string `json:"product_id"`
}

// Look is an outfit composed by the agent.
type Look struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Vibe        string      `json:"vibe,omitempty"`
	Pieces      []LookPiece `json:"pieces"`
}

// LookStatus tracks virtual try-on generation for a look.
type LookStatus string

const (
	LookStatusPending    LookStatus = "pending"
	LookStatusGenerating LookStatus = "generating"
	LookStatusReady      LookStatus = "ready"
	LookStatusFailed     LookStatus = "failed"
)

// LookPieceResult is the try-on output for one piece.
type LookPieceResult struct {
	Slot            string `json:"slot"`
	ProductID       string `json:"product_id,omitempty"`
	ProductName     string `json:"product_name,omitempty"`
	ProductImageURL string `json:"product_image_url,omitempty"`
	TryOnImageURL   string `json:"tryon_image_url,omitempty"`
	GenerationMs    int64  `json:"generation_time_ms,omitempty"`
}

// LookResult is the try-on output for a whole look.
type LookResult struct {
	LookID        string            `json:"look_id"`
	Status        LookStatus        `json:"status"`
	Pieces        []LookPieceResult `json:"pieces,omitempty"`
	FinalImageURL string            `json:"final_image_url,omitempty"`
	ErrorMessage  string            `json:"error_message,omitempty"`
	GenerationMs  int64             `json:"generation_time_ms"`
	CreatedAt     time.Time         `json:"created_at"`
}

// TryOnResult caches a single-garment try-on.
type TryOnResult struct {
	ProductID       string    `json:"product_id"`
	ActionID        string    `json:"action_id,omitempty"`
	TryOnImageURL   string    `json:"tryon_image_url"`
	GarmentName     string    `json:"garment_name"`
	GarmentImageURL string    `json:"garment_image_url"`
	GenerationMs    int64     `json:"generation_time_ms"`
	ProviderName    string    `json:"provider_name"`
	CreatedAt       time.Time `json:"created_at"`
}

// State is everything known about one session.
type State struct {
	ID       string `json:"id"`
	Phase    Phase  `json:"phase"`
	Turn     int    `json:"turn"` // number of user turns processed
	ImageKey string `json:"image_key,omitempty"`
	ImageURL string `json:"image_url,omitempty"`

	Analysis *vision.OutfitAnalysis `json:"analysis,omitempty"`
	Location *Location              `json:"location,omitempty"`
	Profile  Profile                `json:"profile"`

	Messages   []llm.Message               `json:"messages"`   // agent memory (user/assistant/tool)
	Transcript []Turn                      `json:"transcript"` // what the user saw
	Products   map[string]shopping.Product `json:"products"`   // every product the agent has seen
	Stores     []shopping.Store            `json:"stores"`     // nearby stores found

	Recommendation *Recommendation `json:"recommendation,omitempty"`
	Looks          []Look          `json:"looks,omitempty"`
	LookResults    []LookResult    `json:"look_results,omitempty"`
	TryOnResults   []TryOnResult   `json:"tryon_results,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// New creates an empty session in the capture phase.
func New(id string) *State {
	now := time.Now()
	return &State{
		ID:         id,
		Phase:      PhaseCapture,
		Messages:   []llm.Message{},
		Transcript: []Turn{},
		Products:   map[string]shopping.Product{},
		Stores:     []shopping.Store{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// AddProducts records products the agent has seen, keyed by id.
func (s *State) AddProducts(ps []shopping.Product) {
	if s.Products == nil {
		s.Products = map[string]shopping.Product{}
	}
	for _, p := range ps {
		s.Products[p.ID] = p
	}
}

// AddStores records stores, de-duplicated by place id.
func (s *State) AddStores(ss []shopping.Store) {
	seen := map[string]bool{}
	for _, st := range s.Stores {
		seen[st.PlaceID] = true
	}
	for _, st := range ss {
		if st.PlaceID == "" || !seen[st.PlaceID] {
			s.Stores = append(s.Stores, st)
			seen[st.PlaceID] = true
		}
	}
}

// ProductsFor resolves ids to products, skipping unknown ones.
func (s *State) ProductsFor(ids []string) []shopping.Product {
	out := make([]shopping.Product, 0, len(ids))
	for _, id := range ids {
		if p, ok := s.Products[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

// LastAssistantOptions returns the buttons offered in the latest assistant turn.
func (s *State) LastAssistantOptions() []Option {
	for i := len(s.Transcript) - 1; i >= 0; i-- {
		if s.Transcript[i].Role == "assistant" {
			return s.Transcript[i].Options
		}
	}
	return nil
}

// QuestionsAsked counts assistant turns that waited for user input.
func (s *State) QuestionsAsked() int {
	n := 0
	for _, t := range s.Transcript {
		if t.Role == "assistant" && (t.InputMode == "buttons" || t.InputMode == "voice") {
			n++
		}
	}
	return n
}

// FindTryOn returns a cached try-on for a product, if any.
func (s *State) FindTryOn(productID string) *TryOnResult {
	for i := range s.TryOnResults {
		if s.TryOnResults[i].ProductID == productID {
			return &s.TryOnResults[i]
		}
	}
	return nil
}
