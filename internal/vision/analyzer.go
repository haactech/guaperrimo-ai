// Package vision turns the user's photo into a structured style analysis.
package vision

import "context"

// OutfitAnalysis is the structured result of analysing a full-body photo.
type OutfitAnalysis struct {
	DetectedItems  []DetectedItem `json:"detected_items"`
	DetectedStyles []string       `json:"detected_styles"`
	DetectedColors []string       `json:"detected_colors"`
	OverallFit     string         `json:"overall_fit"`
	Observations   string         `json:"observations"`

	ColorAnalysis      *ColorAnalysis      `json:"color_analysis,omitempty"`
	SilhouetteAnalysis *SilhouetteAnalysis `json:"silhouette_analysis,omitempty"`
	ArchetypeAnalysis  *ArchetypeAnalysis  `json:"archetype_analysis,omitempty"`
	ImageQuality       *ImageQuality       `json:"image_quality,omitempty"`
}

// DetectedItem is one visible garment.
type DetectedItem struct {
	Category    string   `json:"category"`
	Subcategory string   `json:"subcategory"`
	Color       string   `json:"color"`
	Fit         string   `json:"fit"`
	StyleTags   []string `json:"style_tags"`
}

// ColorAnalysis is the seasonal colour evaluation.
type ColorAnalysis struct {
	EstimatedSeason   string   `json:"estimated_season"`
	Confidence        string   `json:"confidence"`
	Reasoning         string   `json:"reasoning"`
	ColorHarmonyScore int      `json:"color_harmony_score"`
	HarmoniousPieces  []string `json:"harmonious_pieces"`
	ConflictingPieces []string `json:"conflicting_pieces"`
}

// SilhouetteAnalysis is the Kibbe-based body-line evaluation.
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

// ArchetypeAnalysis names the style archetype the outfit communicates.
type ArchetypeAnalysis struct {
	CurrentArchetype   string `json:"current_archetype"`
	SecondaryArchetype string `json:"secondary_archetype"`
	Reasoning          string `json:"reasoning"`
}

// ImageQuality reports how much the photo allowed the model to see.
type ImageQuality struct {
	Overall      string   `json:"overall"`
	Lighting     string   `json:"lighting"`
	BodyCoverage string   `json:"body_coverage"`
	Focus        string   `json:"focus"`
	Background   string   `json:"background"`
	BlindSpots   []string `json:"blind_spots"`
	Notes        string   `json:"notes"`
}

// Analyzer produces an OutfitAnalysis from image bytes.
type Analyzer interface {
	AnalyzeOutfit(ctx context.Context, imageData []byte) (*OutfitAnalysis, error)
}
