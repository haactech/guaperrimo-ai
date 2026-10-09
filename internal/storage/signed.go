package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SignedURLs wraps an ImageStore so every URL it hands out points at the API's
// own image proxy with an HMAC signature. The bucket can stay private and the
// app can load images with a plain GET (no headers), which is what AsyncImage
// and <img> need.
type SignedURLs struct {
	ImageStore
	baseURL string // e.g. http://192.168.100.39:8080
	secret  []byte
	ttl     time.Duration
	now     func() time.Time
}

// WithSignedURLs builds the wrapper. ttl bounds how long a handed-out URL works.
func WithSignedURLs(inner ImageStore, baseURL, secret string, ttl time.Duration) *SignedURLs {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &SignedURLs{
		ImageStore: inner,
		baseURL:    strings.TrimRight(baseURL, "/"),
		secret:     []byte(secret),
		ttl:        ttl,
		now:        time.Now,
	}
}

// Upload stores the object and returns the proxied, signed URL.
func (s *SignedURLs) Upload(ctx context.Context, in UploadInput) (*UploadOutput, error) {
	if _, err := s.ImageStore.Upload(ctx, in); err != nil {
		return nil, err
	}
	return &UploadOutput{URL: s.URL(in.Key)}, nil
}

// URL returns /images/{key}?exp=…&sig=… on the API base URL.
func (s *SignedURLs) URL(key string) string {
	exp := s.now().Add(s.ttl).Unix()
	return fmt.Sprintf("%s/images/%s?exp=%d&sig=%s", s.baseURL, escapeKey(key), exp, s.sign(key, exp))
}

// Verify checks a signature for key and expiry taken from a request.
func (s *SignedURLs) Verify(key string, expStr, sig string) error {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return fmt.Errorf("bad expiry")
	}
	if s.now().Unix() > exp {
		return fmt.Errorf("link expired")
	}
	if !hmac.Equal([]byte(sig), []byte(s.sign(key, exp))) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

func (s *SignedURLs) sign(key string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(key))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// escapeKey keeps slashes (they are path segments) and escapes the rest.
func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
