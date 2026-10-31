package session

import (
	"regexp"
	"strings"
	"time"

	"stylerag/internal/shopping"
)

// PhasePreview means the agent proposed a style direction and waits for the
// user to pick the garment ideas he likes before searching for real products.
const PhasePreview Phase = "preview"

// Swatch is a named color the app can paint.
type Swatch struct {
	Name string `json:"name"`
	Hex  string `json:"hex,omitempty"` // #RRGGBB
	Why  string `json:"why,omitempty"`
}

// StyleBrief is the short analysis shown above the ideas: what the photo and
// color theory say about the user, in plain Spanish.
type StyleBrief struct {
	Summary     string   `json:"summary"`
	ColorSeason string   `json:"color_season,omitempty"`
	Palette     []Swatch `json:"palette"`
	Avoid       []Swatch `json:"avoid,omitempty"`
	Silhouette  string   `json:"silhouette,omitempty"`
}

// GarmentIdea is a garment type the stylist suggests, before any real product
// is chosen. Reference is a real listing that shows what it looks like; the
// final product may differ.
type GarmentIdea struct {
	ID          string            `json:"id"`
	Slot        string            `json:"slot"`
	Garment     string            `json:"garment"`
	Colors      []Swatch          `json:"colors"`
	Textures    []string          `json:"textures,omitempty"`
	Fit         string            `json:"fit,omitempty"`
	Why         string            `json:"why"`
	PriceMinMXN float64           `json:"price_min_mxn,omitempty"`
	PriceMaxMXN float64           `json:"price_max_mxn,omitempty"`
	Reference   *shopping.Product `json:"reference,omitempty"`
}

// Direction is the preview step: the brief, the ideas and what the user
// picked. Selected and Rejected are the feedback signal worth measuring.
type Direction struct {
	Brief     StyleBrief    `json:"brief"`
	Ideas     []GarmentIdea `json:"ideas"`
	Round     int           `json:"round"` // 1 for the first proposal, +1 per re-proposal
	Selected  []string      `json:"selected,omitempty"`
	Rejected  []string      `json:"rejected,omitempty"`
	Feedback  string        `json:"feedback,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	ChosenAt  *time.Time    `json:"chosen_at,omitempty"`
}

// Idea returns the idea with id, if any.
func (d *Direction) Idea(id string) (GarmentIdea, bool) {
	for _, i := range d.Ideas {
		if i.ID == id {
			return i, true
		}
	}
	return GarmentIdea{}, false
}

// Choose records the user's pick. Unknown ids are ignored; every idea not
// selected counts as rejected. It returns the selected ideas.
func (d *Direction) Choose(ids []string, feedback string, now time.Time) []GarmentIdea {
	want := map[string]bool{}
	for _, id := range ids {
		want[strings.TrimSpace(id)] = true
	}
	d.Selected, d.Rejected = nil, nil
	var picked []GarmentIdea
	for _, i := range d.Ideas {
		if want[i.ID] {
			d.Selected = append(d.Selected, i.ID)
			picked = append(picked, i)
		} else {
			d.Rejected = append(d.Rejected, i.ID)
		}
	}
	d.Feedback = strings.TrimSpace(feedback)
	d.ChosenAt = &now
	return picked
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// CleanSwatches drops empty names and invalid hex codes (the name stays).
func CleanSwatches(in []Swatch) []Swatch {
	out := make([]Swatch, 0, len(in))
	for _, s := range in {
		s.Name, s.Hex, s.Why = strings.TrimSpace(s.Name), strings.ToUpper(strings.TrimSpace(s.Hex)), strings.TrimSpace(s.Why)
		if s.Name == "" {
			continue
		}
		if !hexColor.MatchString(s.Hex) {
			s.Hex = ""
		}
		out = append(out, s)
	}
	return out
}
