package config

import (
	"bufio"
	"log/slog"
	"os"
	"strconv"
	"strings"
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
