package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Product struct {
	ID          string
	Gender      string
	Category    string
	SubCategory string
	ArticleType string
	Color       string
	Season      string
	Year        string
	Usage       string
	DisplayName string
}

func main() {
	dataPath := "data/fashion-dataset/styles.csv"

	file, err := os.Open(dataPath)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1 // Allow variable number of fields
	reader.LazyQuotes = true    // Allow lazy quotes

	records, err := reader.ReadAll()
	if err != nil {
		fmt.Printf("Error reading CSV: %v\n", err)
		os.Exit(1)
	}

	// Skip header
	records = records[1:]

	var products []Product
	for _, record := range records {
		if len(record) < 10 {
			continue
		}
		products = append(products, Product{
			ID:          record[0],
			Gender:      record[1],
			Category:    record[2],
			SubCategory: record[3],
			ArticleType: record[4],
			Color:       record[5],
			Season:      record[6],
			Year:        record[7],
			Usage:       record[8],
			DisplayName: record[9],
		})
	}

	fmt.Printf("Total products: %d\n\n", len(products))

	// Filter for PoC scope: Men's apparel suitable for smart casual
	pocArticleTypes := map[string]bool{
		"Shirts":       true,
		"Tshirts":      true,
		"Trousers":     true,
		"Jeans":        true,
		"Casual Shoes": true,
		"Formal Shoes": true,
		"Blazers":      true,
		"Sweaters":     true,
		"Sweatshirts":  true,
		"Jackets":      true,
		"Shorts":       true,
		"Belts":        true,
		"Watches":      true,
	}

	var pocProducts []Product
	for _, p := range products {
		if p.Gender == "Men" && pocArticleTypes[p.ArticleType] {
			if p.Usage == "Casual" || p.Usage == "Formal" || p.Usage == "Smart Casual" {
				pocProducts = append(pocProducts, p)
			}
		}
	}

	fmt.Printf("PoC scope (Men, Smart Casual compatible): %d products\n\n", len(pocProducts))

	// Count by article type
	articleCounts := make(map[string]int)
	for _, p := range pocProducts {
		articleCounts[p.ArticleType]++
	}

	fmt.Println("PoC products by article type:")
	for article, count := range articleCounts {
		fmt.Printf("  %s: %d\n", article, count)
	}

	// Count by color
	colorCounts := make(map[string]int)
	for _, p := range pocProducts {
		colorCounts[p.Color]++
	}

	fmt.Println("\nTop colors:")
	topColors := getTopN(colorCounts, 10)
	for _, kv := range topColors {
		fmt.Printf("  %s: %d\n", kv.Key, kv.Value)
	}

	// Verify images exist
	fmt.Println("\nVerifying images...")
	imagesDir := "data/fashion-dataset/images"
	missingImages := 0
	for _, p := range pocProducts[:100] { // Check first 100
		imagePath := filepath.Join(imagesDir, p.ID+".jpg")
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			missingImages++
		}
	}
	fmt.Printf("Missing images (sample of 100): %d\n", missingImages)
}

type KV struct {
	Key   string
	Value int
}

func getTopN(m map[string]int, n int) []KV {
	var sorted []KV
	for k, v := range m {
		sorted = append(sorted, KV{k, v})
	}
	// Simple bubble sort for small maps
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Value > sorted[i].Value {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

func init() {
	// Suppress unused import warning
	_ = strings.TrimSpace
}
