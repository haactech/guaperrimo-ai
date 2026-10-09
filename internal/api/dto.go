package api

import "stylerag/internal/shopping"

// LocationDTO is the device location the app may send with any chat turn.
type LocationDTO struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label,omitempty"`
}

// ChatRequest is the input for POST /session/{id}/chat.
type ChatRequest struct {
	Type          string       `json:"type"`                     // image | button_response | voice_response | text
	ImageURL      string       `json:"image_url,omitempty"`      // informational; the photo is read from storage
	OptionID      string       `json:"option_id,omitempty"`      // type=button_response
	Transcript    string       `json:"transcript,omitempty"`     // type=voice_response
	Text          string       `json:"text,omitempty"`           // type=text
	Location      *LocationDTO `json:"location,omitempty"`       // optional, any turn
	RadiusM       int          `json:"radius_m,omitempty"`       // optional, any turn
	AllowShipping *bool        `json:"allow_shipping,omitempty"` // optional, any turn
}

// ChatOption is a button.
type ChatOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PriorityActionResponse is a recommendation card.
type PriorityActionResponse struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Impact      string   `json:"impact"`
	Effort      string   `json:"effort"`
	ProductIDs  []string `json:"product_ids,omitempty"`
}

// ShoppingItemResponse is one line of the shopping list with resolved products.
type ShoppingItemResponse struct {
	Slot        string             `json:"slot"`
	Description string             `json:"description"`
	Why         string             `json:"why,omitempty"`
	Priority    int                `json:"priority,omitempty"`
	Products    []shopping.Product `json:"products"`
}

// ChatResponse is the output for POST /session/{id}/chat and
// GET /session/{id}/recommendation.
type ChatResponse struct {
	SessionID       string                   `json:"session_id"`
	Phase           string                   `json:"phase"`
	Turn            int                      `json:"turn"`
	Message         string                   `json:"message"`
	InputMode       string                   `json:"input_mode"`
	Options         []ChatOption             `json:"options"`
	IsFinal         bool                     `json:"is_final"`
	LocationKnown   bool                     `json:"location_known"`
	PriorityActions []PriorityActionResponse `json:"priority_actions,omitempty"`
	ShoppingList    []ShoppingItemResponse   `json:"shopping_list,omitempty"`
	Products        []shopping.Product       `json:"products,omitempty"`
	Stores          []shopping.Store         `json:"stores,omitempty"`
	TotalMXN        float64                  `json:"total_mxn,omitempty"`
	LooksGenerating bool                     `json:"looks_generating,omitempty"`
}

// --- Try-on ---

// TryOnRequest is the input for POST /session/{id}/tryon. Either product_id
// or action_id (first product of that action) identifies the garment.
type TryOnRequest struct {
	ProductID          string `json:"product_id,omitempty"`
	ActionID           string `json:"action_id,omitempty"`
	GarmentDescription string `json:"garment_description,omitempty"`
}

// TryOnResponse is the output for POST /session/{id}/tryon.
type TryOnResponse struct {
	SessionID     string      `json:"session_id"`
	ActionID      string      `json:"action_id,omitempty"`
	ProductID     string      `json:"product_id"`
	TryOnImageURL string      `json:"tryon_image_url"`
	GarmentUsed   GarmentInfo `json:"garment_used"`
	GenerationMs  int64       `json:"generation_time_ms"`
}

// GarmentInfo describes the garment used in a try-on.
type GarmentInfo struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	CatalogID string `json:"catalog_id"`
	ImageURL  string `json:"image_url"`
	Link      string `json:"link,omitempty"`
}

// --- Looks ---

// LooksResponse is the output for GET /session/{id}/looks.
type LooksResponse struct {
	SessionID   string          `json:"session_id"`
	Status      string          `json:"status"` // none | pending | generating | partial | ready
	Looks       []LookDTO       `json:"looks"`
	LookResults []LookResultDTO `json:"look_results"`
	AllReady    bool            `json:"all_ready"`
	ReadyCount  int             `json:"ready_count"`
	TotalCount  int             `json:"total_count"`
}

// LookDTO is a composed look.
type LookDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Vibe        string         `json:"vibe"`
	Pieces      []LookPieceDTO `json:"pieces"`
}

// LookPieceDTO is one piece of a look.
type LookPieceDTO struct {
	Slot            string  `json:"slot"`
	Description     string  `json:"description"`
	Category        string  `json:"category"`
	ProductID       string  `json:"product_id,omitempty"`
	ProductName     string  `json:"product_name,omitempty"`
	ProductImageURL string  `json:"product_image_url,omitempty"`
	ProductLink     string  `json:"product_link,omitempty"`
	Price           float64 `json:"price,omitempty"`
	Store           string  `json:"store,omitempty"`
}

// LookResultDTO is the try-on result for a look.
type LookResultDTO struct {
	LookID        string           `json:"look_id"`
	Status        string           `json:"status"`
	Pieces        []PieceResultDTO `json:"pieces,omitempty"`
	FinalImageURL string           `json:"final_image_url,omitempty"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	GenerationMs  int64            `json:"generation_time_ms"`
}

// PieceResultDTO is the try-on result for one piece.
type PieceResultDTO struct {
	Slot            string `json:"slot"`
	ProductID       string `json:"product_id,omitempty"`
	ProductName     string `json:"product_name,omitempty"`
	ProductImageURL string `json:"product_image_url,omitempty"`
	TryOnImageURL   string `json:"tryon_image_url,omitempty"`
	Category        string `json:"category"`
	GenerationMs    int64  `json:"generation_time_ms,omitempty"`
}
