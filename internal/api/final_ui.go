package api

import (
	"strings"

	"stylerag/internal/session"
	"stylerag/internal/ui"
)

// composeFinalUI builds the generative screen of a recommendation by code,
// never from model output: the summary, the priority actions as steps, the
// total against the budget and one card per shopping item with its primary
// product (the first product id, the only one that counts for the total).
func composeFinalUI(st *session.State) []ui.Component {
	rec := st.Recommendation
	if rec == nil {
		return nil
	}
	var out []ui.Component
	if summary := strings.TrimSpace(rec.Summary); summary != "" {
		out = append(out, ui.Text(ui.RoleStylist, summary))
	}
	if len(rec.PriorityActions) > 0 {
		items := make([]ui.Step, 0, len(rec.PriorityActions))
		for _, a := range rec.PriorityActions {
			items = append(items, ui.Step{Title: a.Title, Detail: a.Description, ProductIDs: a.ProductIDs})
		}
		out = append(out, ui.Steps(items))
	}
	out = append(out, ui.BudgetBar(rec.TotalMXN, st.Profile.BudgetMXN, "Total aproximado"))
	for _, item := range rec.ShoppingList {
		if len(item.ProductIDs) == 0 {
			continue
		}
		p, ok := st.Products[item.ProductIDs[0]]
		if !ok {
			continue
		}
		image := p.LargeImage
		if image == "" {
			image = p.Thumbnail
		}
		var nearby *ui.Nearby
		if p.NearbyStore != nil {
			nearby = &ui.Nearby{Name: p.NearbyStore.Name, DistanceM: p.NearbyStore.DistanceM}
		}
		out = append(out, ui.ProductCard(p.ID, p.Title, p.Price, p.Store, image, p.Link, nearby))
	}
	return out
}
