package shopping

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Cache memoises provider results for a short time. Searches are slow and
// the model often repeats them, and a retried turn after a timeout should be
// instant.
type Cache struct {
	inner Provider
	ttl   time.Duration
	mu    sync.Mutex
	items map[string]cacheEntry
}

type cacheEntry struct {
	at  time.Time
	val any
}

// NewCache wraps a provider.
func NewCache(inner Provider, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Cache{inner: inner, ttl: ttl, items: map[string]cacheEntry{}}
}

func (c *Cache) Name() string { return c.inner.Name() + "+cache" }

func (c *Cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || time.Since(e.at) > c.ttl {
		delete(c.items, key)
		return nil, false
	}
	return e.val, true
}

func (c *Cache) put(key string, val any) {
	c.mu.Lock()
	c.items[key] = cacheEntry{at: time.Now(), val: val}
	c.mu.Unlock()
}

func (c *Cache) SearchProducts(ctx context.Context, q ProductQuery) ([]Product, error) {
	key := fmt.Sprintf("p|%s|%.0f|%s|%d", strings.ToLower(strings.TrimSpace(q.Query)), q.MaxPrice, strings.ToLower(q.Location), q.Limit)
	if v, ok := c.get(key); ok {
		return cloneProducts(v.([]Product)), nil
	}
	out, err := c.inner.SearchProducts(ctx, q)
	if err != nil {
		return nil, err
	}
	c.put(key, cloneProducts(out))
	return out, nil
}

func (c *Cache) FindStores(ctx context.Context, q StoreQuery) ([]Store, error) {
	key := fmt.Sprintf("s|%s|%.4f|%.4f|%d|%d", strings.ToLower(strings.TrimSpace(q.Query)), q.Lat, q.Lng, q.RadiusM, q.Limit)
	if v, ok := c.get(key); ok {
		return append([]Store(nil), v.([]Store)...), nil
	}
	out, err := c.inner.FindStores(ctx, q)
	if err != nil {
		return nil, err
	}
	c.put(key, append([]Store(nil), out...))
	return out, nil
}

func (c *Cache) Geocode(ctx context.Context, query string) (*Geo, error) {
	key := "g|" + strings.ToLower(strings.TrimSpace(query))
	if v, ok := c.get(key); ok {
		g := *v.(*Geo)
		return &g, nil
	}
	out, err := c.inner.Geocode(ctx, query)
	if err != nil {
		return nil, err
	}
	g := *out
	c.put(key, &g)
	return out, nil
}

// cloneProducts copies the slice so callers can annotate without touching the cache.
func cloneProducts(ps []Product) []Product {
	out := make([]Product, len(ps))
	for i, p := range ps {
		p.NearbyStore = nil
		p.Tags = append([]string(nil), p.Tags...)
		out[i] = p
	}
	return out
}

// ProductDetails caches detail lookups when the inner provider supports them.
func (c *Cache) ProductDetails(ctx context.Context, sourceID string) (*ProductDetails, error) {
	resolver, ok := c.inner.(DetailResolver)
	if !ok {
		return &ProductDetails{}, nil
	}
	key := "d|" + sourceID
	if v, ok := c.get(key); ok {
		d := *v.(*ProductDetails)
		return &d, nil
	}
	out, err := resolver.ProductDetails(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	d := *out
	c.put(key, &d)
	return out, nil
}
