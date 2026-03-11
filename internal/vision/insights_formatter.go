package vision

import (
	"fmt"
	"strings"
)

// FormatInsightsForPrompt renders ImageInsights as a human-readable block
// for injection into the discovery prompt.
func FormatInsightsForPrompt(insights *ImageInsights) string {
	var sb strings.Builder

	sb.WriteString("PROBLEMAS DETECTADOS:\n")
	if len(insights.DetectedIssues) == 0 {
		sb.WriteString("- Ninguno grave\n")
	}
	for _, issue := range insights.DetectedIssues {
		sb.WriteString(fmt.Sprintf("- [%s] %s (score: %d/10, severidad: %s)\n",
			issue.Area, issue.Observation, issue.Score, issue.Severity))
	}

	sb.WriteString("\nFORTALEZAS DETECTADAS:\n")
	if len(insights.DetectedStrengths) == 0 {
		sb.WriteString("- Ninguna destacable\n")
	}
	for _, s := range insights.DetectedStrengths {
		sb.WriteString(fmt.Sprintf("- %s\n", s))
	}

	sb.WriteString("\nHECHOS INFERIDOS:\n")
	f := insights.InferredFacts
	sb.WriteString(fmt.Sprintf("- Color necesita trabajo: %v\n", f.ColorNeedsWork))
	if f.SuggestedSeason != "" {
		sb.WriteString(fmt.Sprintf("- Temporada sugerida: %s\n", f.SuggestedSeason))
	}
	if len(f.ConflictingColors) > 0 {
		sb.WriteString(fmt.Sprintf("- Piezas con colores conflictivos: %s\n",
			strings.Join(f.ConflictingColors, ", ")))
	}
	sb.WriteString(fmt.Sprintf("- Fit necesita trabajo: %v\n", f.FitNeedsWork))
	if f.TooInformalFor != "" {
		sb.WriteString(fmt.Sprintf("- Nota: este outfit NO sería apropiado para contextos como %s. Pero NO asumas que el usuario quiere usarlo para eso. Espera a que ÉL mencione la ocasión.\n", f.TooInformalFor))
	}
	if f.BodyTypeHint != "" {
		sb.WriteString(fmt.Sprintf("- Recomendación de silueta: %s\n", f.BodyTypeHint))
	}

	return sb.String()
}

// FormatCompensationsForPrompt renders pending compensation questions
// for injection into the discovery prompt.
func FormatCompensationsForPrompt(compensations []CompensationArea) string {
	if len(compensations) == 0 {
		return "(ninguna — la foto es suficiente)"
	}
	var sb strings.Builder
	for _, c := range compensations {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.BlindSpot, c.Question))
	}
	return sb.String()
}

// FormatQualityForPrompt renders image quality summary for injection into prompts.
func FormatQualityForPrompt(iq *ImageQuality) string {
	if iq == nil {
		return "Calidad general: no evaluada\nBlind spots: ninguno reportado"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Calidad general: %s\n", iq.Overall))
	sb.WriteString(fmt.Sprintf("Iluminación: %s\n", iq.Lighting))
	sb.WriteString(fmt.Sprintf("Cobertura corporal: %s\n", iq.BodyCoverage))
	sb.WriteString(fmt.Sprintf("Enfoque: %s\n", iq.Focus))
	if len(iq.BlindSpots) > 0 {
		sb.WriteString(fmt.Sprintf("Blind spots: %s\n", strings.Join(iq.BlindSpots, ", ")))
	} else {
		sb.WriteString("Blind spots: ninguno\n")
	}
	if iq.Notes != "" {
		sb.WriteString(fmt.Sprintf("Notas: %s\n", iq.Notes))
	}
	return sb.String()
}
