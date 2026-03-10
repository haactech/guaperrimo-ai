package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type options struct {
	input            string
	output           string
	imageURLTemplate string
	limit            int
}

func main() {
	opts := parseFlags()

	if err := run(opts); err != nil {
		log.Fatal(err)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.input, "input", "", "Path to Kaggle styles.csv")
	flag.StringVar(&opts.output, "output", "data/processed/products.csv", "Output normalized CSV path")
	flag.StringVar(&opts.imageURLTemplate, "image-url-template", "https://cdn.local/stylerag/kaggle/images/%s.jpg", "Template for image URL, must contain %s for product ID")
	flag.IntVar(&opts.limit, "limit", 0, "Optional max rows to process (0 means all)")
	flag.Parse()

	if opts.input == "" {
		log.Fatal("--input es obligatorio")
	}
	if !strings.Contains(opts.imageURLTemplate, "%s") {
		log.Fatal("--image-url-template debe incluir el placeholder de ID de producto")
	}
	return opts
}

func run(opts options) error {
	inputFile, err := os.Open(opts.input)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer inputFile.Close()

	r := csv.NewReader(inputFile)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	headers, err := r.Read()
	if err != nil {
		return fmt.Errorf("read headers: %w", err)
	}

	idx := make(map[string]int, len(headers))
	for i, h := range headers {
		idx[strings.TrimSpace(h)] = i
	}

	mustHave := []string{"id", "masterCategory", "productDisplayName"}
	for _, c := range mustHave {
		if _, ok := idx[c]; !ok {
			return fmt.Errorf("styles.csv no contiene columna requerida: %s", c)
		}
	}

	if err := os.MkdirAll(filepath.Dir(opts.output), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	outputFile, err := os.Create(opts.output)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer outputFile.Close()

	w := csv.NewWriter(outputFile)
	defer w.Flush()

	outputHeaders := []string{
		"id",
		"source_id",
		"name",
		"description",
		"category",
		"subcategory",
		"colors",
		"fit",
		"style_tags",
		"price",
		"currency",
		"sizes",
		"image_url",
		"metadata",
	}
	if err := w.Write(outputHeaders); err != nil {
		return fmt.Errorf("write output headers: %w", err)
	}

	processed := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read csv row: %w", err)
		}

		sourceID := get(rec, idx, "id")
		if sourceID == "" {
			continue
		}

		name := fallback(get(rec, idx, "productDisplayName"), "Unnamed product")
		category := normalizeTag(get(rec, idx, "masterCategory"))
		subcategory := firstNonEmpty(
			normalizeTag(get(rec, idx, "subCategory")),
			normalizeTag(get(rec, idx, "articleType")),
		)
		color := normalizeTag(get(rec, idx, "baseColour"))
		gender := normalizeTag(get(rec, idx, "gender"))
		usage := normalizeTag(get(rec, idx, "usage"))
		season := normalizeTag(get(rec, idx, "season"))
		year := normalizeTag(get(rec, idx, "year"))

		description := buildDescription(name, category, subcategory, color, usage)
		styleTags := dedupeAndSort([]string{
			category,
			subcategory,
			gender,
			usage,
			season,
			year,
		})

		metadataRaw := map[string]string{}
		for h, i := range idx {
			if i >= 0 && i < len(rec) {
				metadataRaw[h] = strings.TrimSpace(rec[i])
			}
		}
		metadataJSON, err := json.Marshal(metadataRaw)
		if err != nil {
			return fmt.Errorf("marshal metadata for product %s: %w", sourceID, err)
		}

		price := parseFloat(get(rec, idx, "price"))

		out := []string{
			"kaggle_" + sourceID,
			sourceID,
			name,
			description,
			fallback(category, "unknown"),
			subcategory,
			toPGArray(compact([]string{color})),
			"",
			toPGArray(styleTags),
			price,
			"MXN",
			"{}",
			fmt.Sprintf(opts.imageURLTemplate, sourceID),
			string(metadataJSON),
		}

		if err := w.Write(out); err != nil {
			return fmt.Errorf("write output row: %w", err)
		}

		processed++
		if opts.limit > 0 && processed >= opts.limit {
			break
		}
	}

	if err := w.Error(); err != nil {
		return fmt.Errorf("flush output csv: %w", err)
	}

	log.Printf("catalogprep completado: %d productos -> %s", processed, opts.output)
	return nil
}

func get(rec []string, idx map[string]int, key string) string {
	pos, ok := idx[key]
	if !ok || pos < 0 || pos >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[pos])
}

func fallback(value, fb string) string {
	if value == "" {
		return fb
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizeTag(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func dedupeAndSort(values []string) []string {
	uniq := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		uniq[v] = struct{}{}
	}

	out := make([]string, 0, len(uniq))
	for v := range uniq {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func parseFloat(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return ""
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func buildDescription(name, category, subcategory, color, usage string) string {
	parts := []string{name}
	if category != "" {
		parts = append(parts, "categoria "+category)
	}
	if subcategory != "" {
		parts = append(parts, "subcategoria "+subcategory)
	}
	if color != "" {
		parts = append(parts, "color "+color)
	}
	if usage != "" {
		parts = append(parts, "uso "+usage)
	}
	return strings.Join(parts, ", ")
}

func toPGArray(items []string) string {
	if len(items) == 0 {
		return "{}"
	}
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ReplaceAll(item, `\\`, `\\\\`)
		item = strings.ReplaceAll(item, `"`, `\\"`)
		quoted = append(quoted, `"`+item+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}
