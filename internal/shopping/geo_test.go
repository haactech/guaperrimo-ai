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

func TestIsRetailStoreSkipsServicesAndOffices(t *testing.T) {
	for _, c := range []struct {
		s    Store
		want bool
	}{
		{Store{Name: "Liverpool Insurgentes", Category: "Tienda departamental"}, true},
		{Store{Name: "H&M", Category: "Tienda de ropa"}, true},
		{Store{Name: "Lefties", Category: ""}, true},
		{Store{Name: "Walmart Supercenter", Category: "Tienda de autoservicio"}, true},
		{Store{Name: "Sears Insurgentes", Category: "Grandes almacenes"}, true},
		{Store{Name: "Sears Car service", Category: "Tienda de accesorios para auto"}, false},
		{Store{Name: "Liverpool", Category: "Centro comercial"}, true},
		{Store{Name: "Studio H&M", Category: "Centro de estética"}, false},
		{Store{Name: "Sears Car service", Category: "Taller de reparación de automóviles"}, false},
		{Store{Name: "Sede Liverpool - Roma Norte", Category: "Oficinas corporativas"}, false},
		{Store{Name: "Coppel", Category: "Banco"}, false},
		{Store{Name: "Liverpool Óptica", Category: "Óptica"}, false},
		{Store{Name: "Papeleria liverpool", Category: "Papelería"}, false},
		{Store{Name: "HM Distribuidores", Category: "Distribuidor mayorista"}, false},
	} {
		if got := IsRetailStore(c.s); got != c.want {
			t.Errorf("IsRetailStore(%q / %q) = %v, want %v", c.s.Name, c.s.Category, got, c.want)
		}
	}
}

func TestIsBranchOfNeedsTheNameToStartWithTheBrand(t *testing.T) {
	for _, c := range []struct {
		merchant, name string
		want           bool
	}{
		{"Liverpool", "Liverpool Insurgentes", true},
		{"Liverpool", "Papeleria liverpool", false},
		{"H&M México", "H&M Reforma", true},
		{"H&M", "H & M Parque Delta", true},
		{"Zara MX", "Zara", true},
		{"Lefties", "Lefties Plaza Universidad", true},
		{"Sears México", "Sears Car service", true}, // name ok; IsRetailStore drops it
		{"Liverpool", "Liv", false},
	} {
		if got := IsBranchOf(c.merchant, c.name); got != c.want {
			t.Errorf("IsBranchOf(%q, %q) = %v, want %v", c.merchant, c.name, got, c.want)
		}
	}
}
