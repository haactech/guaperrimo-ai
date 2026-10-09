package tryon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DownloadImage fetches an image over http(s) with a size cap. It refuses
// other schemes and non-image responses.
func DownloadImage(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("download: invalid image url")
	}
	dlCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("download: build request: %w", err)
	}
	req.Header.Set("User-Agent", "guaperrimo/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") && !strings.HasPrefix(ct, "application/octet-stream") {
		return nil, fmt.Errorf("download: not an image (%s)", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("download: read: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("download: image larger than %d bytes", maxBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("download: empty body")
	}
	return data, nil
}
