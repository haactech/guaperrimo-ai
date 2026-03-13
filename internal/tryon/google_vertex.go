package tryon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2/google"
)

// GoogleVertexVTON implements VTONProvider using Google Vertex AI Virtual Try-On.
type GoogleVertexVTON struct {
	ProjectID  string
	Region     string
	BaseSteps  int
	HTTPClient *http.Client
}

func (g *GoogleVertexVTON) Name() string { return "google_vertex" }

func (g *GoogleVertexVTON) Generate(ctx context.Context, req VTONRequest) (*VTONResult, error) {
	start := time.Now()

	token, err := getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting access token: %w", err)
	}

	personB64 := base64.StdEncoding.EncodeToString(req.PersonImage)
	garmentB64 := base64.StdEncoding.EncodeToString(req.GarmentImage)

	body := map[string]any{
		"instances": []map[string]any{
			{
				"personImage": map[string]any{
					"image": map[string]string{
						"bytesBase64Encoded": personB64,
					},
				},
				"productImages": []map[string]any{
					{
						"image": map[string]string{
							"bytesBase64Encoded": garmentB64,
						},
					},
				},
			},
		},
		"parameters": map[string]any{
			"sampleCount":      1,
			"baseSteps":        g.BaseSteps,
			"personGeneration": "allow_adult",
			"outputOptions": map[string]any{
				"mimeType":           "image/jpeg",
				"compressionQuality": 85,
			},
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/virtual-try-on-001:predict",
		g.Region, g.ProjectID, g.Region,
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling vertex API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Predictions []struct {
			BytesBase64Encoded string `json:"bytesBase64Encoded"`
		} `json:"predictions"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	if len(result.Predictions) == 0 || result.Predictions[0].BytesBase64Encoded == "" {
		return nil, fmt.Errorf("no prediction returned from vertex API")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(result.Predictions[0].BytesBase64Encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding prediction image: %w", err)
	}

	return &VTONResult{
		ImageBytes:   imageBytes,
		MimeType:     "image/jpeg",
		GenerationMs: time.Since(start).Milliseconds(),
		ProviderName: g.Name(),
	}, nil
}

// getAccessToken obtains a Google Cloud access token using Application Default Credentials.
func getAccessToken(ctx context.Context) (string, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("finding default credentials: %w", err)
	}
	token, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("obtaining token: %w", err)
	}
	return token.AccessToken, nil
}
