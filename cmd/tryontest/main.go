// tryontest renders a chain of garments on a person photo with the configured
// provider and saves every step, to compare providers without the server.
//
//	go run ./cmd/tryontest -person foto.jpg \
//	  -garment upper_body=https://…/camisa.jpg -garment lower_body=pantalon.png \
//	  -provider fashn -mode quality -out out/fashn
//
// Every step is one billed image. -max caps the number of calls.
// With -provider gemini -together, all garments go in one call (one image).
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
	flag.Var(&gs, "garment", "slot=path-or-url[::title], repeat in chain order (upper_body, outerwear, lower_body)")
	providerName := flag.String("provider", "", "fashn | google_vertex | gemini (default: VTON_PROVIDER)")
	mode := flag.String("mode", "", "fashn mode: performance | balanced | quality")
	model := flag.String("model", "", "fashn model (default tryon-v1.6) or gemini model (default GEMINI_IMAGE_MODEL)")
	together := flag.Bool("together", false, "gemini only: render every garment in a single call")
	photoType := flag.String("photo-type", "auto", "fashn garment photo type: auto | flat-lay | model")
	outDir := flag.String("out", "tryon_out", "output directory")
	maxCalls := flag.Int("max", 4, "maximum provider calls (each one is billed)")
	flag.Parse()

	if *person == "" || len(gs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tryontest -person foto.jpg -garment upper_body=camisa.jpg [-garment lower_body=…] [-provider fashn]")
		os.Exit(2)
	}
	if !*together && len(gs) > *maxCalls {
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
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			fmt.Fprintln(os.Stderr, "GEMINI_API_KEY not set")
			os.Exit(1)
		}
		mn := cfg.GeminiImageModel
		if *model != "" {
			mn = *model
		}
		p = &tryon.GeminiVTON{APIKey: cfg.GeminiAPIKey, Model: mn, HTTPClient: &http.Client{Timeout: 180 * time.Second}}
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
	if *together {
		gp, ok := p.(*tryon.GeminiVTON)
		if !ok {
			fmt.Fprintln(os.Stderr, "-together needs -provider gemini")
			os.Exit(2)
		}
		var outfit []tryon.OutfitGarment
		var slots []string
		for _, g := range gs {
			slot, src, ok := strings.Cut(g, "=")
			if !ok {
				fmt.Fprintf(os.Stderr, "bad -garment %q, want slot=path-or-url\n", g)
				os.Exit(2)
			}
			src, desc, _ := strings.Cut(src, "::")
			garment, err := load(src)
			if err != nil {
				fmt.Fprintf(os.Stderr, "load %s: %v\n", src, err)
				os.Exit(1)
			}
			outfit = append(outfit, tryon.OutfitGarment{Image: garment, Slot: slot, Desc: desc})
			slots = append(slots, slot)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		res, err := gp.GenerateOutfit(ctx, current, outfit)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "outfit failed:", err)
			os.Exit(1)
		}
		out := filepath.Join(*outDir, fmt.Sprintf("%s_outfit_%s%s", safeName(p.Name()), strings.Join(slots, "+"), ext(res.MimeType)))
		if err := os.WriteFile(out, res.ImageBytes, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("outfit %-30s %6d ms  %s\n", strings.Join(slots, "+"), res.GenerationMs, out)
		return
	}
	for i, g := range gs {
		slot, src, ok := strings.Cut(g, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "bad -garment %q, want slot=path-or-url\n", g)
			os.Exit(2)
		}
		src, desc, _ := strings.Cut(src, "::")
		garment, err := load(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load %s: %v\n", src, err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		res, err := p.Generate(ctx, tryon.VTONRequest{PersonImage: current, GarmentImage: garment, GarmentDesc: desc, Category: slot})
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "step %d (%s) failed: %v\n", i+1, slot, err)
			os.Exit(1)
		}
		out := filepath.Join(*outDir, fmt.Sprintf("%s_step%d_%s%s", safeName(p.Name()), i+1, slot, ext(res.MimeType)))
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

// safeName keeps provider names like "gemini:model" usable as file names.
func safeName(s string) string { return strings.ReplaceAll(s, ":", "_") }

func ext(mime string) string {
	if mime == "image/png" {
		return ".png"
	}
	return ".jpg"
}
