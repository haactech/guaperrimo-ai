package vision

import (
	"strings"

	"stylerag/internal/session"
)

// painPhrases maps user input substrings to normalized pain point labels.
var painPhrases = map[string]string{
	"me queda pegada":     "ropa ajustada/pegada",
	"me queda ajustada":   "ropa ajustada",
	"me queda grande":     "ropa holgada",
	"me queda chica":      "ropa pequeña",
	"no combinar":         "dificultad para combinar",
	"no sé combinar":      "no sabe combinar colores/prendas",
	"no se combinar":      "no sabe combinar colores/prendas",
	"no me favorece":      "prendas que no favorecen",
	"no acorde a mi edad": "estilo no acorde a su edad",
	"me veo gordo":        "insatisfacción con silueta",
	"estoy gordo":         "insatisfacción con peso",
	"piernas gruesas":     "insatisfacción con piernas",
	"no sé qué ponerme":  "inseguridad general con vestimenta",
	"no se que ponerme":   "inseguridad general con vestimenta",
	"no sé vestirme":      "inseguridad general con vestimenta",
	"no se vestirme":      "inseguridad general con vestimenta",
	"muy pegada":          "ropa ajustada/pegada",
}

// constraintPhrases maps user input substrings to normalized constraint labels.
var constraintPhrases = map[string]string{
	"no vestidos":    "evitar vestidos",
	"no formal":      "evitar looks formales",
	"no corbata":     "evitar corbata",
	"no traje":       "evitar traje",
	"no tan formal":  "evitar formalidad excesiva",
	"no como viejito": "evitar estilo anticuado",
	"no como abuelo": "evitar estilo anticuado",
	"no muy pegada":  "evitar ropa ajustada",
	"no estampada":   "evitar estampados",
}

// occasionPhrases maps user input substrings to occasion values.
var occasionPhrases = map[string]string{
	"oficina":    "oficina",
	"trabajo":    "trabajo",
	"reunión":    "reuniones",
	"reuniones":  "reuniones",
	"cita":       "cita",
	"fiesta":     "evento social",
	"evento":     "evento",
	"día a día":  "uso diario",
	"dia a dia":  "uso diario",
	"diario":     "uso diario",
}

// ExtractFactsFromInput does keyword-based fact extraction from user input
// WITHOUT calling the LLM. This ensures facts are captured even when Go
// forces advance before/after the LLM call.
func ExtractFactsFromInput(fm *session.FactMap, input string) {
	if fm == nil {
		return
	}
	lower := strings.ToLower(input)

	// Pain points
	for phrase, painPoint := range painPhrases {
		if strings.Contains(lower, phrase) {
			if !containsString(fm.PainPoints, painPoint) {
				fm.PainPoints = append(fm.PainPoints, painPoint)
			}
		}
	}

	// Constraints
	for phrase, constraint := range constraintPhrases {
		if strings.Contains(lower, phrase) {
			if !containsString(fm.Constraints, constraint) {
				fm.Constraints = append(fm.Constraints, constraint)
			}
		}
	}

	// Occasion (only if not already set)
	if fm.Occasion == nil {
		for phrase, occasion := range occasionPhrases {
			if strings.Contains(lower, phrase) {
				val := occasion
				fm.Occasion = &val
				break
			}
		}
	}

	// Approach (only if not already set)
	if fm.Approach == nil {
		if strings.Contains(lower, "cambiar") || strings.Contains(lower, "diferente") || strings.Contains(lower, "nuevo") {
			val := "explorar"
			fm.Approach = &val
		} else if strings.Contains(lower, "mejorar") || strings.Contains(lower, "refinar") || strings.Contains(lower, "ajustar") {
			val := "refinar"
			fm.Approach = &val
		}
	}
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
