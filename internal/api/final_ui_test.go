package api

import (
	"testing"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

func TestComposeFinalUIEdgeCases(t *testing.T) {
	st := session.New("sess_final_ui")
	st.AddProducts([]shopping.Product{
		{ID: "p_1", Title: "Camisa", Store: "H&M", Price: 740, Thumbnail: "thumb", LargeImage: "large", Link: "https://l",
			NearbyStore: &shopping.StoreRef{PlaceID: "x", Name: "H&M Reforma", DistanceM: 450}},
		{ID: "p_2", Title: "Pantalón", Price: 500, Thumbnail: "thumb2"},
	})
	st.Recommendation = &session.Recommendation{
		Summary: "Resumen",
		ShoppingList: []session.ShoppingItem{
			{Slot: "upper_body", ProductIDs: []string{"p_1", "p_2"}},
			{Slot: "lower_body", ProductIDs: []string{"p_2"}},
			{Slot: "accessory", ProductIDs: nil},             // no product: no card
			{Slot: "outerwear", ProductIDs: []string{"p_9"}}, // unknown product: no card
		},
		TotalMXN: 1240,
	}
	got := composeFinalUI(st)
	// text, budget_bar (no priority actions → no steps), 2 cards
	if len(got) != 4 || got[1].Type != "budget_bar" || got[1].BudgetMXN != 0 {
		t.Fatalf("%+v", got)
	}
	card := got[2]
	if card.ProductID != "p_1" || card.ImageURL != "large" || card.Nearby == nil || card.Nearby.DistanceM != 450 {
		t.Errorf("first card must use the primary product and the large image: %+v", card)
	}
	if c := got[3]; c.ProductID != "p_2" || c.ImageURL != "thumb2" || c.Nearby != nil {
		t.Errorf("second card falls back to the thumbnail: %+v", c)
	}
	if composeFinalUI(session.New("x")) != nil {
		t.Error("no recommendation, no ui")
	}
}
