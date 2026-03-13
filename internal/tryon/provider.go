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
