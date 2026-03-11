package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const openaiEmbeddingsURL = "https://api.openai.com/v1/embeddings"

// OpenAIEmbedder generates embeddings via the OpenAI API.
type OpenAIEmbedder struct {
	apiKey string
	model  string
	client *http.Client
}

func NewOpenAIEmbedder(apiKey, model string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// EmbedText generates an embedding for a single text string.
func (e *OpenAIEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedder: empty response")
	}
	return vecs[0], nil
}

// EmbedImage is not supported by text-embedding models.
func (e *OpenAIEmbedder) EmbedImage(ctx context.Context, imageData []byte) ([]float32, error) {
	return nil, fmt.Errorf("embedder: image embedding not supported by %s", e.model)
}

// EmbedBatch generates embeddings for multiple texts in a single API call.
func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := openaiEmbedRequest{
		Model: e.model,
		Input: texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedder: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiEmbeddingsURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedder: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedder: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedder: status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp openaiEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("embedder: unmarshal response: %w", err)
	}

	results := make([][]float32, len(embedResp.Data))
	for _, d := range embedResp.Data {
		if d.Index < 0 || d.Index >= len(results) {
			continue
		}
		results[d.Index] = d.Embedding
	}
	return results, nil
}

// OpenAI API types

type openaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedResponse struct {
	Data []openaiEmbedData `json:"data"`
}

type openaiEmbedData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}
