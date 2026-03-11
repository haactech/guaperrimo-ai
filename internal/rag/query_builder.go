package rag

import (
	"regexp"
	"strconv"
	"strings"

	"stylerag/internal/session"
)

var budgetRegex = regexp.MustCompile(`(\d[\d,]*)\s*(?:pesos|mxn|usd|\$)`)

// BuildSearchQueries translates gap items into SearchQuery objects for semantic search.
func BuildSearchQueries(gaps []session.GapItem, profile *session.UserStyleProfile, limit int) []SearchQuery {
	if limit <= 0 {
		limit = 3
	}

	var maxPrice float64
	if profile != nil {
		maxPrice = extractBudget(profile.Constraints)
	}

	var queries []SearchQuery
	for _, g := range gaps {
		if g.Actionable == "" {
			continue
		}
		q := SearchQuery{
			Text:     g.Actionable,
			Limit:    limit,
			MaxPrice: maxPrice,
		}
		queries = append(queries, q)
	}
	return queries
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
