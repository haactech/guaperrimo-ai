package agent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

// storeLookupTimeout bounds the store lookup that runs as soon as the
// location is known, so a slow Maps call never stalls a turn for long.
const storeLookupTimeout = 8 * time.Second

// NearbyStores finds men's clothing stores around st.Location without
// mutating st. It returns nil when the location is unknown or the lookup
// fails; the caller decides what to do with the result.
func (r *Runner) NearbyStores(ctx context.Context, st *session.State) []shopping.Store {
	if st.Location == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, storeLookupTimeout)
	defer cancel()
	stores, _, err := r.lookupStores(ctx, st.Location, "", st.Profile.RadiusM)
	if err != nil {
		slog.WarnContext(ctx, "agent: nearby store lookup failed", "session_id", st.ID, "error", err)
		return nil
	}
	return stores
}

// lookupStores runs the store search with the same defaults and radius
// fallback as the find_nearby_stores tool: the profile radius (or the
// default one), widened to 5 km when fewer than three stores are found.
func (r *Runner) lookupStores(ctx context.Context, loc *session.Location, query string, radius int) ([]shopping.Store, int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		query = "tienda de ropa para hombre"
	}
	if radius <= 0 {
		radius = r.Deps.DefaultRadiusM
	}
	stores, err := r.Deps.Shopping.FindStores(ctx, shopping.StoreQuery{Query: query, Lat: loc.Lat, Lng: loc.Lng, RadiusM: radius, Limit: r.Deps.MaxStores})
	if err != nil {
		return nil, radius, err
	}
	used := radius
	if len(stores) < 3 && radius < 5000 {
		used = 5000
		more, err := r.Deps.Shopping.FindStores(ctx, shopping.StoreQuery{Query: query, Lat: loc.Lat, Lng: loc.Lng, RadiusM: used, Limit: r.Deps.MaxStores})
		if err == nil {
			stores = mergeStores(stores, more)
		}
	}
	return stores, used, nil
}

// maxBranchLookups caps the map searches one batch of products can trigger.
const maxBranchLookups = 4

// branchRadiusM is how far a merchant's branch may be to count as nearby.
const branchRadiusM = 5000

// findBranches looks up, near the user, the nearest branch of each merchant
// that sells the given products. The generic "tienda de ropa" search returns
// small boutiques that never match the chains Google Shopping lists (H&M,
// Liverpool, Zara...), so without this almost no product carried a nearby
// store even with the device location known. Each merchant is searched once
// per session; online-only merchants are skipped. Stores found are added to
// st.Stores, so MatchNearbyStore links the products afterwards.
func (r *Runner) findBranches(ctx context.Context, ts *turnState, products []shopping.Product) {
	ts.mu.Lock()
	st := ts.st
	if st.Location == nil {
		ts.mu.Unlock()
		return
	}
	loc := *st.Location
	if st.BranchLookups == nil {
		st.BranchLookups = map[string]bool{}
	}
	var merchants []string
	for _, p := range products {
		key := shopping.MerchantKey(p.Store)
		if len(key) < 2 || st.BranchLookups[key] || shopping.OnlineOnly(p.Store) || shopping.MatchNearbyStore(p.Store, st.Stores) != nil {
			continue
		}
		st.BranchLookups[key] = true // claimed: parallel batches do not repeat it
		merchants = append(merchants, p.Store)
		if len(merchants) == maxBranchLookups {
			break
		}
	}
	ts.mu.Unlock()
	if len(merchants) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, storeLookupTimeout)
	defer cancel()
	found := make([][]shopping.Store, len(merchants))
	var wg sync.WaitGroup
	for i, m := range merchants {
		wg.Add(1)
		go func(i int, merchant string) {
			defer wg.Done()
			stores, err := r.Deps.Shopping.FindStores(ctx, shopping.StoreQuery{Query: brand(merchant), Lat: loc.Lat, Lng: loc.Lng, RadiusM: branchRadiusM, Limit: 3})
			if err != nil {
				slog.WarnContext(ctx, "agent: branch lookup failed", "session_id", st.ID, "merchant", merchant, "error", err)
				return
			}
			for _, s := range stores {
				if shopping.StoreMatches(merchant, s.Name) && s.DistanceM <= branchRadiusM {
					found[i] = append(found[i], s)
				}
			}
		}(i, m)
	}
	wg.Wait()

	ts.mu.Lock()
	defer ts.mu.Unlock()
	added := 0
	for _, ss := range found {
		st.AddStores(ss)
		added += len(ss)
	}
	slog.InfoContext(ctx, "agent: merchant branches", "session_id", st.ID, "merchants", strings.Join(merchants, ","), "stores_found", added)
}

// brand is the chain name to search on the map ("H&M México" → "H&M",
// "Liverpool Insurgentes" → "Liverpool").
func brand(name string) string {
	fields := strings.Fields(name)
	var kept []string
	for _, f := range fields {
		switch strings.ToLower(strings.Trim(f, ".,")) {
		case "mx", "méxico", "mexico", "online", "oficial", "official", "tienda", "store", "shop":
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) == 0 {
		return name
	}
	return strings.Join(kept, " ")
}
