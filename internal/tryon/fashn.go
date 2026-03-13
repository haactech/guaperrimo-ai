package tryon

import (
	"context"
	"fmt"
	"net/http"
)

// FashnVTON implements VTONProvider using the FASHN API.
type FashnVTON struct {
	APIKey     string
	BaseURL    string
	Mode       string
	HTTPClient *http.Client
}

func (f *FashnVTON) Name() string { return "fashn" }

func (f *FashnVTON) Generate(_ context.Context, _ VTONRequest) (*VTONResult, error) {
	return nil, fmt.Errorf("FASHN provider not yet implemented")
}
