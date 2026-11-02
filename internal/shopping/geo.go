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

// MerchantKey is the normalised merchant name used to match stores
// ("H&M México" → "hm", "Liverpool" → "liverpool").
func MerchantKey(merchant string) string { return normalizeMerchant(merchant) }

// onlineOnly are merchants without walk-in stores in Mexico: looking for a
// nearby branch would only waste a Maps search.
var onlineOnly = []string{"amazon", "mercadolibre", "shein", "temu", "aliexpress", "ebay", "etsy", "wish", "linio", "claroshop", "dafiti", "farfetch", "asos"}

// OnlineOnly reports whether the merchant sells only online.
func OnlineOnly(merchant string) bool {
	k := MerchantKey(merchant)
	for _, o := range onlineOnly {
		if strings.Contains(k, o) {
			return true
		}
	}
	return false
}

// notRetail marks map results that carry a chain's name but are not a shop
// you can walk into to buy clothes: "Sears Car service", "Sede Liverpool",
// warehouses, offices.
var notRetail = []string{
	"car service", "automovil", "automotriz", "taller", "reparacion", "centro de servicio", "service center", "oficina", "office", "sede",
	"corporativo", "corporate", "bodega", "warehouse", "almacen general", "centro de distribucion",
	"distribution", "seguros", "insurance", "credito", "banco", "optica", "restaurante", "restaurant",
	"cafeteria", "agencia de viajes", "viajes", "estacionamiento", "parking", "outlet de muebles",
}

// retailCategories are Maps categories of places that sell clothes.
var retailCategories = []string{
	"ropa", "moda", "departamental", "department", "grandes almacenes", "clothing", "clothes", "fashion", "boutique",
	"zapater", "calzado", "shoe", "outlet", "autoservicio", "supermercado", "hipermercado",
	"supermarket", "hypermarket", "centro comercial", "shopping", "mall", "deportiv", "sportswear",
	"sastreria", "camiseria", "jeans", "uniformes",
}

// IsRetailStore reports whether a map result looks like a store that sells
// clothes: nothing in notRetail, and a clothing-related Maps category when
// the result has one ("Papelería Liverpool" is a stationery shop).
func IsRetailStore(s Store) bool {
	text := " " + accentReplacer.Replace(strings.ToLower(s.Name+" "+s.Category)) + " "
	for _, w := range notRetail {
		if strings.Contains(text, w) {
			return false
		}
	}
	cat := accentReplacer.Replace(strings.ToLower(s.Category))
	if strings.TrimSpace(cat) == "" {
		return true
	}
	for _, w := range retailCategories {
		if strings.Contains(cat, w) {
			return true
		}
	}
	return false
}

// IsBranchOf reports whether a store is a branch of the merchant: its first
// words spell the brand ("Liverpool Insurgentes", "H & M Reforma"), so
// "Papelería Liverpool" does not count.
func IsBranchOf(merchant, storeName string) bool {
	key := MerchantKey(merchant)
	if len(key) < 2 {
		return false
	}
	prefix := ""
	for _, tok := range strings.Fields(storeName) {
		prefix += normalizeName(tok)
		if prefix == key {
			return true
		}
		if len(prefix) >= len(key) {
			return false
		}
	}
	return false
}
