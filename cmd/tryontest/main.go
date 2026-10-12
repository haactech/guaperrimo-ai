// tryontest renders a chain of garments on a person photo with the configured
// provider and saves every step, to compare providers without the server.
//
//	go run ./cmd/tryontest -person foto.jpg \
//	  -garment upper_body=https://…/camisa.jpg -garment lower_body=pantalon.png \
//	  -provider fashn -mode quality -out out/fashn
//
// Every step is one billed image. -max caps the number of calls.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stylerag/internal/config"
	"stylerag/internal/tryon"
)

type garments []string

func (g *garments) String() string     { return strings.Join(*g, ",") }
func (g *garments) Set(v string) error { *g = append(*g, v); return nil }

func main() {
	var gs garments
	person := flag.String("person", "", "person photo (jpg/png)")
	flag.Var(&gs, "garment", "slot=path-or-url, repeat in chain order (upper_body, outerwear, lower_body)")
	providerName := flag.String("provider", "", "fashn | google_vertex (default: VTON_PROVIDER)")
	mode := flag.String("mode", "", "fashn mode: performance | balanced | quality")
	model := flag.String("model", "", "fashn model name (default tryon-v1.6)")
	photoType := flag.String("photo-type", "auto", "fashn garment photo type: auto | flat-lay | model")
	outDir := flag.String("out", "tryon_out", "output directory")
	maxCalls := flag.Int("max", 4, "maximum provider calls (each one is billed)")
	flag.Parse()

	if *person == "" || len(gs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tryontest -person foto.jpg -garment upper_body=camisa.jpg [-garment lower_body=…] [-provider fashn]")
		os.Exit(2)
	}
	if len(gs) > *maxCalls {
		fmt.Fprintf(os.Stderr, "refusing: %d garments exceed -max %d (each call is billed)\n", len(gs), *maxCalls)
		os.Exit(2)
	}

	cfg := config.Load()
	name := *providerName
	if name == "" {
		name = cfg.VTONProvider
	}
	var p tryon.VTONProvider
	switch name {
	case "fashn":
		if cfg.FashnAPIKey == "" {
			fmt.Fprintln(os.Stderr, "FASHN_API_KEY not set")
			os.Exit(1)
		}
		m := cfg.FashnMode
		if *mode != "" {
			m = *mode
		}
		mn := cfg.FashnModel
		if *model != "" {
			mn = *model
		}
		p = &tryon.FashnVTON{APIKey: cfg.FashnAPIKey, Model: mn, Mode: m, PhotoType: *photoType, HTTPClient: &http.Client{Timeout: 30 * time.Second}}
	case "google_vertex":
		if err := cfg.SetupGCPCredentials(); err != nil || cfg.GCPProjectID == "" {
			fmt.Fprintln(os.Stderr, "GCP_PROJECT_ID / credentials not available")
			os.Exit(1)
		}
		p = &tryon.GoogleVertexVTON{ProjectID: cfg.GCPProjectID, Region: cfg.GCPRegion, BaseSteps: cfg.VTONBaseSteps, HTTPClient: &http.Client{Timeout: 60 * time.Second}}
	default:
		fmt.Fprintf(os.Stderr, "unknown provider %q\n", name)
		os.Exit(1)
	}

	current, err := os.ReadFile(*person)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read person:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	total := time.Now()
	for i, g := range gs {
		slot, src, ok := strings.Cut(g, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "bad -garment %q, want slot=path-or-url\n", g)
			os.Exit(2)
		}
		garment, err := load(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load %s: %v\n", src, err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		res, err := p.Generate(ctx, tryon.VTONRequest{PersonImage: current, GarmentImage: garment, Category: slot})
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "step %d (%s) failed: %v\n", i+1, slot, err)
			os.Exit(1)
		}
		out := filepath.Join(*outDir, fmt.Sprintf("%s_step%d_%s.jpg", p.Name(), i+1, slot))
		if err := os.WriteFile(out, res.ImageBytes, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("step %d %-10s %6d ms  %s\n", i+1, slot, res.GenerationMs, out)
		current = res.ImageBytes
	}
	fmt.Printf("done: %d images in %s with %s\n", len(gs), time.Since(total).Round(time.Second), p.Name())
}

func load(src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return tryon.DownloadImage(ctx, nil, src, 20<<20)
	}
	return os.ReadFile(src)
}
