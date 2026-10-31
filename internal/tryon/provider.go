package tryon

import "context"

// VTONRequest contains the inputs for a virtual try-on generation.
type VTONRequest struct {
	PersonImage  []byte // JPEG/PNG bytes of the user
	GarmentImage []byte // JPEG/PNG bytes of the garment
	GarmentDesc  string // textual description (optional)
	Category     string // "upper_body", "lower_body", "dresses"
}

// VTONResult contains the output of a virtual try-on generation.
type VTONResult struct {
	ImageBytes   []byte // result JPEG/PNG
	MimeType     string // "image/jpeg"
	GenerationMs int64
	ProviderName string // "google_vertex", "fashn"
}

// VTONProvider is the interface for virtual try-on providers.
type VTONProvider interface {
	Generate(ctx context.Context, req VTONRequest) (*VTONResult, error)
	Name() string
}

// OutfitGarment is one product photo for a single-call outfit render.
type OutfitGarment struct {
	Image []byte
	Slot  string // upper_body | outerwear | lower_body | accessory
	Desc  string
}

// OutfitProvider renders several garments in one call from the original
// photo. Image-editing models (Gemini) do this cheaper and with less drift
// than chaining one garment per call.
type OutfitProvider interface {
	VTONProvider
	SupportsOutfits() bool
	GenerateOutfit(ctx context.Context, person []byte, garments []OutfitGarment) (*VTONResult, error)
}

// AsOutfitProvider reports whether p can render a whole outfit in one call.
func AsOutfitProvider(p VTONProvider) (OutfitProvider, bool) {
	op, ok := p.(OutfitProvider)
	if !ok || !op.SupportsOutfits() {
		return nil, false
	}
	return op, true
}
