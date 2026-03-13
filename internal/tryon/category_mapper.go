package tryon

import "strings"

// MapActionToCategory maps an action ID and garment description to a VTON category.
func MapActionToCategory(actionID, garmentDesc string) string {
	text := strings.ToLower(actionID + " " + garmentDesc)

	lowerBodyKeywords := []string{"pantalón", "pantalon", "jeans", "shorts", "bermuda", "chinos", "trousers", "pants"}
	for _, kw := range lowerBodyKeywords {
		if strings.Contains(text, kw) {
			return "lower_body"
		}
	}

	dressKeywords := []string{"vestido", "jumpsuit", "dress", "romper"}
	for _, kw := range dressKeywords {
		if strings.Contains(text, kw) {
			return "dresses"
		}
	}

	// Default: upper_body (most common for style advisor recommendations)
	return "upper_body"
}
