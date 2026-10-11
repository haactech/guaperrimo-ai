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
	inner    Provider
	ttl      time.Duration
	mu       sync.Mutex
	items    map[string]cacheEntry
	inflight map[string]chan struct{}
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
	return &Cache{inner: inner, ttl: ttl, items: map[string]cacheEntry{}, inflight: map[string]chan struct{}{}}
}

// once runs fetch for key unless a cached value exists; concurrent callers
// for the same key wait for the first and reuse its result.
func (c *Cache) once(key string, fetch func() (any, error)) (any, error) {
	for {
		if v, ok := c.get(key); ok {
			return v, nil
		}
		c.mu.Lock()
		ch, running := c.inflight[key]
		if !running {
			ch = make(chan struct{})
			c.inflight[key] = ch
			c.mu.Unlock()
			v, err := fetch()
			if err == nil {
				c.put(key, v)
			}
			c.mu.Lock()
			delete(c.inflight, key)
			close(ch)
			c.mu.Unlock()
			return v, err
		}
		c.mu.Unlock()
		<-ch
		// loop: the value is cached now, or the fetch failed and we retry it ourselves
		if _, ok := c.get(key); !ok {
			c.mu.Lock()
			if _, still := c.inflight[key]; still {
				c.mu.Unlock()
				continue
			}
			c.mu.Unlock()
			v, err := fetch()
			if err == nil {
				c.put(key, v)
			}
			return v, err
		}
	}
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
	v, err := c.once(key, func() (any, error) {
		out, err := c.inner.SearchProducts(ctx, q)
		if err != nil {
			return nil, err
		}
		return cloneProducts(out), nil
	})
	if err != nil {
		return nil, err
	}
	return cloneProducts(v.([]Product)), nil
}

func (c *Cache) FindStores(ctx context.Context, q StoreQuery) ([]Store, error) {
	key := fmt.Sprintf("s|%s|%.4f|%.4f|%d|%d", strings.ToLower(strings.TrimSpace(q.Query)), q.Lat, q.Lng, q.RadiusM, q.Limit)
	v, err := c.once(key, func() (any, error) {
		out, err := c.inner.FindStores(ctx, q)
		if err != nil {
			return nil, err
		}
		return append([]Store(nil), out...), nil
	})
	if err != nil {
		return nil, err
	}
	return append([]Store(nil), v.([]Store)...), nil
}

func (c *Cache) Geocode(ctx context.Context, query string) (*Geo, error) {
	key := "g|" + strings.ToLower(strings.TrimSpace(query))
	v, err := c.once(key, func() (any, error) {
		out, err := c.inner.Geocode(ctx, query)
		if err != nil {
			return nil, err
		}
		g := *out
		return &g, nil
	})
	if err != nil {
		return nil, err
	}
	g := *v.(*Geo)
	return &g, nil
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
	v, err := c.once(key, func() (any, error) {
		out, err := resolver.ProductDetails(ctx, sourceID)
		if err != nil {
			return nil, err
		}
		d := *out
		return &d, nil
	})
	if err != nil {
		return nil, err
	}
	d := *v.(*ProductDetails)
	return &d, nil
}
