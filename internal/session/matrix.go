package session

import (
	"sort"
	"strings"
	"time"

	"stylerag/internal/shopping"
)

// SlotOrder is the chain order for virtual try-on: inner layers first so the
// next garment renders on top of the previous result.
// Pants are not a swipe row: FASHN leaves them unchanged when the legs are
// cut off (Oct 2026). Vertex does replace them, but costs about 14 times more
// per image, so pants are rendered once, last, as the "complete look" on top
// of the chosen grid combination (see Matrix.Bottoms and CompleteCombo).
var SlotOrder = []string{"upper_body", "outerwear"}

// BottomSlot is the slot rendered only in the complete look.
const BottomSlot = "lower_body"

// SlotLabels are the Spanish row titles shown in the app.
var SlotLabels = map[string]string{
	"upper_body": "Arriba",
	"outerwear":  "Encima",
	"lower_body": "Abajo",
}

// SlotRank orders slots for chaining; unknown slots go last.
func SlotRank(slot string) int {
	for i, s := range SlotOrder {
		if s == slot {
			return i
		}
	}
	return len(SlotOrder)
}

// MatrixOption is one alternative inside a row.
type MatrixOption struct {
	ProductID string `json:"product_id"`
	Why       string `json:"why,omitempty"`
	Priority  int    `json:"priority,omitempty"`
}

// MatrixSlot is a swipeable row.
type MatrixSlot struct {
	Slot    string         `json:"slot"`
	Label   string         `json:"label"`
	Options []MatrixOption `json:"options"`
}

// Matrix is the mix-and-match grid built from the recommendation.
type Matrix struct {
	Slots     []MatrixSlot      `json:"slots"`
	Default   map[string]string `json:"default"`           // slot -> product id
	Bottoms   []MatrixOption    `json:"bottoms,omitempty"` // pants for the complete look
	CreatedAt time.Time         `json:"created_at"`
}

// Selection maps slot -> product id.
type Selection map[string]string

// ComboPiece is one garment of a combination, in chain order.
type ComboPiece struct {
	Slot      string `json:"slot"`
	ProductID string `json:"product_id"`
}

// Combo returns the selection as ordered pieces, skipping slots not in the matrix.
func (m *Matrix) Combo(sel Selection) []ComboPiece {
	var out []ComboPiece
	for _, s := range m.Slots {
		id, ok := sel[s.Slot]
		if !ok || id == "" {
			continue
		}
		out = append(out, ComboPiece{Slot: s.Slot, ProductID: id})
	}
	sort.SliceStable(out, func(i, j int) bool { return SlotRank(out[i].Slot) < SlotRank(out[j].Slot) })
	return out
}

// Validate checks that every selected id is an option of its slot and that
// every slot has a selection. It returns the normalised selection.
func (m *Matrix) Validate(sel Selection) (Selection, string) {
	out := Selection{}
	for _, s := range m.Slots {
		id := strings.TrimSpace(sel[s.Slot])
		if id == "" {
			id = m.Default[s.Slot]
		}
		found := false
		for _, o := range s.Options {
			if o.ProductID == id {
				found = true
				break
			}
		}
		if !found {
			return nil, "selection for slot " + s.Slot + " is not one of its options"
		}
		out[s.Slot] = id
	}
	return out, ""
}

// CompleteCombo is the grid combination with the pants appended last, so the
// cached grid render is reused and only the pants cost a new image. bottomID
// defaults to the first option.
func (m *Matrix) CompleteCombo(sel Selection, bottomID string) ([]ComboPiece, string) {
	if len(m.Bottoms) == 0 {
		return nil, "this outfit has no pants to try on"
	}
	bottomID = strings.TrimSpace(bottomID)
	if bottomID == "" {
		bottomID = m.Bottoms[0].ProductID
	}
	for _, o := range m.Bottoms {
		if o.ProductID == bottomID {
			return append(m.Combo(sel), ComboPiece{Slot: BottomSlot, ProductID: bottomID}), ""
		}
	}
	return nil, "lower_body is not one of the pants options"
}

// Neighbors returns every selection that differs from sel in exactly one slot:
// the combinations one swipe away.
func (m *Matrix) Neighbors(sel Selection) []Selection {
	var out []Selection
	for _, s := range m.Slots {
		for _, o := range s.Options {
			if o.ProductID == sel[s.Slot] {
				continue
			}
			n := Selection{}
			for k, v := range sel {
				n[k] = v
			}
			n[s.Slot] = o.ProductID
			out = append(out, n)
		}
	}
	return out
}

// ComboKey is the cache key of a (prefix of a) combination.
func ComboKey(pieces []ComboPiece) string {
	ids := make([]string, len(pieces))
	for i, p := range pieces {
		ids[i] = p.ProductID
	}
	return strings.Join(ids, "|")
}

// BuildMatrix derives the grid from the recommendation: products with an
// image, grouped by slot, principal option first, at most maxPerSlot each.
// Accessories are left out because try-on cannot render them.
func BuildMatrix(st *State, maxPerSlot int) *Matrix {
	if st.Recommendation == nil {
		return nil
	}
	if maxPerSlot <= 0 {
		maxPerSlot = 3
	}
	type cand struct {
		opt      MatrixOption
		order    int
		priority int
	}
	bySlot := map[string][]cand{}
	seen := map[string]bool{}
	order := 0
	add := func(slot, id, why string, priority int) {
		if (SlotRank(slot) >= len(SlotOrder) && slot != BottomSlot) || seen[slot+"|"+id] {
			return
		}
		p, ok := st.Products[id]
		if !ok || (p.Thumbnail == "" && p.LargeImage == "") {
			return
		}
		seen[slot+"|"+id] = true
		if priority <= 0 {
			priority = 99
		}
		bySlot[slot] = append(bySlot[slot], cand{opt: MatrixOption{ProductID: id, Why: why, Priority: priority}, order: order, priority: priority})
		order++
	}
	for _, item := range st.Recommendation.ShoppingList {
		for _, id := range item.ProductIDs {
			add(item.Slot, id, item.Why, item.Priority)
		}
	}
	for _, l := range st.Looks {
		for _, piece := range l.Pieces {
			add(piece.Slot, piece.ProductID, "", 50)
		}
	}

	ranked := func(slot string) []MatrixOption {
		cands := bySlot[slot]
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].priority != cands[j].priority {
				return cands[i].priority < cands[j].priority
			}
			return cands[i].order < cands[j].order
		})
		if len(cands) > maxPerSlot {
			cands = cands[:maxPerSlot]
		}
		var out []MatrixOption
		for _, c := range cands {
			out = append(out, c.opt)
		}
		return out
	}

	m := &Matrix{Default: map[string]string{}, CreatedAt: time.Now()}
	for _, slot := range SlotOrder {
		opts := ranked(slot)
		if len(opts) == 0 {
			continue
		}
		m.Slots = append(m.Slots, MatrixSlot{Slot: slot, Label: SlotLabels[slot], Options: opts})
		m.Default[slot] = opts[0].ProductID
	}
	m.Bottoms = ranked(BottomSlot)
	if len(m.Slots) == 0 {
		return nil
	}
	return m
}

// Render is the try-on image of a combination prefix.
type Render struct {
	Key          string       `json:"key"`
	Pieces       []ComboPiece `json:"pieces"`
	Status       LookStatus   `json:"status"`
	ImageKey     string       `json:"image_key,omitempty"`
	ImageURL     string       `json:"image_url,omitempty"`
	Error        string       `json:"error,omitempty"`
	GenerationMs int64        `json:"generation_time_ms,omitempty"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// SavedItem is one garment of a saved look with its explanation.
type SavedItem struct {
	Slot    string           `json:"slot"`
	Product shopping.Product `json:"product"`
	Why     string           `json:"why,omitempty"`
}

// SavedLook is a combination the user chose to keep for shopping.
type SavedLook struct {
	ID        string           `json:"id"`
	Key       string           `json:"key"`
	Selection Selection        `json:"selection"`
	ImageURL  string           `json:"image_url,omitempty"`
	Items     []SavedItem      `json:"items"`
	TotalMXN  float64          `json:"total_mxn"`
	Stores    []shopping.Store `json:"stores"`
	Note      string           `json:"note,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}
