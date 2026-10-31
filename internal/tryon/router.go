package tryon

import (
	"context"
	"sort"
	"strings"
)

// SlotRouter sends each garment to the provider configured for its slot and
// everything else to Default. It lets a cheap provider handle the swipe grid
// while a pricier one renders only the garments the cheap one gets wrong
// (pants, Oct 2026). Wrap each provider in its own BudgetedVTON.
type SlotRouter struct {
	Default VTONProvider
	BySlot  map[string]VTONProvider
}

func (s *SlotRouter) Generate(ctx context.Context, req VTONRequest) (*VTONResult, error) {
	return s.For(req.Category).Generate(ctx, req)
}

// For returns the provider that renders slot.
func (s *SlotRouter) For(slot string) VTONProvider {
	if p, ok := s.BySlot[slot]; ok && p != nil {
		return p
	}
	return s.Default
}

// Name lists the providers, default first, e.g. "fashn+lower_body:google_vertex".
func (s *SlotRouter) Name() string {
	parts := []string{s.Default.Name()}
	slots := make([]string, 0, len(s.BySlot))
	for slot := range s.BySlot {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	for _, slot := range slots {
		parts = append(parts, slot+":"+s.BySlot[slot].Name())
	}
	return strings.Join(parts, "+")
}
