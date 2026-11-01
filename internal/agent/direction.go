package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

const (
	// The preview offers ideasPerSlot alternatives for each garment of the
	// outfit; a proposal with fewer than minPerSlot in any of them is sent
	// back to the model.
	ideasPerSlot = 3
	minPerSlot   = 2
	// referenceTimeout bounds each reference-photo search; a slow one just
	// leaves its idea without a photo.
	referenceTimeout = 15 * time.Second
)

type proposeDirectionArgs struct {
	Message string `json:"message"`
	Brief   struct {
		Summary     string           `json:"summary"`
		ColorSeason string           `json:"color_season"`
		Palette     []session.Swatch `json:"palette"`
		Avoid       []session.Swatch `json:"avoid"`
		Silhouette  string           `json:"silhouette"`
	} `json:"brief"`
	Ideas []struct {
		Slot        string           `json:"slot"`
		Garment     string           `json:"garment"`
		Colors      []session.Swatch `json:"colors"`
		Textures    []string         `json:"textures"`
		Fit         string           `json:"fit"`
		Why         string           `json:"why"`
		PriceMinMXN float64          `json:"price_min_mxn"`
		PriceMaxMXN float64          `json:"price_max_mxn"`
	} `json:"ideas"`
}

// ideaSlots are the garments of a complete outfit, in the order the preview
// shows them: shirt, pants, jacket. Accessories and footwear are not offered:
// the try-on renders whole outfits, and a belt alone is not worth an image.
var ideaSlots = []string{"upper_body", "lower_body", "outerwear"}

func isIdeaSlot(slot string) bool {
	for _, s := range ideaSlots {
		if s == slot {
			return true
		}
	}
	return false
}

// buildDirection validates the model's proposal. It returns an error message
// for the model when the proposal is unusable.
func buildDirection(a proposeDirectionArgs, now time.Time) (*session.Direction, string) {
	if strings.TrimSpace(a.Message) == "" {
		return nil, "falta message"
	}
	if strings.TrimSpace(a.Brief.Summary) == "" {
		return nil, "falta brief.summary: explica en 2-3 frases qué le favorece según su foto y colorimetría"
	}
	d := &session.Direction{
		Brief: session.StyleBrief{
			Summary:     strings.TrimSpace(a.Brief.Summary),
			ColorSeason: strings.TrimSpace(a.Brief.ColorSeason),
			Palette:     session.CleanSwatches(a.Brief.Palette),
			Avoid:       session.CleanSwatches(a.Brief.Avoid),
			Silhouette:  strings.TrimSpace(a.Brief.Silhouette),
		},
		CreatedAt: now,
	}
	bySlot := map[string][]session.GarmentIdea{}
	for _, in := range a.Ideas {
		slot := strings.TrimSpace(in.Slot)
		garment := strings.TrimSpace(in.Garment)
		if !isIdeaSlot(slot) || garment == "" || len(bySlot[slot]) == ideasPerSlot {
			continue // accessories, footwear, unknown slots and extras are dropped
		}
		idea := session.GarmentIdea{
			Slot:        slot,
			Garment:     garment,
			Colors:      session.CleanSwatches(in.Colors),
			Fit:         strings.TrimSpace(in.Fit),
			Why:         strings.TrimSpace(in.Why),
			PriceMinMXN: in.PriceMinMXN,
			PriceMaxMXN: in.PriceMaxMXN,
		}
		for _, t := range in.Textures {
			if t = strings.TrimSpace(t); t != "" {
				idea.Textures = append(idea.Textures, t)
			}
		}
		bySlot[slot] = append(bySlot[slot], idea)
	}
	var missing []string
	for _, slot := range ideaSlots {
		if len(bySlot[slot]) < minPerSlot {
			missing = append(missing, slot)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Sprintf("propón %d ideas de cada prenda del outfit: upper_body (camisa/playera/polo/suéter), lower_body (pantalón) y outerwear (saco/blazer/chamarra), distintas en color o textura; faltan ideas de %s. Nunca accesorios ni calzado.", ideasPerSlot, strings.Join(missing, ", "))
	}
	// Ids follow the preview order: shirts, then pants, then jackets.
	for _, slot := range ideaSlots {
		for _, idea := range bySlot[slot] {
			idea.ID = fmt.Sprintf("idea_%d", len(d.Ideas)+1)
			d.Ideas = append(d.Ideas, idea)
		}
	}
	return d, ""
}

// referenceQuery is the search for an idea's example photo.
func referenceQuery(i session.GarmentIdea) string {
	parts := []string{i.Garment}
	if len(i.Colors) > 0 && !strings.Contains(strings.ToLower(i.Garment), strings.ToLower(i.Colors[0].Name)) {
		parts = append(parts, i.Colors[0].Name)
	}
	if !strings.Contains(strings.ToLower(i.Garment), "hombre") {
		parts = append(parts, "hombre")
	}
	return strings.Join(parts, " ")
}

// attachReferences finds one real listing with a photo per idea, in parallel.
// Ideas whose search fails or times out simply have no reference.
func (r *Runner) attachReferences(ctx context.Context, d *session.Direction) {
	if timeLeft(ctx) < referenceTimeout/2 {
		slog.WarnContext(ctx, "agent: no time left for reference photos")
		return
	}
	refs := make([]*shopping.Product, len(d.Ideas))
	var wg sync.WaitGroup
	for i, idea := range d.Ideas {
		wg.Add(1)
		go func(i int, idea session.GarmentIdea) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, referenceTimeout)
			defer cancel()
			q := shopping.ProductQuery{Query: referenceQuery(idea), MaxPrice: idea.PriceMaxMXN, Location: r.Deps.DefaultLocation, Limit: 10}
			ps, err := r.Deps.Shopping.SearchProducts(sctx, q)
			if err == nil && len(ps) == 0 && q.MaxPrice > 0 {
				q.MaxPrice = 0 // an example photo is still useful above the price range
				ps, err = r.Deps.Shopping.SearchProducts(sctx, q)
			}
			if err != nil {
				slog.WarnContext(ctx, "agent: reference search failed", "idea", idea.ID, "query", q.Query, "error", err)
				return
			}
			refs[i] = bestReference(idea, ps)
		}(i, idea)
	}
	wg.Wait()
	for i, p := range refs {
		d.Ideas[i].Reference = p
	}
}

// bestReference picks the listing whose title names the idea's color (and
// fabric), so the photo shows what the user is choosing; Google Shopping
// often ranks other colors first. Ties keep the search order.
func bestReference(idea session.GarmentIdea, ps []shopping.Product) *shopping.Product {
	var best *shopping.Product
	bestScore := -1
	for i := range ps {
		p := ps[i]
		if p.Currency != "MXN" || (p.Thumbnail == "" && p.LargeImage == "") {
			continue
		}
		title := strings.ToLower(p.Title)
		score := 0
		for _, c := range idea.Colors {
			if words := significantWords(c.Name); len(words) > 0 && containsAll(title, words) {
				score += 3
				break
			}
		}
		for _, t := range idea.Textures {
			if words := significantWords(t); len(words) > 0 && containsAll(title, words[:1]) {
				score++
				break
			}
		}
		if score > bestScore {
			best, bestScore = &p, score
		}
	}
	return best
}

// significantWords lowercases s and drops connectors ("de", "o", "con"...).
func significantWords(s string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if len([]rune(w)) > 2 && w != "con" && w != "del" && w != "las" && w != "los" {
			out = append(out, w)
		}
	}
	return out
}

func containsAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// selectionText turns the user's pick into the message the model reads.
func selectionText(d *session.Direction, picked []session.GarmentIdea) string {
	var sb strings.Builder
	sb.WriteString("[selección] ")
	if len(picked) == 0 {
		sb.WriteString("No eligió ninguna idea.")
	} else {
		sb.WriteString("Le gustaron: ")
		for i, p := range picked {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(describeIdea(p))
		}
		sb.WriteString(".")
	}
	var rejected []string
	for _, id := range d.Rejected {
		if idea, ok := d.Idea(id); ok {
			rejected = append(rejected, describeIdea(idea))
		}
	}
	if len(rejected) > 0 {
		sb.WriteString(" No le llamaron la atención: " + strings.Join(rejected, "; ") + ".")
	}
	if d.Feedback != "" {
		sb.WriteString(" Comentario: " + d.Feedback)
	}
	return sb.String()
}

func describeIdea(i session.GarmentIdea) string {
	s := i.ID + " (" + i.Slot + ": " + i.Garment
	var colors []string
	for _, c := range i.Colors {
		colors = append(colors, c.Name)
	}
	if len(colors) > 0 {
		s += ", " + strings.Join(colors, "/")
	}
	if len(i.Textures) > 0 {
		s += ", " + strings.Join(i.Textures, "/")
	}
	if i.Reference != nil {
		s += ", referencia " + i.Reference.ID
	}
	return s + ")"
}
