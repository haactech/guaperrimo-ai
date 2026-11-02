package agent

import (
	"context"
	"sync"
	"testing"

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
