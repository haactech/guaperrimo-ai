package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"stylerag/internal/rag"
)

type options struct {
	input          string
	qdrantURL      string
	collection     string
	vectorSize     int
	batchSize      int
	recreate       bool
	openaiAPIKey   string
	embeddingModel string
	fakeVectors    bool
	embedBatchSize int
}

type point struct {
	ID      string         `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

func main() {
	opts := parseFlags()
	if err := run(opts); err != nil {
		log.Fatal(err)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.input, "input", "", "Path to normalized products CSV")
	flag.StringVar(&opts.qdrantURL, "qdrant-url", "http://localhost:6333", "Qdrant base URL")
	flag.StringVar(&opts.collection, "collection", "products", "Qdrant collection name")
	flag.IntVar(&opts.vectorSize, "vector-size", 1536, "Vector size for embeddings")
	flag.IntVar(&opts.batchSize, "batch-size", 256, "Batch size for upsert")
	flag.BoolVar(&opts.recreate, "recreate", false, "Drop collection before seeding")
	flag.StringVar(&opts.openaiAPIKey, "openai-api-key", "", "OpenAI API key (env: OPENAI_API_KEY)")
	flag.StringVar(&opts.embeddingModel, "embedding-model", "text-embedding-3-small", "OpenAI embedding model")
	flag.BoolVar(&opts.fakeVectors, "fake-vectors", false, "Use deterministic fake vectors instead of OpenAI")
	flag.IntVar(&opts.embedBatchSize, "embed-batch-size", 50, "Texts per OpenAI embedding request")
	flag.Parse()

	if opts.input == "" {
		log.Fatal("--input es obligatorio")
	}
	if opts.vectorSize <= 0 {
		log.Fatal("--vector-size debe ser > 0")
	}
	if opts.batchSize <= 0 {
		log.Fatal("--batch-size debe ser > 0")
	}

	// Fallback to env var
	if opts.openaiAPIKey == "" {
		opts.openaiAPIKey = os.Getenv("OPENAI_API_KEY")
	}

	// If no API key, force fake vectors
	if opts.openaiAPIKey == "" && !opts.fakeVectors {
		log.Println("WARN: no OpenAI API key provided, falling back to --fake-vectors")
		opts.fakeVectors = true
	}

	return opts
}

// row holds parsed CSV data for a single product
type row struct {
	id        string
	payload   map[string]any
	embedText string
}

func run(opts options) error {
	client := &http.Client{Timeout: 30 * time.Second}
	baseURL := strings.TrimRight(opts.qdrantURL, "/")

	if opts.recreate {
		if err := deleteCollection(client, baseURL, opts.collection); err != nil {
			return err
		}
	}

	if err := ensureCollection(client, baseURL, opts.collection, opts.vectorSize); err != nil {
		return err
	}

	// Set up embedder
	var embedder *rag.OpenAIEmbedder
	if !opts.fakeVectors {
		embedder = rag.NewOpenAIEmbedder(opts.openaiAPIKey, opts.embeddingModel)
		log.Printf("using OpenAI embeddings: model=%s, batch_size=%d", opts.embeddingModel, opts.embedBatchSize)
	} else {
		log.Printf("using fake deterministic vectors: size=%d", opts.vectorSize)
	}

	input, err := os.Open(opts.input)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer input.Close()

	csvr := csv.NewReader(input)
	csvr.FieldsPerRecord = -1
	csvr.LazyQuotes = true

	headers, err := csvr.Read()
	if err != nil {
		return fmt.Errorf("read headers: %w", err)
	}

	idx := map[string]int{}
	for i, h := range headers {
		idx[strings.TrimSpace(h)] = i
	}

	mustHave := []string{"id", "name", "description", "category", "subcategory", "style_tags", "colors", "image_url", "price", "currency"}
	for _, c := range mustHave {
		if _, ok := idx[c]; !ok {
			return fmt.Errorf("csv no contiene columna requerida: %s", c)
		}
	}

	// Read all rows first
	var rows []row
	for {
		rec, err := csvr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read csv row: %w", err)
		}

		id := get(rec, idx, "id")
		if id == "" {
			continue
		}

		styleTags := parsePGArray(get(rec, idx, "style_tags"))
		colors := parsePGArray(get(rec, idx, "colors"))
		price := parseNullableFloat(get(rec, idx, "price"))

		payload := map[string]any{
			"name":        get(rec, idx, "name"),
			"description": get(rec, idx, "description"),
			"category":    get(rec, idx, "category"),
			"subcategory": get(rec, idx, "subcategory"),
			"style_tags":  styleTags,
			"colors":      colors,
			"image_url":   get(rec, idx, "image_url"),
			"currency":    get(rec, idx, "currency"),
		}
		if price != nil {
			payload["price"] = *price
		}

		embedText := strings.Join([]string{
			get(rec, idx, "name"),
			get(rec, idx, "description"),
			get(rec, idx, "category"),
			get(rec, idx, "subcategory"),
			strings.Join(styleTags, " "),
			strings.Join(colors, " "),
		}, " ")

		rows = append(rows, row{id: id, payload: payload, embedText: embedText})
	}

	log.Printf("parsed %d rows from CSV", len(rows))

	// Process in upsert batches
	total := 0
	for batchStart := 0; batchStart < len(rows); batchStart += opts.batchSize {
		batchEnd := batchStart + opts.batchSize
		if batchEnd > len(rows) {
			batchEnd = len(rows)
		}
		batchRows := rows[batchStart:batchEnd]

		var points []point

		if opts.fakeVectors {
			// Fake vectors — no API call
			for _, r := range batchRows {
				points = append(points, point{
					ID:      r.id,
					Vector:  deterministicVector(r.embedText, opts.vectorSize),
					Payload: r.payload,
				})
			}
		} else {
			// Real embeddings — batch calls to OpenAI
			texts := make([]string, len(batchRows))
			for i, r := range batchRows {
				texts[i] = r.embedText
			}

			embeddings, err := embedBatchWithRetry(embedder, texts, opts.embedBatchSize)
			if err != nil {
				return fmt.Errorf("embed batch at offset %d: %w", batchStart, err)
			}

			for i, r := range batchRows {
				vec := make([]float64, len(embeddings[i]))
				for j, v := range embeddings[i] {
					vec[j] = float64(v)
				}
				points = append(points, point{
					ID:      r.id,
					Vector:  vec,
					Payload: r.payload,
				})
			}
		}

		if err := upsertPoints(client, baseURL, opts.collection, points); err != nil {
			return err
		}
		total += len(points)
		log.Printf("upserted %d/%d", total, len(rows))
	}

	log.Printf("qdrantseed completado: %d productos en collection %q", total, opts.collection)
	return nil
}

// embedBatchWithRetry embeds texts in sub-batches with exponential backoff on 429.
func embedBatchWithRetry(embedder *rag.OpenAIEmbedder, texts []string, batchSize int) ([][]float32, error) {
	ctx := context.Background()
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		chunk := texts[i:end]

		var vecs [][]float32
		var err error

		// Retry with exponential backoff
		for attempt := 0; attempt < 5; attempt++ {
			vecs, err = embedder.EmbedBatch(ctx, chunk)
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate") {
				wait := time.Duration(1<<uint(attempt)) * time.Second
				log.Printf("rate limited, waiting %v (attempt %d/5)", wait, attempt+1)
				time.Sleep(wait)
				continue
			}
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("failed after retries: %w", err)
		}

		copy(allEmbeddings[i:end], vecs)
		log.Printf("embedded %d/%d texts", end, len(texts))
	}

	return allEmbeddings, nil
}

func get(rec []string, idx map[string]int, key string) string {
	pos, ok := idx[key]
	if !ok || pos < 0 || pos >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[pos])
}

func parseNullableFloat(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

func deterministicVector(text string, size int) []float64 {
	vec := make([]float64, size)
	tokens := strings.Fields(strings.ToLower(text))
	if len(tokens) == 0 {
		return vec
	}

	for _, token := range tokens {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		hash := h.Sum64()

		idx := int(hash % uint64(size))
		sign := 1.0
		if (hash>>8)&1 == 1 {
			sign = -1.0
		}
		weight := 1.0 + float64((hash>>16)%7)/10.0
		vec[idx] += sign * weight
	}

	normalize(vec)
	return vec
}

func normalize(vec []float64) {
	var sum float64
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return
	}
	norm := math.Sqrt(sum)
	for i := range vec {
		vec[i] /= norm
	}
}

func parsePGArray(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" || input == "{}" {
		return nil
	}
	if strings.HasPrefix(input, "{") && strings.HasSuffix(input, "}") {
		input = input[1 : len(input)-1]
	}
	if strings.TrimSpace(input) == "" {
		return nil
	}

	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		p = strings.ReplaceAll(p, `\\"`, `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ensureCollection(client *http.Client, baseURL, name string, vectorSize int) error {
	url := fmt.Sprintf("%s/collections/%s", baseURL, name)

	reqBody := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal create collection request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create collection request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("create collection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict {
		return nil
	}

	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("create collection failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
}

func deleteCollection(client *http.Client, baseURL, name string) error {
	url := fmt.Sprintf("%s/collections/%s", baseURL, name)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("delete collection request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete collection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("delete collection failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
}

func upsertPoints(client *http.Client, baseURL, collection string, points []point) error {
	url := fmt.Sprintf("%s/collections/%s/points?wait=true", baseURL, collection)
	reqBody := map[string]any{"points": points}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal points: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build upsert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upsert points failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("upsert points failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
}
