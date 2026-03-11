package rag

import (
	"testing"

	"stylerag/internal/session"
)

func TestBuildSearchQueries_Basic(t *testing.T) {
	gaps := []session.GapItem{
		{Dimension: "color", Priority: 3, Actionable: "camisa oxford verde bosque en slim fit"},
		{Dimension: "fit", Priority: 1, Actionable: "pantalón chino en regular fit color beige"},
		{Dimension: "empty", Priority: 2, Actionable: ""},
	}
	profile := &session.UserStyleProfile{
		Constraints: []string{"presupuesto máximo 2000 pesos"},
	}

	queries := BuildSearchQueries(gaps, profile, 3)

	if len(queries) != 2 {
		t.Fatalf("expected 2 queries (skipping empty actionable), got %d", len(queries))
	}

	if queries[0].Text != "camisa oxford verde bosque en slim fit" {
		t.Errorf("expected first query text from gap, got %q", queries[0].Text)
	}
	if queries[0].Limit != 3 {
		t.Errorf("expected limit=3, got %d", queries[0].Limit)
	}
	if queries[0].MaxPrice != 2000 {
		t.Errorf("expected MaxPrice=2000 from constraints, got %f", queries[0].MaxPrice)
	}
}

func TestBuildSearchQueries_NoBudget(t *testing.T) {
	gaps := []session.GapItem{
		{Dimension: "color", Priority: 1, Actionable: "something"},
	}

	queries := BuildSearchQueries(gaps, nil, 0)

	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(queries))
	}
	if queries[0].MaxPrice != 0 {
		t.Errorf("expected MaxPrice=0 (no budget), got %f", queries[0].MaxPrice)
	}
	if queries[0].Limit != 3 {
		t.Errorf("expected default limit=3, got %d", queries[0].Limit)
	}
}

func TestBuildSearchQueries_EmptyGaps(t *testing.T) {
	queries := BuildSearchQueries(nil, nil, 5)
	if len(queries) != 0 {
		t.Fatalf("expected 0 queries for nil gaps, got %d", len(queries))
	}
}

func TestExtractBudget(t *testing.T) {
	tests := []struct {
		name        string
		constraints []string
		expected    float64
	}{
		{"pesos format", []string{"presupuesto máximo 2000 pesos"}, 2000},
		{"mxn format", []string{"budget 1500 mxn"}, 1500},
		{"usd format", []string{"max 100 usd"}, 100},
		{"dollar sign", []string{"up to 3000$"}, 3000},
		{"comma number", []string{"no more than 1,500 pesos"}, 1500},
		{"no budget", []string{"no specific constraints"}, 0},
		{"empty", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBudget(tt.constraints)
			if result != tt.expected {
				t.Errorf("extractBudget(%v) = %f, want %f", tt.constraints, result, tt.expected)
			}
		})
	}
}
