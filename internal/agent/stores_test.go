package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

// countingShops records the map searches and serves the fake stores.
type countingShops struct {
	shopping.Fake
	mu      sync.Mutex
	queries []string
}

func (c *countingShops) FindStores(ctx context.Context, q shopping.StoreQuery) ([]shopping.Store, error) {
	c.mu.Lock()
	c.queries = append(c.queries, q.Query)
	c.mu.Unlock()
	return c.Fake.FindStores(ctx, q)
}

func TestFindBranchesLinksChainProductsToTheirNearestStore(t *testing.T) {
	shops := &countingShops{}
	r := NewRunner(&scripted{}, Deps{Shopping: shops}, 5, 6)
	st := newState() // device location, no stores yet
	st.Stores = nil
	ts := &turnState{st: st}
	products := []shopping.Product{
		{ID: "p1", Store: "Liverpool", Currency: "MXN"},
		{ID: "p2", Store: "H&M México", Currency: "MXN"},
		{ID: "p3", Store: "Amazon MX", Currency: "MXN"},
		{ID: "p4", Store: "Liverpool", Currency: "MXN"},
	}

	r.findBranches(context.Background(), ts, products)
	if len(shops.queries) != 2 {
		t.Fatalf("one search per chain, none for online-only merchants: %v", shops.queries)
	}
	shopping.AnnotateNearby(products, st.Stores)
	if products[0].NearbyStore == nil || products[0].NearbyStore.Name != "Liverpool Insurgentes" {
		t.Fatalf("Liverpool product should point to its branch: %+v", products[0].NearbyStore)
	}
	if products[1].NearbyStore == nil || products[1].NearbyStore.Name != "H&M" {
		t.Fatalf("H&M product should point to its branch: %+v", products[1].NearbyStore)
	}
	if products[2].NearbyStore != nil {
		t.Fatalf("online-only merchant has no branch: %+v", products[2].NearbyStore)
	}

	// Each merchant is searched once per session.
	r.findBranches(context.Background(), ts, products)
	if len(shops.queries) != 2 {
		t.Fatalf("repeated lookups: %v", shops.queries)
	}
}

func TestFindBranchesNeedsTheLocation(t *testing.T) {
	shops := &countingShops{}
	r := NewRunner(&scripted{}, Deps{Shopping: shops}, 5, 6)
	st := newState()
	st.Location = nil
	r.findBranches(context.Background(), &turnState{st: st}, []shopping.Product{{Store: "Liverpool"}})
	if len(shops.queries) != 0 {
		t.Fatalf("no map search without a location: %v", shops.queries)
	}
}

func TestBrandDropsCountryNoise(t *testing.T) {
	for in, want := range map[string]string{"H&M México": "H&M", "Zara MX": "Zara", "Liverpool": "Liverpool", "Tienda Oficial": "Tienda Oficial"} {
		if got := brand(in); got != want {
			t.Errorf("brand(%q) = %q, want %q", in, got, want)
		}
	}
}

// mixedShops returns a women's and a kids' listing next to the men's ones,
// as Google Shopping does even for "hombre" queries.
type mixedShops struct {
	shopping.Fake
	queries []string
}

func (m *mixedShops) SearchProducts(ctx context.Context, q shopping.ProductQuery) ([]shopping.Product, error) {
	m.queries = append(m.queries, q.Query)
	ps, err := m.Fake.SearchProducts(ctx, q)
	ps = append(ps,
		shopping.Product{ID: "p_blusa", Title: "Blusa de lino manga larga para mujer", Store: "Zara MX", Price: 699, Currency: "MXN", Thumbnail: "x"},
		shopping.Product{ID: "p_nino", Title: "Camisa de lino para niño", Store: "H&M", Price: 399, Currency: "MXN", Thumbnail: "x"},
	)
	return ps, err
}

func TestSearchKeepsOnlyMensClothing(t *testing.T) {
	shops := &mixedShops{}
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("s1", toolSearchProducts, map[string]any{"query": "camisa de lino verde"})),
		calls(call("a1", toolAskUser, map[string]any{"message": "¿Te late?", "input_mode": "voice"})),
	}}
	r := NewRunner(model, Deps{Shopping: shops}, 5, 6)
	st := newState()
	if _, err := r.RunTurn(context.Background(), st, TurnInput{Kind: "voice", Text: "hola"}); err != nil {
		t.Fatal(err)
	}
	if len(shops.queries) != 1 || shops.queries[0] != "camisa de lino verde hombre" {
		t.Fatalf("the search must ask for men's clothing: %v", shops.queries)
	}
	for id, p := range st.Products {
		if !shopping.IsMenswear(p.Title) || id == "p_blusa" || id == "p_nino" {
			t.Fatalf("women's or kids' listing reached the stylist: %+v", p)
		}
	}
	if msg := toolMessage(st, "s1"); strings.Contains(msg, "Blusa") || strings.Contains(msg, "niño") {
		t.Fatalf("the model must not see them either: %s", msg)
	}
}

func TestReferencesSkipWomensListings(t *testing.T) {
	idea := session.GarmentIdea{Colors: []session.Swatch{{Name: "Verde"}}, Textures: []string{"lino"}}
	ps := []shopping.Product{
		{ID: "blusa", Title: "Blusa de lino verde para mujer", Currency: "MXN", Thumbnail: "x"},
		{ID: "camisa", Title: "Camisa azul de algodón hombre", Currency: "MXN", Thumbnail: "x"},
	}
	if got := bestReference(idea, ps); got == nil || got.ID != "camisa" {
		t.Fatalf("a better color match for women never wins: %+v", got)
	}
}
