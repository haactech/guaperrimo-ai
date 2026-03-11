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

	// Expert evaluation (populated by style theory addendum)
	ColorAnalysis      *ColorAnalysis      `json:"color_analysis,omitempty"`
	SilhouetteAnalysis *SilhouetteAnalysis `json:"silhouette_analysis,omitempty"`
	ArchetypeAnalysis  *ArchetypeAnalysis  `json:"archetype_analysis,omitempty"`

	// Image quality assessment
	ImageQuality *ImageQuality `json:"image_quality,omitempty"`
}

// DetectedItem represents a single clothing item detected in the image
type DetectedItem struct {
	Category    string   `json:"category"`    // tops, bottoms, footwear, accessories
	Subcategory string   `json:"subcategory"` // t-shirt, jeans, sneakers, etc.
	Color       string   `json:"color"`
	Fit         string   `json:"fit"`
	StyleTags   []string `json:"style_tags"`
}

// ColorAnalysis contains seasonal color theory evaluation
type ColorAnalysis struct {
	EstimatedSeason   string   `json:"estimated_season"`
	Confidence        string   `json:"confidence"`
	Reasoning         string   `json:"reasoning"`
	ColorHarmonyScore int      `json:"color_harmony_score"`
	HarmoniousPieces  []string `json:"harmonious_pieces"`
	ConflictingPieces []string `json:"conflicting_pieces"`
}

// SilhouetteAnalysis contains Kibbe-based body line evaluation
type SilhouetteAnalysis struct {
	EstimatedKibbeFamily string `json:"estimated_kibbe_family"`
	Confidence           string `json:"confidence"`
	Reasoning            string `json:"reasoning"`
	FitScore             int    `json:"fit_score"`
	FitNotes             string `json:"fit_notes"`
	ProportionScore      int    `json:"proportion_score"`
	ProportionNotes      string `json:"proportion_notes"`
	LineHarmonyScore     int    `json:"line_harmony_score"`
	LineHarmonyNotes     string `json:"line_harmony_notes"`
}

// ArchetypeAnalysis identifies the style archetype communicated by the outfit
type ArchetypeAnalysis struct {
	CurrentArchetype   string `json:"current_archetype"`
	SecondaryArchetype string `json:"secondary_archetype"`
	Reasoning          string `json:"reasoning"`
}

// Analyzer defines the interface for outfit image analysis
type Analyzer interface {
	// AnalyzeOutfit processes an image and returns style analysis
	AnalyzeOutfit(ctx context.Context, imageData []byte) (*OutfitAnalysis, error)
}
