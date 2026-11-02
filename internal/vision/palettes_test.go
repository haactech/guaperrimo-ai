package vision

import (
	"regexp"
	"testing"
)

func TestPaletteForMapsEverySeason(t *testing.T) {
	for in, want := range map[string]string{
		"Deep Autumn": "Deep Autumn", "Soft Autumn": "Soft Autumn", "True Autumn": "True Autumn", "Autumn": "True Autumn",
		"Light Spring": "Light Spring", "Bright Spring": "Bright Spring", "Clear Spring": "Bright Spring", "Spring": "True Spring",
		"Light Summer": "Light Summer", "Soft Summer": "Soft Summer", "Summer": "True Summer",
		"Deep Winter": "Deep Winter", "Bright Winter": "Bright Winter", "Winter": "True Winter",
		"Otoño profundo": "Deep Autumn", "verano suave": "Soft Summer", "Invierno brillante": "Bright Winter",
	} {
		p, ok := PaletteFor(in)
		if !ok || p.Season != want {
			t.Errorf("PaletteFor(%q) = %q %v, want %q", in, p.Season, ok, want)
		}
	}
	if _, ok := PaletteFor("no sé"); ok {
		t.Error("an unknown season must not get a palette")
	}
}

func TestEveryPaletteIsComplete(t *testing.T) {
	hex := regexp.MustCompile(`^#[0-9A-F]{6}$`)
	if len(seasonPalettes) != 12 {
		t.Fatalf("want the 12 seasons, got %d", len(seasonPalettes))
	}
	for season, p := range seasonPalettes {
		if p.Name == "" || p.Why == "" || len(p.Best) < 5 || len(p.Avoid) < 2 {
			t.Errorf("%s is incomplete: %+v", season, p)
		}
		for _, s := range append(append([]Swatch{}, p.Best...), p.Avoid...) {
			if s.Name == "" || !hex.MatchString(s.Hex) {
				t.Errorf("%s: bad swatch %+v", season, s)
			}
		}
	}
}
