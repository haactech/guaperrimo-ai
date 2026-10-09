package shopping

import (
	"context"
	"fmt"
	"strings"
)

// Fake is a deterministic provider for local development and tests. It
// returns plausible Mexican menswear results without touching the network.
type Fake struct{}

func (Fake) Name() string { return "fake" }

var fakeMerchants = []struct {
	name  string
	price float64
}{
	{"Zara MX", 899},
	{"H&M México", 549},
	{"Liverpool", 1299},
	{"Bershka", 699},
	{"Mercado Libre", 459},
}

// SearchProducts returns a few products whose titles echo the query.
func (Fake) SearchProducts(_ context.Context, q ProductQuery) ([]Product, error) {
	limit := q.Limit
	if limit <= 0 || limit > len(fakeMerchants) {
		limit = len(fakeMerchants)
	}
	base := strings.TrimSpace(q.Query)
	if base == "" {
		base = "prenda"
	}
	lower := strings.ToLower(base)
	var out []Product
	for i, m := range fakeMerchants {
		if first := strings.ToLower(strings.Fields(m.name)[0]); onlyMerchant(lower) && !strings.HasPrefix(lower, first) {
			continue
		}
		price := m.price
		if q.MaxPrice > 0 && price > q.MaxPrice {
			price = q.MaxPrice * (0.6 + 0.08*float64(i))
		}
		p := Product{
			ID:        fmt.Sprintf("p_fake%d_%s", i, shortHash(base)[:6]),
			Title:     fmt.Sprintf("%s (%s)", capitalize(base), m.name),
			Store:     m.name,
			Price:     float64(int(price)),
			Currency:  "MXN",
			Link:      fmt.Sprintf("https://example.com/%s/%d", shortHash(m.name)[:6], i),
			Thumbnail: fmt.Sprintf("https://picsum.photos/seed/%s%d/400/500", shortHash(base)[:4], i),
			Delivery:  "Envío gratis",
		}
		out = append(out, p)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// FindStores returns fixed stores around the requested point.
func (Fake) FindStores(_ context.Context, q StoreQuery) ([]Store, error) {
	seed := []struct {
		name, addr string
		dlat, dlng float64
	}{
		{"Zara", "Av. Insurgentes Sur 1", 0.0030, 0.0020},
		{"H&M", "Plaza local 2", -0.0050, 0.0035},
		{"Liverpool Insurgentes", "Av. Insurgentes Sur 1310", 0.0090, -0.0060},
		{"Bershka", "Centro comercial 3", -0.0120, 0.0100},
		{"Pull&Bear", "Centro comercial 3", -0.0125, 0.0105},
	}
	var out []Store
	for i, s := range seed {
		lat, lng := q.Lat+s.dlat, q.Lng+s.dlng
		st := Store{
			PlaceID:   fmt.Sprintf("fake_place_%d", i),
			Name:      s.name,
			Address:   s.addr,
			Lat:       lat,
			Lng:       lng,
			DistanceM: DistanceM(q.Lat, q.Lng, lat, lng),
			Rating:    4.2,
			Reviews:   120 + i*10,
			OpenState: "Abierto",
			Category:  "Tienda de ropa",
		}
		if q.RadiusM > 0 && st.DistanceM > q.RadiusM {
			continue
		}
		out = append(out, st)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// Geocode always resolves to Roma Norte, Mexico City.
func (Fake) Geocode(_ context.Context, query string) (*Geo, error) {
	return &Geo{Lat: 19.4194, Lng: -99.1616, Label: strings.TrimSpace(query) + " (aprox.)"}, nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// onlyMerchant reports whether the query is directed at one of the fake merchants.
func onlyMerchant(lowerQuery string) bool {
	for _, m := range fakeMerchants {
		if strings.HasPrefix(lowerQuery, strings.ToLower(strings.Fields(m.name)[0])) {
			return true
		}
	}
	return false
}

// ProductDetails returns no extra data; the renderer falls back to the thumbnail.
func (Fake) ProductDetails(_ context.Context, _ string) (*ProductDetails, error) {
	return &ProductDetails{}, nil
}
