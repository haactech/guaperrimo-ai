package agent

import (
	"context"
	"log/slog"
	"strings"
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
