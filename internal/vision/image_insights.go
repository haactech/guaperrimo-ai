package vision

import (
	"fmt"
	"strings"
)

// ImageInsights translates raw analysis data into actionable observations.
type ImageInsights struct {
	DetectedIssues     []DetectedIssue  `json:"detected_issues"`
	DetectedStrengths  []string         `json:"detected_strengths"`
	InferredFacts      InferredFacts    `json:"inferred_facts"`
	ImageQuality       *ImageQuality    `json:"image_quality"`
	CompensationNeeded []CompensationArea `json:"compensation_needed"`
}

// DetectedIssue is a problem found in the photo analysis.
type DetectedIssue struct {
	Area        string `json:"area"`        // "fit", "color", "proportion", "occasion", "coherence"
	Score       int    `json:"score"`       // score from analysis (1-10)
	Observation string `json:"observation"` // human-readable description
	Severity    string `json:"severity"`    // "high" (<=4), "medium" (5-6), "low" (7+)
}

// InferredFacts are things we can infer from the photo without asking.
type InferredFacts struct {
	ColorNeedsWork    bool     `json:"color_needs_work"`
	FitNeedsWork      bool     `json:"fit_needs_work"`
	TooInformalFor    string   `json:"too_informal_for,omitempty"`
	SuggestedSeason   string   `json:"suggested_season,omitempty"`
	ConflictingColors []string `json:"conflicting_colors,omitempty"`
	BodyTypeHint      string   `json:"body_type_hint,omitempty"`
}

// GenerateInsights derives actionable insights from an OutfitAnalysis.
func GenerateInsights(analysis *OutfitAnalysis) *ImageInsights {
	insights := &ImageInsights{}

	// --- Detected issues ---

	if sa := analysis.SilhouetteAnalysis; sa != nil {
		if sa.FitScore <= 5 {
			insights.DetectedIssues = append(insights.DetectedIssues, DetectedIssue{
				Area:        "fit",
				Score:       sa.FitScore,
				Observation: sa.FitNotes,
				Severity:    severity(sa.FitScore),
			})
			insights.InferredFacts.FitNeedsWork = true
		}

		if sa.ProportionScore <= 5 {
			insights.DetectedIssues = append(insights.DetectedIssues, DetectedIssue{
				Area:        "proportion",
				Score:       sa.ProportionScore,
				Observation: sa.ProportionNotes,
				Severity:    severity(sa.ProportionScore),
			})
		}

		switch sa.EstimatedKibbeFamily {
		case "Natural":
			insights.InferredFacts.BodyTypeHint = "Prendas con caída relajada y tejidos naturales van a favorecer tu silueta. Evitar ropa muy ajustada o muy rígida."
		case "Dramatic":
			insights.InferredFacts.BodyTypeHint = "Líneas rectas y siluetas alargadas van a favorecer tu figura. Evitar volumen excesivo."
		case "Classic":
			insights.InferredFacts.BodyTypeHint = "Cortes equilibrados y proporciones clásicas son lo tuyo. Evitar extremos (ni muy oversize ni muy ajustado)."
		case "Romantic":
			insights.InferredFacts.BodyTypeHint = "Telas fluidas y prendas que definan cintura van a favorecerte. Evitar líneas muy rectas o boxy."
		case "Gamine":
			insights.InferredFacts.BodyTypeHint = "Prendas con detalles y contrastes te favorecen. Puedes jugar con prints y accesorios más que otros tipos."
		}
	}

	if ca := analysis.ColorAnalysis; ca != nil {
		if ca.ColorHarmonyScore <= 6 {
			insights.DetectedIssues = append(insights.DetectedIssues, DetectedIssue{
				Area:        "color",
				Score:       ca.ColorHarmonyScore,
				Observation: fmt.Sprintf("Piezas conflictivas: %s", strings.Join(ca.ConflictingPieces, ", ")),
				Severity:    severity(ca.ColorHarmonyScore),
			})
			insights.InferredFacts.ColorNeedsWork = true
		}
		insights.InferredFacts.SuggestedSeason = ca.EstimatedSeason
		insights.InferredFacts.ConflictingColors = ca.ConflictingPieces
	}

	if aa := analysis.ArchetypeAnalysis; aa != nil {
		if aa.CurrentArchetype == "Streetwear" || aa.CurrentArchetype == "Casual" || aa.SecondaryArchetype == "Casual" {
			insights.InferredFacts.TooInformalFor = "oficina, reuniones, eventos profesionales"
		}
	}

	// --- Detected strengths ---

	if sa := analysis.SilhouetteAnalysis; sa != nil {
		if sa.ProportionScore >= 7 {
			insights.DetectedStrengths = append(insights.DetectedStrengths, "Buenas proporciones entre parte superior e inferior")
		}
		if sa.FitScore >= 7 {
			insights.DetectedStrengths = append(insights.DetectedStrengths, "Buen fit general en las prendas principales")
		}
	}
	if ca := analysis.ColorAnalysis; ca != nil && ca.ColorHarmonyScore >= 7 {
		insights.DetectedStrengths = append(insights.DetectedStrengths, "Colores que armonizan bien con tu tono de piel")
	}
	if len(analysis.DetectedStyles) > 0 {
		insights.DetectedStrengths = append(insights.DetectedStrengths,
			fmt.Sprintf("Coherencia de estilo: %s", strings.Join(analysis.DetectedStyles, "/")))
	}

	// --- Image quality & compensations ---

	if analysis.ImageQuality != nil {
		insights.ImageQuality = analysis.ImageQuality
		insights.CompensationNeeded = analysis.ImageQuality.NeedsCompensation()
	}

	return insights
}

// HighSeverityCount returns how many detected issues have severity "high".
func (ins *ImageInsights) HighSeverityCount() int {
	count := 0
	for _, issue := range ins.DetectedIssues {
		if issue.Severity == "high" {
			count++
		}
	}
	return count
}

func severity(score int) string {
	if score <= 4 {
		return "high"
	}
	if score <= 6 {
		return "medium"
	}
	return "low"
}
