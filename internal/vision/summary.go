package vision

import (
	"fmt"
	"strings"
)

// Summarize renders the analysis as a compact Spanish block for the agent's
// system prompt. It keeps the facts and drops the verbose reasoning.
func Summarize(a *OutfitAnalysis) string {
	if a == nil {
		return "(sin análisis de foto)"
	}
	var sb strings.Builder

	if len(a.DetectedItems) > 0 {
		sb.WriteString("Prendas visibles:\n")
		for _, it := range a.DetectedItems {
			sb.WriteString(fmt.Sprintf("- %s %s, color %s, fit %s", it.Category, it.Subcategory, it.Color, it.Fit))
			if len(it.StyleTags) > 0 {
				sb.WriteString(" (" + strings.Join(it.StyleTags, ", ") + ")")
			}
			sb.WriteString("\n")
		}
	}
	if len(a.DetectedStyles) > 0 {
		sb.WriteString("Estilo actual: " + strings.Join(a.DetectedStyles, ", ") + "\n")
	}
	if len(a.DetectedColors) > 0 {
		sb.WriteString("Colores del outfit: " + strings.Join(a.DetectedColors, ", ") + "\n")
	}
	if a.OverallFit != "" {
		sb.WriteString("Fit general: " + a.OverallFit + "\n")
	}
	if a.Observations != "" {
		sb.WriteString("Observaciones: " + a.Observations + "\n")
	}
	if ca := a.ColorAnalysis; ca != nil {
		sb.WriteString(fmt.Sprintf("Temporada de color estimada: %s (confianza %s), armonía %d/10", ca.EstimatedSeason, ca.Confidence, ca.ColorHarmonyScore))
		if len(ca.ConflictingPieces) > 0 {
			sb.WriteString("; piezas que desentonan: " + strings.Join(ca.ConflictingPieces, ", "))
		}
		sb.WriteString("\n")
	}
	if sa := a.SilhouetteAnalysis; sa != nil {
		sb.WriteString(fmt.Sprintf("Silueta (Kibbe): %s (confianza %s). Fit %d/10, proporción %d/10, líneas %d/10.\n",
			sa.EstimatedKibbeFamily, sa.Confidence, sa.FitScore, sa.ProportionScore, sa.LineHarmonyScore))
		if sa.FitNotes != "" {
			sb.WriteString("  Fit: " + sa.FitNotes + "\n")
		}
		if sa.ProportionNotes != "" {
			sb.WriteString("  Proporción: " + sa.ProportionNotes + "\n")
		}
	}
	if aa := a.ArchetypeAnalysis; aa != nil {
		sb.WriteString("Arquetipo que comunica hoy: " + aa.CurrentArchetype)
		if aa.SecondaryArchetype != "" {
			sb.WriteString(" / " + aa.SecondaryArchetype)
		}
		sb.WriteString("\n")
	}
	if iq := a.ImageQuality; iq != nil {
		sb.WriteString(fmt.Sprintf("Calidad de la foto: %s; cobertura %s; luz %s", iq.Overall, iq.BodyCoverage, iq.Lighting))
		if len(iq.BlindSpots) > 0 {
			sb.WriteString("; no se pudo evaluar: " + strings.Join(iq.BlindSpots, ", "))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
