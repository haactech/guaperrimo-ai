package vision

import "stylerag/internal/session"

// Dimension weights for overall score calculation.
const (
	weightFit            = 0.25
	weightColorHarmony   = 0.20
	weightStyleCoherence = 0.15
	weightOccasionMatch  = 0.15
	weightProportion     = 0.15
	weightLineHarmony    = 0.10
)

// CalculateOverallScore computes a weighted average and letter grade from StyleScores.
func CalculateOverallScore(s session.StyleScores) (float64, string) {
	score := float64(s.Fit)*weightFit +
		float64(s.ColorHarmony)*weightColorHarmony +
		float64(s.StyleCoherence)*weightStyleCoherence +
		float64(s.OccasionMatch)*weightOccasionMatch +
		float64(s.Proportion)*weightProportion +
		float64(s.LineHarmony)*weightLineHarmony

	grade := scoreToGrade(score)
	return score, grade
}

func scoreToGrade(score float64) string {
	switch {
	case score >= 9.0:
		return "A"
	case score >= 7.5:
		return "B"
	case score >= 6.0:
		return "C"
	case score >= 4.0:
		return "D"
	default:
		return "F"
	}
}

// ValidateScores clamps all dimension scores to the 1-10 range.
func ValidateScores(s session.StyleScores) session.StyleScores {
	s.ColorHarmony = clamp(s.ColorHarmony)
	s.Fit = clamp(s.Fit)
	s.Proportion = clamp(s.Proportion)
	s.LineHarmony = clamp(s.LineHarmony)
	s.StyleCoherence = clamp(s.StyleCoherence)
	s.OccasionMatch = clamp(s.OccasionMatch)
	return s
}

func clamp(v int) int {
	if v < 1 {
		return 1
	}
	if v > 10 {
		return 10
	}
	return v
}
