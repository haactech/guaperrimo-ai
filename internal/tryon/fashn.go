package tryon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FashnVTON implements VTONProvider with the FASHN API (https://docs.fashn.ai):
// POST /v1/run, then poll GET /v1/status/{id}. Images go and come back as
// base64, so FASHN never stores a URL to the user's photo. Billed per output
// image (~0.075 USD on tryon-v1.6); failed predictions are not billed.
type FashnVTON struct {
	APIKey       string
	BaseURL      string        // default https://api.fashn.ai/v1
	Model        string        // default tryon-v1.6
	Mode         string        // performance | balanced | quality (default balanced)
	PhotoType    string        // auto | flat-lay | model (default auto)
	PollInterval time.Duration // default 1.5 s
	HTTPClient   *http.Client
}

func (f *FashnVTON) Name() string { return "fashn" }

type fashnRunRequest struct {
	ModelName string         `json:"model_name"`
	Inputs    map[string]any `json:"inputs"`
}

type fashnStatus struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Output []string `json:"output"`
	Error  *struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error"`
}

// fashnCategory maps our slots to FASHN categories.
func fashnCategory(slot string) string {
	switch slot {
	case "lower_body":
		return "bottoms"
	case "upper_body", "outerwear":
		return "tops"
	case "dresses":
		return "one-pieces"
	default:
		return "auto"
	}
}

func dataURI(b []byte) string {
	mt := http.DetectContentType(b)
	if !strings.HasPrefix(mt, "image/") {
		mt = "image/jpeg"
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// Generate runs one garment on the person image and waits for the result.
func (f *FashnVTON) Generate(ctx context.Context, req VTONRequest) (*VTONResult, error) {
	start := time.Now()
	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		base = "https://api.fashn.ai/v1"
	}
	model := f.Model
	if model == "" {
		model = "tryon-v1.6"
	}
	mode := f.Mode
	if mode == "" {
		mode = "balanced"
	}
	photoType := f.PhotoType
	if photoType == "" {
		photoType = "auto"
	}
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	poll := f.PollInterval
	if poll <= 0 {
		poll = 1500 * time.Millisecond
	}

	body, err := json.Marshal(fashnRunRequest{ModelName: model, Inputs: map[string]any{
		"model_image":        dataURI(req.PersonImage),
		"garment_image":      dataURI(req.GarmentImage),
		"category":           fashnCategory(req.Category),
		"garment_photo_type": photoType,
		"mode":               mode,
		"num_samples":        1,
		"output_format":      "jpeg",
		"return_base64":      true,
	}})
	if err != nil {
		return nil, fmt.Errorf("fashn: encode request: %w", err)
	}

	var run struct {
		ID    string `json:"id"`
		Error any    `json:"error"`
	}
	if err := f.do(ctx, client, http.MethodPost, base+"/run", body, &run); err != nil {
		return nil, err
	}
	if run.ID == "" {
		return nil, fmt.Errorf("fashn: run returned no id (error: %v)", run.Error)
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("fashn: prediction %s: %w", run.ID, ctx.Err())
		case <-ticker.C:
		}
		var st fashnStatus
		if err := f.do(ctx, client, http.MethodGet, base+"/status/"+run.ID, nil, &st); err != nil {
			return nil, err
		}
		switch st.Status {
		case "completed":
			if len(st.Output) == 0 {
				return nil, fmt.Errorf("fashn: prediction %s completed without output", run.ID)
			}
			img, mime, err := decodeOutput(ctx, client, st.Output[0])
			if err != nil {
				return nil, err
			}
			return &VTONResult{ImageBytes: img, MimeType: mime, GenerationMs: time.Since(start).Milliseconds(), ProviderName: f.Name()}, nil
		case "failed":
			name, msg := "unknown", ""
			if st.Error != nil {
				name, msg = st.Error.Name, st.Error.Message
			}
			return nil, fmt.Errorf("fashn: prediction failed: %s: %s", name, msg)
		}
	}
}

func (f *FashnVTON) do(ctx context.Context, client *http.Client, method, url string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return fmt.Errorf("fashn: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fashn: %s %s: %w", method, strings.TrimPrefix(url, "https://"), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	if err != nil {
		return fmt.Errorf("fashn: read response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("fashn: returned 429 RESOURCE_EXHAUSTED: %s", truncateBytes(raw, 200))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("fashn: status %d: %s", resp.StatusCode, truncateBytes(raw, 300))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("fashn: parse response: %w", err)
	}
	return nil
}

// decodeOutput accepts a data URI (return_base64) or a CDN URL.
func decodeOutput(ctx context.Context, client *http.Client, out string) ([]byte, string, error) {
	if strings.HasPrefix(out, "data:") {
		comma := strings.Index(out, ",")
		if comma < 0 {
			return nil, "", fmt.Errorf("fashn: malformed data uri")
		}
		mime := strings.TrimSuffix(strings.TrimPrefix(out[:comma], "data:"), ";base64")
		img, err := base64.StdEncoding.DecodeString(out[comma+1:])
		if err != nil {
			return nil, "", fmt.Errorf("fashn: decode output: %w", err)
		}
		return img, mime, nil
	}
	img, err := DownloadImage(ctx, client, out, 20<<20)
	if err != nil {
		return nil, "", fmt.Errorf("fashn: download output: %w", err)
	}
	return img, http.DetectContentType(img), nil
}

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
