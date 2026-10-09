package shopping

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type counting struct {
	Fake
	mu sync.Mutex
	n  int
}

func (c *counting) SearchProducts(ctx context.Context, q ProductQuery) ([]Product, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.Fake.SearchProducts(ctx, q)
}

func TestCacheMemoisesSearches(t *testing.T) {
	inner := &counting{}
	c := NewCache(inner, time.Minute)
	ctx := context.Background()

	a, _ := c.SearchProducts(ctx, ProductQuery{Query: "Camisa hombre ", MaxPrice: 800})
	b, _ := c.SearchProducts(ctx, ProductQuery{Query: "camisa hombre", MaxPrice: 800})
	if inner.n != 1 || len(a) != len(b) {
		t.Fatalf("identical searches should hit the cache, provider called %d times", inner.n)
	}
	if _, err := c.SearchProducts(ctx, ProductQuery{Query: "camisa hombre", MaxPrice: 500}); err != nil || inner.n != 2 {
		t.Fatalf("different price cap must miss the cache (calls=%d, err=%v)", inner.n, err)
	}
	a[0].NearbyStore = &StoreRef{Name: "x"}
	again, _ := c.SearchProducts(ctx, ProductQuery{Query: "camisa hombre", MaxPrice: 800})
	if again[0].NearbyStore != nil {
		t.Fatal("cache must hand out copies, not shared slices")
	}
}

func TestStripURLHidesAPIKey(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://serpapi.com/search.json?api_key=SECRET123&q=x", Err: errors.New("context deadline exceeded")}
	got := stripURL(err).Error()
	if strings.Contains(got, "SECRET123") || !strings.Contains(got, "deadline") {
		t.Fatalf("sanitised error = %q", got)
	}
}
