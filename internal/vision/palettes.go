package vision

import "strings"

// Swatch is a named colour for the palette the app shows.
type Swatch struct {
	Name string
	Hex  string
}

// SeasonPalette is what the app shows the user about his colour season,
// written for a man who knows nothing about colorimetry.
type SeasonPalette struct {
	Season string   // canonical English name, as in ColorSeasonReference
	Name   string   // Spanish name shown to the user
	Why    string   // one sentence: what it means for him
	Best   []Swatch // colours that favour him
	Avoid  []Swatch // colours that wash him out
}

// seasonPalettes follows ColorSeasonReference: the same favoured and avoided
// colours, with a hex for each so the app can paint them.
var seasonPalettes = map[string]SeasonPalette{
	"Light Spring": {Name: "Primavera clara", Why: "Tu piel tiene subtono cálido y poco contraste: los colores claros y cálidos te iluminan, los oscuros intensos te endurecen.",
		Best:  []Swatch{{"Durazno", "#F6B98F"}, {"Coral claro", "#F59A86"}, {"Amarillo suave", "#F5DC7A"}, {"Verde menta", "#A8D8B9"}, {"Azul claro", "#9CC7E8"}, {"Beige cálido", "#E8D3B0"}},
		Avoid: []Swatch{{"Negro puro", "#111111"}, {"Borgoña oscuro", "#5A1626"}}},
	"Bright Spring": {Name: "Primavera brillante", Why: "Tu piel es cálida y con contraste: los colores vivos y limpios te dan presencia, los apagados te opacan.",
		Best:  []Swatch{{"Rojo brillante", "#E2302F"}, {"Verde esmeralda", "#0F9D58"}, {"Azul rey", "#2453B8"}, {"Coral vivo", "#FF6F59"}, {"Turquesa", "#12B5B0"}, {"Amarillo", "#F7C531"}},
		Avoid: []Swatch{{"Beige apagado", "#CBBFA8"}, {"Tonos tierra", "#7A5C45"}}},
	"True Spring": {Name: "Primavera cálida", Why: "Tu piel tiene un subtono dorado: los colores cálidos y alegres te favorecen, los fríos y grises te apagan.",
		Best:  []Swatch{{"Coral", "#F47C5F"}, {"Dorado", "#D9A93B"}, {"Verde cálido", "#6FA33A"}, {"Turquesa", "#1FB3A6"}, {"Rojo tomate", "#E2492F"}, {"Camel", "#C19A6B"}},
		Avoid: []Swatch{{"Gris frío", "#9AA3AD"}, {"Azul marino", "#1F2A44"}, {"Borgoña", "#6D1A2B"}}},
	"True Autumn": {Name: "Otoño cálido", Why: "Tu piel tiene un subtono cálido y profundo: los tonos tierra y especias te iluminan, los fríos y plateados te apagan.",
		Best:  []Swatch{{"Naranja quemado", "#C8621B"}, {"Terracota", "#B5562F"}, {"Verde bosque", "#2F5233"}, {"Dorado", "#C99A2E"}, {"Chocolate", "#5A3A22"}, {"Mostaza", "#C9A227"}},
		Avoid: []Swatch{{"Rosa frío", "#E7A1C0"}, {"Gris azulado", "#8C9AAB"}, {"Plateado", "#C0C4C8"}}},
	"Soft Autumn": {Name: "Otoño suave", Why: "Tu piel es cálida y de contraste suave: los colores tierra apagados te favorecen, el negro puro y los neón te endurecen.",
		Best:  []Swatch{{"Verde oliva", "#6B7445"}, {"Terracota suave", "#C07A5A"}, {"Topo", "#8B7D6B"}, {"Dorado suave", "#C8A75A"}, {"Camel", "#B5895A"}, {"Salvia", "#9AA582"}},
		Avoid: []Swatch{{"Negro puro", "#111111"}, {"Colores neón", "#39FF14"}, {"Rosa frío", "#E7A1C0"}}},
	"Deep Autumn": {Name: "Otoño profundo", Why: "Tu piel es cálida y con contraste alto: los tonos cálidos y profundos te dan presencia, los pasteles te deslavan.",
		Best:  []Swatch{{"Borgoña", "#6D1A2B"}, {"Verde bosque", "#233D2B"}, {"Marrón cálido", "#6B4226"}, {"Bronce", "#A0692E"}, {"Verde botella", "#2E4A3B"}, {"Mostaza oscuro", "#B08A1E"}},
		Avoid: []Swatch{{"Rosa pastel", "#F3D1DC"}, {"Celeste lavado", "#CFE3EE"}}},
	"Light Summer": {Name: "Verano claro", Why: "Tu piel tiene subtono frío y poco contraste: los tonos claros y suaves te favorecen, los cálidos intensos te opacan.",
		Best:  []Swatch{{"Lavanda", "#B9A7D6"}, {"Rosa pastel", "#F2C1CF"}, {"Azul cielo", "#9DB8D9"}, {"Gris perla", "#D3D6DB"}, {"Menta", "#B3DCC9"}, {"Azul polvo", "#A9BCD0"}},
		Avoid: []Swatch{{"Naranja", "#E8741E"}, {"Dorado", "#D4A017"}}},
	"True Summer": {Name: "Verano frío", Why: "Tu piel tiene un subtono frío: los azules, grises y rosas suaves te favorecen, los cálidos dorados te apagan.",
		Best:  []Swatch{{"Azul medio", "#4F79A8"}, {"Rosa ceniza", "#C69AA6"}, {"Gris azulado", "#7D8FA3"}, {"Ciruela suave", "#8C5A7A"}, {"Azul marino suave", "#3B4E6B"}, {"Menta fría", "#8FC1B5"}},
		Avoid: []Swatch{{"Naranja", "#E8741E"}, {"Amarillo dorado", "#D4A017"}, {"Marrón cálido", "#7A4A2A"}}},
	"Soft Summer": {Name: "Verano suave", Why: "Tu piel es fría y de contraste suave: los colores grisáceos y polvosos te favorecen, los brillantes te opacan.",
		Best:  []Swatch{{"Gris suave", "#A7A9AC"}, {"Malva", "#A88BA0"}, {"Verde salvia", "#9CAF88"}, {"Azul mezclilla", "#5B7A99"}, {"Rosa polvo", "#D4A5A5"}, {"Topo frío", "#8A8580"}},
		Avoid: []Swatch{{"Rojo vivo", "#E2302F"}, {"Naranja", "#E8741E"}}},
	"Deep Winter": {Name: "Invierno profundo", Why: "Tu piel es fría y con contraste alto: los colores oscuros e intensos te dan presencia, los tonos tierra cálidos te apagan.",
		Best:  []Swatch{{"Negro", "#151515"}, {"Borgoña", "#6D1A2B"}, {"Verde esmeralda", "#046A4C"}, {"Azul marino", "#14213D"}, {"Blanco", "#FFFFFF"}, {"Ciruela", "#5B2245"}},
		Avoid: []Swatch{{"Naranja", "#E8741E"}, {"Dorado", "#D4A017"}, {"Tonos tierra", "#8B5A2B"}}},
	"True Winter": {Name: "Invierno frío", Why: "Tu piel es fría y con mucho contraste: los colores puros y nítidos te favorecen, los apagados y beige te deslavan.",
		Best:  []Swatch{{"Blanco", "#FFFFFF"}, {"Negro", "#151515"}, {"Rojo cereza", "#B0102A"}, {"Azul rey", "#2453B8"}, {"Magenta", "#B0247E"}, {"Gris carbón", "#36454F"}},
		Avoid: []Swatch{{"Beige", "#D8C7A5"}, {"Marrón", "#7A4A2A"}, {"Topo", "#8B7D6B"}}},
	"Bright Winter": {Name: "Invierno brillante", Why: "Tu piel es fría y muy contrastada: los colores vivos y eléctricos te dan presencia, los apagados te opacan.",
		Best:  []Swatch{{"Fucsia", "#D6247A"}, {"Azul eléctrico", "#1F5BFF"}, {"Verde esmeralda", "#00A36C"}, {"Rojo puro", "#D7192A"}, {"Negro", "#151515"}, {"Blanco", "#FFFFFF"}},
		Avoid: []Swatch{{"Tonos tierra", "#8B5A2B"}, {"Pasteles sucios", "#C9B8A8"}}},
}

// PaletteFor maps the analyzer's season (English like "Deep Autumn", or a
// Spanish wording) to its palette. ok is false when the season is unknown.
func PaletteFor(season string) (SeasonPalette, bool) {
	s := strings.ToLower(season)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	deep := has("deep", "dark", "profund", "oscur")
	light := has("light", "clar")
	soft := has("soft", "muted", "suave", "apagad")
	bright := has("bright", "clear", "brillant", "vivo", "puro", "pura")
	var key string
	switch {
	case has("spring", "primavera"):
		key = pick(light, "Light Spring", bright, "Bright Spring", "True Spring")
	case has("autumn", "fall", "otoño", "otono"):
		key = pick(soft, "Soft Autumn", deep, "Deep Autumn", "True Autumn")
	case has("summer", "verano"):
		key = pick(light, "Light Summer", soft, "Soft Summer", "True Summer")
	case has("winter", "invierno"):
		key = pick(deep, "Deep Winter", bright, "Bright Winter", "True Winter")
	default:
		return SeasonPalette{}, false
	}
	p := seasonPalettes[key]
	p.Season = key
	return p, true
}

func pick(a bool, ak string, b bool, bk, fallback string) string {
	switch {
	case a:
		return ak
	case b:
		return bk
	}
	return fallback
}
