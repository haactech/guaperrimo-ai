package api

import (
	"stylerag/internal/catalog"
	"stylerag/internal/rag"
)

// StyleAnalysisResponse matches the iOS StyleAnalysis Codable struct.
type StyleAnalysisResponse struct {
	Message  string                `json:"message"`
	Options  []StyleOptionResponse `json:"options"`
	Analysis string                `json:"analysis,omitempty"`
}

// StyleOptionResponse represents a single style option suggestion.
type StyleOptionResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ChatRequest is the input for POST /session/{id}/chat.
type ChatRequest struct {
	Type       string `json:"type"`                  // "image" | "button_response" | "voice_response"
	ImageURL   string `json:"image_url,omitempty"`   // required when type=image
	OptionID   string `json:"option_id,omitempty"`   // required when type=button_response
	Transcript string `json:"transcript,omitempty"`  // required when type=voice_response
}

// ChatResponse is the output for POST /session/{id}/chat.
type ChatResponse struct {
	SessionID       string                   `json:"session_id"`
	Phase           string                   `json:"phase"`
	Turn            int                      `json:"turn"`
	Message         string                   `json:"message"`
	InputMode       string                   `json:"input_mode"`
	Options         []ChatOption             `json:"options"`
	IsFinal         bool                     `json:"is_final"`
	PriorityActions []PriorityActionResponse `json:"priority_actions,omitempty"`
	Products        []rag.Product            `json:"products,omitempty"`
	LooksGenerating bool                     `json:"looks_generating,omitempty"`
}

// ChatOption represents a button option in discovery.
type ChatOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PriorityActionResponse is a recommended action in the final response.
type PriorityActionResponse struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Impact      string   `json:"impact"`
	Effort      string   `json:"effort"`
	ProductIDs  []string `json:"product_ids,omitempty"`
}

// --- Catalog DTOs ---

// ProductListResponse is the response for GET /products.
type ProductListResponse struct {
	Products []rag.Product `json:"products"`
	Total    int           `json:"total"`
	Offset   int           `json:"offset"`
	Limit    int           `json:"limit"`
}

// BatchGetRequest is the body for POST /products/batch.
type BatchGetRequest struct {
	IDs []string `json:"ids"`
}

// BatchGetResponse is the response for POST /products/batch.
type BatchGetResponse struct {
	Products []rag.Product `json:"products"`
}

// CategoriesResponse is the response for GET /products/categories.
type CategoriesResponse struct {
	Categories []catalog.CategoryCount `json:"categories"`
}

// --- Try-On DTOs ---

// TryOnRequest is the input for POST /session/{id}/tryon.
type TryOnRequest struct {
	ActionID           string `json:"action_id"`
	GarmentDescription string `json:"garment_description"`
}

// TryOnResponse is the output for POST /session/{id}/tryon.
type TryOnResponse struct {
	SessionID     string      `json:"session_id"`
	ActionID      string      `json:"action_id"`
	TryOnImageURL string      `json:"tryon_image_url"`
	GarmentUsed   GarmentInfo `json:"garment_used"`
	GenerationMs  int64       `json:"generation_time_ms"`
}

// GarmentInfo describes the garment used in the try-on.
type GarmentInfo struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	CatalogID string `json:"catalog_id"`
	ImageURL  string `json:"image_url"`
}

// --- Look DTOs ---

// LooksResponse is the response for GET /session/{id}/looks.
type LooksResponse struct {
	SessionID   string          `json:"session_id"`
	Status      string          `json:"status"` // "pending" | "generating" | "partial" | "ready"
	Looks       []LookDTO       `json:"looks"`
	LookResults []LookResultDTO `json:"look_results"`
	AllReady    bool            `json:"all_ready"`
	ReadyCount  int             `json:"ready_count"`
	TotalCount  int             `json:"total_count"`
}

// LookDTO represents a composed look definition.
type LookDTO struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Vibe        string        `json:"vibe"`
	Pieces      []LookPieceDTO `json:"pieces"`
}

// LookPieceDTO represents a single piece in a look.
type LookPieceDTO struct {
	Slot        string `json:"slot"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// LookResultDTO represents the generation result for a look.
type LookResultDTO struct {
	LookID        string           `json:"look_id"`
	Status        string           `json:"status"`
	Pieces        []PieceResultDTO `json:"pieces,omitempty"`
	FinalImageURL string           `json:"final_image_url,omitempty"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	GenerationMs  int64            `json:"generation_time_ms"`
}

// PieceResultDTO represents the generation result for a single piece.
type PieceResultDTO struct {
	Slot            string `json:"slot"`
	ProductID       string `json:"product_id,omitempty"`
	ProductName     string `json:"product_name,omitempty"`
	ProductImageURL string `json:"product_image_url,omitempty"`
	TryOnImageURL   string `json:"tryon_image_url,omitempty"`
	Category        string `json:"category"`
	GenerationMs    int64  `json:"generation_time_ms,omitempty"`
}
