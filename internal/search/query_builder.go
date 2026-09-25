package search

import (
	"regexp"
	"strconv"
	"strings"

	"stylerag/internal/session"
)

var budgetRegex = regexp.MustCompile(`(\d[\d,]*)\s*(?:pesos|mxn|usd|\$)`)

// BuildQuery creates a web-search-friendly query from a gap and user profile.
// Returns the query string and extracted budget ceiling.
func BuildQuery(gap session.GapItem, profile *session.UserStyleProfile) (query string, maxPrice float64) {
	// Start with the actionable text from the gap
	q := gap.Actionable

	if profile != nil {
		// Add fit context from Kibbe family if available
		if profile.KibbeFamily != "" {
			q = appendIfMissing(q, kibbeToFit(profile.KibbeFamily))
		}

		// Add occasion context
		if profile.Occasion != "" {
			q = appendIfMissing(q, profile.Occasion)
		}

		maxPrice = extractBudget(profile.Constraints)
	}

	// Always scope to menswear (PoC target demographic)
	q = appendIfMissing(q, "hombre")

	return q, maxPrice
}

// BuildQueries creates search queries for multiple gaps.
func BuildQueries(gaps []session.GapItem, profile *session.UserStyleProfile) (queries []string, opts []SearchOptions) {
	for _, g := range gaps {
		if g.Actionable == "" {
			continue
		}
		q, maxPrice := BuildQuery(g, profile)
		queries = append(queries, q)
		opts = append(opts, SearchOptions{
			MaxPrice: maxPrice,
			Limit:    3,
		})
	}
	return queries, opts
}

// kibbeToFit translates Kibbe family to common fit terms for search.
func kibbeToFit(kibbe string) string {
	switch strings.ToLower(kibbe) {
	case "dramatic", "soft dramatic":
		return "structured fit"
	case "natural", "soft natural", "flamboyant natural":
		return "relaxed fit"
	case "classic", "dramatic classic", "soft classic":
		return "regular fit"
	case "romantic":
		return "soft fit"
	case "gamine", "soft gamine", "flamboyant gamine":
		return "slim fit"
	default:
		return ""
	}
}

// appendIfMissing adds a term to the query only if not already present.
func appendIfMissing(query, term string) string {
	if term == "" {
		return query
	}
	if strings.Contains(strings.ToLower(query), strings.ToLower(term)) {
		return query
	}
	return query + " " + term
}

// extractBudget tries to find a numeric budget ceiling from constraint strings.
func extractBudget(constraints []string) float64 {
	for _, c := range constraints {
		lower := strings.ToLower(c)
		matches := budgetRegex.FindStringSubmatch(lower)
		if len(matches) >= 2 {
			s := strings.ReplaceAll(matches[1], ",", "")
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				return v
			}
		}
	}
	return 0
}
