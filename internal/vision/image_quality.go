package vision

// ImageQuality captures the LLM's assessment of the input photo.
type ImageQuality struct {
	Overall      string   `json:"overall"`       // "good", "acceptable", "poor"
	Lighting     string   `json:"lighting"`      // "good", "dim", "overexposed", "mixed"
	BodyCoverage string   `json:"body_coverage"` // "full", "three_quarter", "half", "face_only"
	Focus        string   `json:"focus"`         // "sharp", "acceptable", "blurry"
	Background   string   `json:"background"`    // "clean", "busy", "irrelevant"
	BlindSpots   []string `json:"blind_spots"`   // things the LLM couldn't assess
	Notes        string   `json:"notes"`         // brief explanation of limitations
}

// CanReliablyAssess returns true if the given dimension is NOT in blind spots.
func (iq *ImageQuality) CanReliablyAssess(dimension string) bool {
	for _, bs := range iq.BlindSpots {
		if bs == dimension {
			return false
		}
	}
	return true
}

// CompensationArea describes a question that discovery should ask
// to compensate for information the photo couldn't provide.
type CompensationArea struct {
	BlindSpot string   `json:"blind_spot"`
	Question  string   `json:"question"`
	InputMode string   `json:"input_mode"`
	Options   []string `json:"options,omitempty"`
	FactField string   `json:"fact_field"`
}

// NeedsCompensation returns discovery questions to compensate for blind spots.
func (iq *ImageQuality) NeedsCompensation() []CompensationArea {
	var areas []CompensationArea

	for _, bs := range iq.BlindSpots {
		switch bs {
		case "skin_tone":
			areas = append(areas, CompensationArea{
				BlindSpot: bs,
				Question:  "No alcanzo a apreciar bien tu tono de piel en la foto. ¿Dirías que es más cálido (dorado) o más frío (rosado)?",
				InputMode: "buttons",
				Options:   []string{"Cálido / dorado", "Frío / rosado", "No estoy seguro"},
				FactField: "additional_context",
			})
		case "full_silhouette":
			areas = append(areas, CompensationArea{
				BlindSpot: bs,
				Question:  "No alcanzo a ver tu outfit completo en la foto. ¿Qué llevas de la cintura para abajo?",
				InputMode: "voice",
				FactField: "additional_context",
			})
		case "color_accuracy":
			areas = append(areas, CompensationArea{
				BlindSpot: bs,
				Question:  "La iluminación de la foto me dificulta ver los colores reales. ¿De qué color es tu camiseta?",
				InputMode: "voice",
				FactField: "additional_context",
			})
		case "lower_body":
			areas = append(areas, CompensationArea{
				BlindSpot: bs,
				Question:  "No alcanzo a ver bien la parte de abajo. ¿Qué pantalón y zapatos llevas?",
				InputMode: "voice",
				FactField: "additional_context",
			})
		case "fit_precision":
			areas = append(areas, CompensationArea{
				BlindSpot: bs,
				Question:  "¿Cómo sientes que te queda la ropa? ¿Ajustada, holgada, o depende de la prenda?",
				InputMode: "buttons",
				Options:   []string{"Ajustada en general", "Holgada en general", "Depende de la prenda"},
				FactField: "additional_context",
			})
		}
	}

	return areas
}
