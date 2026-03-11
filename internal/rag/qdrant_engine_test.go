package rag

import (
	"testing"
)

func TestBuildQdrantFilter_Empty(t *testing.T) {
	q := SearchQuery{Text: "something"}
	filter := buildQdrantFilter(q)
	if filter != nil {
		t.Error("expected nil filter for query without filters")
	}
}

func TestBuildQdrantFilter_Category(t *testing.T) {
	q := SearchQuery{Categories: []string{"Topwear"}}
	filter := buildQdrantFilter(q)
	if filter == nil {
		t.Fatal("expected non-nil filter")
	}
	if len(filter.Must) != 1 {
		t.Fatalf("expected 1 must condition, got %d", len(filter.Must))
	}
	if filter.Must[0].Key != "category" {
		t.Errorf("expected key=category, got %s", filter.Must[0].Key)
	}
	if filter.Must[0].Match == nil || filter.Must[0].Match.Value != "Topwear" {
		t.Error("expected match value=Topwear")
	}
}

func TestBuildQdrantFilter_PriceRange(t *testing.T) {
	q := SearchQuery{MinPrice: 100, MaxPrice: 2000}
	filter := buildQdrantFilter(q)
	if filter == nil {
		t.Fatal("expected non-nil filter")
	}
	if len(filter.Must) != 2 {
		t.Fatalf("expected 2 must conditions (min + max), got %d", len(filter.Must))
	}

	var hasMax, hasMin bool
	for _, c := range filter.Must {
		if c.Key == "price" && c.Range != nil {
			if c.Range.Lte != nil && *c.Range.Lte == 2000 {
				hasMax = true
			}
			if c.Range.Gte != nil && *c.Range.Gte == 100 {
				hasMin = true
			}
		}
	}
	if !hasMax {
		t.Error("expected max price filter")
	}
	if !hasMin {
		t.Error("expected min price filter")
	}
}

func TestBuildQdrantFilter_Combined(t *testing.T) {
	q := SearchQuery{
		Categories: []string{"Topwear"},
		MaxPrice:   1500,
	}
	filter := buildQdrantFilter(q)
	if filter == nil {
		t.Fatal("expected non-nil filter")
	}
	if len(filter.Must) != 2 {
		t.Fatalf("expected 2 must conditions, got %d", len(filter.Must))
	}
}
