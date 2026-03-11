package vision

import "strings"

// stopwords are too frequent in this domain to be meaningful for comparison.
var stopwords = map[string]bool{
	"para":     true,
	"como":     true,
	"algo":     true,
	"más":      true,
	"tipo":     true,
	"ejemplo":  true,
	"qué":      true,
	"que":      true,
	"pero":     true,
	"también":  true,
	"este":     true,
	"esta":     true,
	"tiene":    true,
	"quieres":  true,
	"quiero":   true,
	"estilo":   true,
	"oficina":  true,
	"empezar":  true,
	"decir":    true,
}

// extractKeywords returns significant words (>4 chars, not stopwords).
func extractKeywords(text string) map[string]bool {
	keywords := make(map[string]bool)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		clean := strings.Trim(word, ".,?!¿¡\"':")
		if len(clean) > 4 && !stopwords[clean] {
			keywords[clean] = true
		}
	}
	return keywords
}

// IsSimilarQuestion returns true if the current bot message reuses >60%
// of the keywords from the previous bot message.
func IsSimilarQuestion(current, previous string) bool {
	if previous == "" {
		return false
	}

	currentKW := extractKeywords(current)
	previousKW := extractKeywords(previous)

	if len(currentKW) == 0 || len(previousKW) == 0 {
		return false
	}

	overlap := 0
	for kw := range currentKW {
		if previousKW[kw] {
			overlap++
		}
	}

	ratio := float64(overlap) / float64(len(currentKW))
	return ratio > 0.6
}
