package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"stylerag/internal/config"
	"stylerag/internal/llm"
	"stylerag/internal/vision"
)

func main() {
	imagePath := flag.String("image", "", "path to outfit image (required)")
	flag.Parse()

	if *imagePath == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/analyze -image <path>")
		os.Exit(1)
	}

	cfg := config.Load()
	if cfg.MistralAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: MISTRAL_API_KEY environment variable is required")
		os.Exit(1)
	}

	slog.Info("initializing pipeline", "model_potent", cfg.MistralPotentModel, "model_economy", cfg.MistralEconomyModel)

	potent := llm.NewMistralProvider(cfg.MistralAPIKey, cfg.MistralPotentModel)
	economy := llm.NewMistralProvider(cfg.MistralAPIKey, cfg.MistralEconomyModel)
	router := llm.NewRouter(potent, economy)
	analyzer := vision.NewLLMAnalyzer(router)

	imageData, err := os.ReadFile(*imagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot read image: %v\n", err)
		os.Exit(1)
	}

	slog.Info("analyzing outfit", "image", *imagePath, "size_bytes", len(imageData))

	analysis, err := analyzer.AnalyzeOutfit(context.Background(), imageData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: analysis failed: %v\n", err)
		os.Exit(1)
	}

	output, _ := json.MarshalIndent(analysis, "", "  ")
	fmt.Println(string(output))
}
