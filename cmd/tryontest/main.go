// tryontest is a standalone CLI tool to test the Virtual Try-On provider
// without needing the full server, RAG, or session flow.
//
// Usage:
//
//	go run ./cmd/tryontest -person photo.jpg -garment garment.jpg
//
// Prerequisites:
//
//	export GCP_PROJECT_ID=gen-lang-client-0210965642
//	gcloud auth application-default login
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"stylerag/internal/config"
	"stylerag/internal/tryon"
)

func main() {
	personPath := flag.String("person", "", "Path to person image (JPEG/PNG)")
	garmentPath := flag.String("garment", "", "Path to garment image (JPEG/PNG)")
	outputPath := flag.String("out", "tryon_result.jpg", "Output file path")
	category := flag.String("category", "upper_body", "Category: upper_body, lower_body, dresses")
	flag.Parse()

	if *personPath == "" || *garmentPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/tryontest -person photo.jpg -garment garment.jpg")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  Requires: GCP_PROJECT_ID env var + gcloud auth application-default login")
		os.Exit(1)
	}

	cfg := config.Load()
	if cfg.GCPProjectID == "" {
		fmt.Fprintln(os.Stderr, "Error: GCP_PROJECT_ID not set. Run: export GCP_PROJECT_ID=your-project-id")
		os.Exit(1)
	}

	if err := cfg.SetupGCPCredentials(); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up GCP credentials: %v\n", err)
		os.Exit(1)
	}

	personImg, err := os.ReadFile(*personPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading person image: %v\n", err)
		os.Exit(1)
	}

	garmentImg, err := os.ReadFile(*garmentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading garment image: %v\n", err)
		os.Exit(1)
	}

	provider := &tryon.GoogleVertexVTON{
		ProjectID:  cfg.GCPProjectID,
		Region:     cfg.GCPRegion,
		BaseSteps:  cfg.VTONBaseSteps,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}

	slog.Info("starting VTON test",
		"provider", provider.Name(),
		"project", cfg.GCPProjectID,
		"region", cfg.GCPRegion,
		"person_size", len(personImg),
		"garment_size", len(garmentImg),
		"category", *category,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := provider.Generate(ctx, tryon.VTONRequest{
		PersonImage:  personImg,
		GarmentImage: garmentImg,
		Category:     *category,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "VTON generation failed: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputPath, result.ImageBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}

	slog.Info("VTON test complete",
		"output", *outputPath,
		"size_bytes", len(result.ImageBytes),
		"generation_ms", result.GenerationMs,
		"provider", result.ProviderName,
	)
}
