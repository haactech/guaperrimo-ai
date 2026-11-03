package shopping

import "strings"

// notMenswear are title words of listings for women or children. Google
// Shopping returns them even for queries that say "hombre", and the app is
// for men: they never reach the stylist, the preview or the try-on.
var notMenswear = map[string]bool{
	"mujer": true, "mujeres": true, "dama": true, "damas": true, "femenino": true, "femenina": true,
	"femeninos": true, "femeninas": true, "women": true, "womens": true, "woman": true, "ladies": true,
	"lady": true, "girl": true, "girls": true, "nina": true, "ninas": true, "nino": true, "ninos": true,
	"infantil": true, "kids": true, "kid": true, "boys": true, "boy": true, "bebe": true, "bebes": true,
	"blusa": true, "blusas": true, "vestido": true, "vestidos": true, "falda": true, "faldas": true,
	"brasier": true, "sosten": true, "bralette": true, "leggings": true, "maternidad": true,
	"embarazada": true, "crop": true,
}

// IsMenswear reports whether a listing title is fine for a man: no word that
// marks it as women's or children's clothing.
func IsMenswear(title string) bool {
	t := accentReplacer.Replace(strings.ToLower(title))
	words := strings.FieldsFunc(t, func(r rune) bool { return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') })
	for _, w := range words {
		if notMenswear[w] {
			return false
		}
	}
	return !strings.Contains(t, "para ella")
}

// ForMen keeps only the listings that are fine for a man.
func ForMen(ps []Product) []Product {
	out := ps[:0:0]
	for _, p := range ps {
		if IsMenswear(p.Title) {
			out = append(out, p)
		}
	}
	return out
}

// MensQuery makes sure a product search asks for men's clothing.
func MensQuery(q string) string {
	l := accentReplacer.Replace(strings.ToLower(q))
	if strings.Contains(l, "hombre") || strings.Contains(l, "caballero") || strings.Contains(l, "masculin") {
		return q
	}
	return strings.TrimSpace(q) + " hombre"
}
