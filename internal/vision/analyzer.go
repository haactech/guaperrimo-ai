package vision

import (
	"context"
)

// OutfitAnalysis represents the result of analyzing a user's outfit photo
type OutfitAnalysis struct {
	DetectedItems  []DetectedItem `json:"detected_items"`
	DetectedStyles []string       `json:"detected_styles"`
	DetectedColors []string       `json:"detected_colors"`
	OverallFit     string         `json:"overall_fit"`
	Observations   string         `json:"observations"`
}

// DetectedItem represents a single clothing item detected in the image
type DetectedItem struct {
	Category    string   `json:"category"`    // tops, bottoms, footwear, accessories
	Subcategory string   `json:"subcategory"` // t-shirt, jeans, sneakers, etc.
	Color       string   `json:"color"`
	Fit         string   `json:"fit"`
	StyleTags   []string `json:"style_tags"`
}

// Analyzer defines the interface for outfit image analysis
type Analyzer interface {
	// AnalyzeOutfit processes an image and returns style analysis
	AnalyzeOutfit(ctx context.Context, imageData []byte) (*OutfitAnalysis, error)
}
