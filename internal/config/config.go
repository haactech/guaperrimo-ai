package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port     string
	LogLevel slog.Level

	// Database
	PostgresURL string
	RedisURL    string
	QdrantURL   string

	// LLM Providers
	AnthropicAPIKey string
	OpenAIAPIKey    string

	// Embeddings (OpenAI)
	EmbeddingModel string
	EmbeddingDims  int

	// Qdrant
	QdrantCollection string

	// Mistral
	MistralAPIKey       string
	MistralPotentModel  string
	MistralEconomyModel string

	// Kimi (Moonshot)
	MoonshotAPIKey    string
	KimiPotentModel   string
	KimiEconomyModel  string

	// LLM provider selection: "kimi" or "mistral"
	LLMProvider string

	// Voice
	AvalonAPIKey string

	// Local image serving
	ImageDir string

	// Storage (Cloudflare R2)
	R2AccountID       string
	R2AccessKeyID     string
	R2AccessKeySecret string
	R2BucketName      string
	R2PublicURL       string

	// Virtual Try-On
	VTONProvider   string        // "google_vertex" (default) or "fashn"
	GCPProjectID   string
	GCPSAKeyJSON   string        // Service account JSON for production (written to temp file)
	GCPRegion      string
	VTONBaseSteps int
	FashnAPIKey   string
	FashnMode     string
	VTONTimeout   time.Duration

	// Look Generation
	LookGenerationTimeout time.Duration
	LookCount             int
}

func Load() *Config {
	// Load .env file if present (supports "export KEY=VAL" format)
	loadDotenv(".env")

	return &Config{
		Port:     getEnv("PORT", "8080"),
		LogLevel: parseLogLevel(getEnv("LOG_LEVEL", "info")),

		PostgresURL: getEnv("POSTGRES_URL", ""),
		RedisURL:    getEnv("REDIS_URL", ""),
		QdrantURL:   getEnv("QDRANT_URL", ""),

		AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
		OpenAIAPIKey:    getEnv("OPENAI_API_KEY", ""),

		EmbeddingModel:   getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingDims:    getEnvInt("OPENAI_EMBEDDING_DIMS", 1536),
		QdrantCollection: getEnv("QDRANT_COLLECTION", "products"),

		MistralAPIKey:       getEnv("MISTRAL_API_KEY", ""),
		MistralPotentModel:  getEnv("MISTRAL_POTENT_MODEL", "mistral-large-latest"),
		MistralEconomyModel: getEnv("MISTRAL_ECONOMY_MODEL", "mistral-small-latest"),

		MoonshotAPIKey:   getEnv("MOONSHOT_API_KEY", ""),
		KimiPotentModel:  getEnv("KIMI_POTENT_MODEL", "kimi-k2.5"),
		KimiEconomyModel: getEnv("KIMI_ECONOMY_MODEL", "kimi-k2.5"),

		LLMProvider: getEnv("LLM_PROVIDER", "mistral"),

		AvalonAPIKey: getEnv("AVALON_API_KEY", ""),

		ImageDir: getEnv("IMAGE_DIR", "data/fashion-dataset/images"),

		R2AccountID:       getEnv("R2_ACCOUNT_ID", ""),
		R2AccessKeyID:     getEnv("R2_ACCESS_KEY_ID", ""),
		R2AccessKeySecret: getEnv("R2_ACCESS_KEY_SECRET", ""),
		R2BucketName:      getEnv("R2_BUCKET_NAME", ""),
		R2PublicURL:       getEnv("R2_PUBLIC_URL", ""),

		VTONProvider:  getEnv("VTON_PROVIDER", "google_vertex"),
		GCPProjectID:  getEnv("GCP_PROJECT_ID", ""),
		GCPSAKeyJSON:  getEnv("GCP_SA_KEY_JSON", ""),
		GCPRegion:     getEnv("GCP_REGION", "us-central1"),
		VTONBaseSteps: getEnvInt("VTON_BASE_STEPS", 20),
		FashnAPIKey:   getEnv("FASHN_API_KEY", ""),
		FashnMode:     getEnv("FASHN_MODE", "balanced"),
		VTONTimeout:   parseDuration(getEnv("VTON_TIMEOUT", "30s")),

		LookGenerationTimeout: parseDuration(getEnv("LOOK_GENERATION_TIMEOUT", "90s")),
		LookCount:             getEnvInt("LOOK_COUNT", 3),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// SetupGCPCredentials writes the service account JSON to a temp file and sets
// GOOGLE_APPLICATION_CREDENTIALS so that ADC (FindDefaultCredentials) picks it up.
// No-op if GCPSAKeyJSON is empty (local dev uses `gcloud auth application-default login`).
func (c *Config) SetupGCPCredentials() error {
	if c.GCPSAKeyJSON == "" {
		return nil
	}
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return nil // already set externally
	}
	f, err := os.CreateTemp("", "gcp-sa-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file for GCP credentials: %w", err)
	}
	if _, err := f.WriteString(c.GCPSAKeyJSON); err != nil {
		f.Close()
		return fmt.Errorf("writing GCP credentials: %w", err)
	}
	f.Close()
	return os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", f.Name())
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// loadDotenv reads a .env file and sets env vars that aren't already set.
// Supports both "KEY=VAL" and "export KEY=VAL" formats.
func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, "\"'")
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
