package tryon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiSendsEveryImageAndDecodesTheResult(t *testing.T) {
	var got struct {
		Contents []struct {
			Parts []struct {
				Text       string            `json:"text"`
				InlineData map[string]string `json:"inline_data"`
			} `json:"parts"`
		} `json:"contents"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" || r.URL.Path != "/models/m:generateContent" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		img := base64.StdEncoding.EncodeToString([]byte("result-png"))
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"aquí está"},{"inlineData":{"mimeType":"image/png","data":"` + img + `"}}]},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()

	g := &GeminiVTON{APIKey: "k", Model: "m", BaseURL: srv.URL}
	res, err := g.GenerateOutfit(context.Background(), []byte("person"), []OutfitGarment{
		{Image: []byte("shirt"), Slot: "upper_body"},
		{Image: []byte("pants"), Slot: "lower_body", Desc: "chino beige"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.ImageBytes) != "result-png" || res.MimeType != "image/png" || res.ProviderName != "gemini:m" {
		t.Fatalf("result = %q %s %s", res.ImageBytes, res.MimeType, res.ProviderName)
	}
	parts := got.Contents[0].Parts
	if len(parts) != 4 {
		t.Fatalf("parts = %d, want prompt + person + 2 garments", len(parts))
	}
	prompt := parts[0].Text
	for _, want := range []string{"Image 2 is a store product photo of the shirt", "Image 3 is a store product photo of the pants", `listed as "chino beige"`, "real color", "never invent patterns"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if d, _ := base64.StdEncoding.DecodeString(parts[1].InlineData["data"]); string(d) != "person" {
		t.Errorf("image 1 should be the person, got %q", d)
	}
}

func TestGeminiReportsRefusals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"I can't edit this photo."}]},"finishReason":"IMAGE_SAFETY"}]}`))
	}))
	defer srv.Close()

	g := &GeminiVTON{APIKey: "k", BaseURL: srv.URL}
	_, err := g.Generate(context.Background(), VTONRequest{PersonImage: []byte("p"), GarmentImage: []byte("g"), Category: "lower_body"})
	if err == nil || !strings.Contains(err.Error(), "IMAGE_SAFETY") || !strings.Contains(err.Error(), "can't edit") {
		t.Fatalf("err = %v", err)
	}
}

func TestNearestAspectMatchesThePhoto(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want string
	}{{1080, 1920, "9:16"}, {729, 1296, "9:16"}, {1000, 1000, "1:1"}, {1200, 1600, "3:4"}, {1920, 1080, "16:9"}} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, c.w, c.h)), nil); err != nil {
			t.Fatal(err)
		}
		if got := nearestAspect(buf.Bytes()); got != c.want {
			t.Errorf("%dx%d: got %s, want %s", c.w, c.h, got, c.want)
		}
	}
	if got := nearestAspect([]byte("not an image")); got != "" {
		t.Errorf("unreadable image: got %q", got)
	}
}
