package tryon

import (
	"context"
	"testing"
)

type namedFake struct {
	name  string
	calls int
}

func (n *namedFake) Name() string { return n.name }
func (n *namedFake) Generate(context.Context, VTONRequest) (*VTONResult, error) {
	n.calls++
	return &VTONResult{ImageBytes: []byte(n.name)}, nil
}

func TestSlotRouterSendsPantsToTheirProvider(t *testing.T) {
	cheap, pricey := &namedFake{name: "fashn"}, &namedFake{name: "google_vertex"}
	r := &SlotRouter{Default: cheap, BySlot: map[string]VTONProvider{"lower_body": pricey}}

	for _, slot := range []string{"upper_body", "outerwear", "lower_body"} {
		if _, err := r.Generate(context.Background(), VTONRequest{Category: slot}); err != nil {
			t.Fatal(err)
		}
	}
	if cheap.calls != 2 || pricey.calls != 1 {
		t.Fatalf("calls: default=%d lower_body=%d, want 2 and 1", cheap.calls, pricey.calls)
	}
	if got, want := r.Name(), "fashn+lower_body:google_vertex"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}
