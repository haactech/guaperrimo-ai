package vision

import (
	"testing"

	"stylerag/internal/session"
)

func TestValidateScores_ClampsLow(t *testing.T) {
	s := ValidateScores(session.StyleScores{
		ColorHarmony: 0, Fit: -5, Proportion: 0,
		LineHarmony: -1, StyleCoherence: 0, OccasionMatch: 0,
	})
	for _, v := range []int{s.ColorHarmony, s.Fit, s.Proportion, s.LineHarmony, s.StyleCoherence, s.OccasionMatch} {
		if v != 1 {
			t.Errorf("expected 1, got %d", v)
		}
	}
}

func TestValidateScores_ClampsHigh(t *testing.T) {
	s := ValidateScores(session.StyleScores{
		ColorHarmony: 15, Fit: 11, Proportion: 100,
		LineHarmony: 20, StyleCoherence: 99, OccasionMatch: 12,
	})
	for _, v := range []int{s.ColorHarmony, s.Fit, s.Proportion, s.LineHarmony, s.StyleCoherence, s.OccasionMatch} {
		if v != 10 {
			t.Errorf("expected 10, got %d", v)
		}
	}
}

func TestValidateScores_PreservesValid(t *testing.T) {
	s := ValidateScores(session.StyleScores{
		ColorHarmony: 5, Fit: 7, Proportion: 3,
		LineHarmony: 8, StyleCoherence: 1, OccasionMatch: 10,
	})
	if s.ColorHarmony != 5 || s.Fit != 7 || s.Proportion != 3 || s.LineHarmony != 8 || s.StyleCoherence != 1 || s.OccasionMatch != 10 {
		t.Errorf("valid scores should not change: %+v", s)
	}
}

func TestCalculateOverallScore_PerfectScores(t *testing.T) {
	s := session.StyleScores{
		ColorHarmony: 10, Fit: 10, Proportion: 10,
		LineHarmony: 10, StyleCoherence: 10, OccasionMatch: 10,
	}
	score, grade := CalculateOverallScore(s)
	if score != 10.0 {
		t.Errorf("expected 10.0, got %.2f", score)
	}
	if grade != "A" {
		t.Errorf("expected A, got %s", grade)
	}
}

func TestCalculateOverallScore_MinScores(t *testing.T) {
	s := session.StyleScores{
		ColorHarmony: 1, Fit: 1, Proportion: 1,
		LineHarmony: 1, StyleCoherence: 1, OccasionMatch: 1,
	}
	score, grade := CalculateOverallScore(s)
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.2f", score)
	}
	if grade != "F" {
		t.Errorf("expected F, got %s", grade)
	}
}

func TestCalculateOverallScore_GradeBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		scores   session.StyleScores
		wantGrade string
	}{
		{
			name:      "grade A boundary",
			scores:    session.StyleScores{ColorHarmony: 9, Fit: 9, Proportion: 9, LineHarmony: 9, StyleCoherence: 9, OccasionMatch: 9},
			wantGrade: "A",
		},
		{
			name:      "grade B",
			scores:    session.StyleScores{ColorHarmony: 8, Fit: 8, Proportion: 8, LineHarmony: 8, StyleCoherence: 8, OccasionMatch: 8},
			wantGrade: "B",
		},
		{
			name:      "grade C",
			scores:    session.StyleScores{ColorHarmony: 6, Fit: 6, Proportion: 6, LineHarmony: 6, StyleCoherence: 6, OccasionMatch: 6},
			wantGrade: "C",
		},
		{
			name:      "grade D",
			scores:    session.StyleScores{ColorHarmony: 4, Fit: 4, Proportion: 4, LineHarmony: 4, StyleCoherence: 4, OccasionMatch: 4},
			wantGrade: "D",
		},
		{
			name:      "grade F",
			scores:    session.StyleScores{ColorHarmony: 3, Fit: 3, Proportion: 3, LineHarmony: 3, StyleCoherence: 3, OccasionMatch: 3},
			wantGrade: "F",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, grade := CalculateOverallScore(tt.scores)
			if grade != tt.wantGrade {
				t.Errorf("expected grade %s, got %s", tt.wantGrade, grade)
			}
		})
	}
}

func TestCalculateOverallScore_WeightedAverage(t *testing.T) {
	// Verify weights sum to 1.0
	totalWeight := weightFit + weightColorHarmony + weightStyleCoherence + weightOccasionMatch + weightProportion + weightLineHarmony
	if totalWeight < 0.999 || totalWeight > 1.001 {
		t.Errorf("weights should sum to 1.0, got %.4f", totalWeight)
	}

	// Fit-heavy score (fit=10, rest=1) should be higher than color-heavy (color=10, rest=1)
	fitHeavy := session.StyleScores{Fit: 10, ColorHarmony: 1, Proportion: 1, LineHarmony: 1, StyleCoherence: 1, OccasionMatch: 1}
	colorHeavy := session.StyleScores{Fit: 1, ColorHarmony: 10, Proportion: 1, LineHarmony: 1, StyleCoherence: 1, OccasionMatch: 1}
	fitScore, _ := CalculateOverallScore(fitHeavy)
	colorScore, _ := CalculateOverallScore(colorHeavy)
	if fitScore <= colorScore {
		t.Errorf("fit (weight=0.25) should produce higher score than color (weight=0.20): fit=%.2f, color=%.2f", fitScore, colorScore)
	}
}
