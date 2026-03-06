package api

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
