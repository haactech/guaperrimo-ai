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
