package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

type nullStore struct{}

func (nullStore) Upload(_ context.Context, _ UploadInput) (*UploadOutput, error) {
	return &UploadOutput{URL: "s3://x"}, nil
}
func (nullStore) Download(_ context.Context, _ string) ([]byte, error)   { return nil, nil }
func (nullStore) ListKeys(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (nullStore) URL(key string) string                                  { return "s3://" + key }

func TestSignedURLRoundTrip(t *testing.T) {
	s := WithSignedURLs(nullStore{}, "http://10.0.0.5:8080/", "secret", time.Hour)
	key := "sessions/abc/welcome 1.jpg"
	u, err := url.Parse(s.URL(key))
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "10.0.0.5:8080" || !strings.HasPrefix(u.Path, "/images/sessions/abc/") {
		t.Fatalf("url: %s", u)
	}
	gotKey := strings.TrimPrefix(u.Path, "/images/")
	if gotKey != key {
		t.Fatalf("key round trip: %q", gotKey)
	}
	if err := s.Verify(key, u.Query().Get("exp"), u.Query().Get("sig")); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := s.Verify("sessions/other.jpg", u.Query().Get("exp"), u.Query().Get("sig")); err == nil {
		t.Fatal("signature must be bound to the key")
	}
	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if err := s.Verify(key, u.Query().Get("exp"), u.Query().Get("sig")); err == nil {
		t.Fatal("expired link must be rejected")
	}
	out, _ := s.Upload(context.Background(), UploadInput{Key: key})
	if !strings.Contains(out.URL, "/images/") {
		t.Fatalf("upload must return the proxied url, got %s", out.URL)
	}
}
