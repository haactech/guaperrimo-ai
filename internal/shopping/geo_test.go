package shopping

import "testing"

func TestDistanceM(t *testing.T) {
	if d := DistanceM(19.4326, -99.1332, 19.4326, -99.1332); d != 0 {
		t.Fatalf("same point = %d", d)
	}
	// 0.01 degrees of latitude is roughly 1.1 km.
	d := DistanceM(19.4326, -99.1332, 19.4426, -99.1332)
	if d < 1050 || d > 1150 {
		t.Fatalf("0.01 deg lat = %d m", d)
	}
}

func TestMatchNearbyStore(t *testing.T) {
	stores := []Store{
		{PlaceID: "a", Name: "ZARA Reforma 222", DistanceM: 900},
		{PlaceID: "b", Name: "Zara Parque Delta", DistanceM: 400},
		{PlaceID: "c", Name: "H&M Parque Delta", DistanceM: 450},
		{PlaceID: "d", Name: "Liverpool Insurgentes", DistanceM: 1200},
		{PlaceID: "e", Name: "Pull & Bear Antara", DistanceM: 2000},
	}
	cases := map[string]string{
		"Zara MX":       "b", // closest of two matches
		"H&M México":    "c",
		"Liverpool":     "d",
		"Pull&Bear":     "e",
		"Amazon.com.mx": "",
		"Mercado Libre": "",
		"":              "",
	}
	for merchant, want := range cases {
		got := MatchNearbyStore(merchant, stores)
		switch {
		case want == "" && got != nil:
			t.Errorf("%q: expected no match, got %s", merchant, got.PlaceID)
		case want != "" && (got == nil || got.PlaceID != want):
			t.Errorf("%q: got %v, want %s", merchant, got, want)
		}
	}
}

func TestFakeDirectedSearchReturnsOnlyThatMerchant(t *testing.T) {
	f := Fake{}
	ps, _ := f.SearchProducts(t.Context(), ProductQuery{Query: "Zara camisa lino azul hombre"})
	if len(ps) != 1 || ps[0].Store != "Zara MX" {
		t.Fatalf("directed search should return only Zara, got %+v", ps)
	}
	all, _ := f.SearchProducts(t.Context(), ProductQuery{Query: "camisa lino azul hombre"})
	if len(all) < 4 {
		t.Fatalf("undirected search should return every merchant, got %d", len(all))
	}
}
