package shopping

import (
	"math"
	"strings"
)

// DistanceM returns the great-circle distance between two points in meters.
func DistanceM(lat1, lng1, lat2, lng2 float64) int {
	const earthRadius = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return int(math.Round(2 * earthRadius * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))))
}

// merchantNoise are tokens that appear in online merchant names but not in
// the physical store's name ("Zara MX", "H&M México", "Liverpool online").
var merchantNoise = map[string]bool{
	"mx": true, "mexico": true, "méxico": true, "online": true, "oficial": true,
	"official": true, "com": true, "tienda": true, "store": true, "shop": true,
	"latam": true, "es": true,
}

var accentReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n",
)

// normalizeName lower-cases, strips accents and keeps only letters and digits.
func normalizeName(s string) string {
	s = accentReplacer.Replace(strings.ToLower(s))
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// normalizeMerchant strips noise tokens before normalising.
func normalizeMerchant(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	var kept []string
	for _, tok := range strings.Fields(strings.ToLower(s)) {
		if !merchantNoise[tok] {
			kept = append(kept, tok)
		}
	}
	return normalizeName(strings.Join(kept, ""))
}

// MatchNearbyStore returns the closest store whose name matches the merchant.
func MatchNearbyStore(merchant string, stores []Store) *StoreRef {
	m := normalizeMerchant(merchant)
	if len(m) < 2 {
		return nil
	}
	var best *Store
	for i := range stores {
		n := normalizeName(stores[i].Name)
		if n == "" {
			continue
		}
		if strings.Contains(n, m) || (len(n) >= 3 && strings.Contains(m, n)) {
			if best == nil || stores[i].DistanceM < best.DistanceM {
				best = &stores[i]
			}
		}
	}
	if best == nil {
		return nil
	}
	return &StoreRef{PlaceID: best.PlaceID, Name: best.Name, DistanceM: best.DistanceM}
}

// AnnotateNearby fills NearbyStore on each product that matches a known store.
func AnnotateNearby(products []Product, stores []Store) {
	for i := range products {
		products[i].NearbyStore = MatchNearbyStore(products[i].Store, stores)
	}
}
