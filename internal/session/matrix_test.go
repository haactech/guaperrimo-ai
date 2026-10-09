package session

import (
	"testing"

	"stylerag/internal/shopping"
)

func matrixState() *State {
	st := New("m1")
	for _, id := range []string{"s1", "s2", "s3", "s4", "p1", "p2", "z1", "belt"} {
		st.Products[id] = shopping.Product{ID: id, Title: id, Price: 100, Thumbnail: "https://img/" + id}
	}
	st.Products["noimg"] = shopping.Product{ID: "noimg", Title: "sin foto"}
	st.Recommendation = &Recommendation{
		ShoppingList: []ShoppingItem{
			{Slot: "upper_body", ProductIDs: []string{"s1", "s2", "s3", "s4"}, Why: "alarga", Priority: 1},
			{Slot: "lower_body", ProductIDs: []string{"p1", "noimg", "p2"}, Priority: 2},
			{Slot: "footwear", ProductIDs: []string{"z1"}, Priority: 3},
			{Slot: "accessory", ProductIDs: []string{"belt"}, Priority: 4},
		},
	}
	st.Looks = []Look{{ID: "look_1", Pieces: []LookPiece{{Slot: "lower_body", ProductID: "p2"}, {Slot: "footwear", ProductID: "z1"}}}}
	return st
}

func TestBuildMatrix(t *testing.T) {
	m := BuildMatrix(matrixState(), 3)
	if m == nil || len(m.Slots) != 3 {
		t.Fatalf("expected 3 slots (no accessory), got %+v", m)
	}
	if m.Slots[0].Slot != "upper_body" || len(m.Slots[0].Options) != 3 || m.Slots[0].Options[0].ProductID != "s1" || m.Slots[0].Options[0].Why != "alarga" {
		t.Errorf("upper row: %+v", m.Slots[0])
	}
	if m.Slots[1].Slot != "lower_body" || len(m.Slots[1].Options) != 2 {
		t.Errorf("lower row should skip the product without image: %+v", m.Slots[1])
	}
	if m.Default["footwear"] != "z1" || m.Slots[2].Label != "Calzado" {
		t.Errorf("default/labels: %+v", m)
	}
}

func TestComboValidateAndNeighbors(t *testing.T) {
	m := BuildMatrix(matrixState(), 3)
	sel, msg := m.Validate(Selection{"upper_body": "s2"})
	if msg != "" || sel["lower_body"] != "p1" || sel["footwear"] != "z1" {
		t.Fatalf("validate should fill defaults: %v %s", sel, msg)
	}
	if _, msg := m.Validate(Selection{"upper_body": "s4"}); msg == "" {
		t.Fatal("s4 was cut by maxPerSlot and must be rejected")
	}
	combo := m.Combo(sel)
	if ComboKey(combo) != "s2|p1|z1" {
		t.Fatalf("combo key = %s", ComboKey(combo))
	}
	n := m.Neighbors(sel)
	// upper has 2 other options, lower 1, footwear 0
	if len(n) != 3 {
		t.Fatalf("neighbors = %d, want 3", len(n))
	}
}

func TestBuildMatrixWithoutRecommendation(t *testing.T) {
	if BuildMatrix(New("x"), 3) != nil {
		t.Fatal("no recommendation means no matrix")
	}
}
