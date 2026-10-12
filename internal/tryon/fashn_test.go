package tryon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFashnRunPollAndDecode(t *testing.T) {
	var polls atomic.Int32
	var got map[string]any
	out := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("result-jpeg"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/run":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"id":"pred1","error":null}`))
		case r.URL.Path == "/v1/status/pred1":
			if polls.Add(1) < 2 {
				_, _ = w.Write([]byte(`{"id":"pred1","status":"processing"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"pred1","status":"completed","output":["` + out + `"]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := &FashnVTON{APIKey: "k", BaseURL: srv.URL + "/v1", PollInterval: 10 * time.Millisecond}
	res, err := f.Generate(context.Background(), VTONRequest{PersonImage: []byte{0xFF, 0xD8, 0xFF}, GarmentImage: []byte("\x89PNG\r\n\x1a\n"), Category: "lower_body"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.ImageBytes) != "result-jpeg" || res.MimeType != "image/jpeg" || res.ProviderName != "fashn" {
		t.Fatalf("result: %+v", res)
	}
	in := got["inputs"].(map[string]any)
	if got["model_name"] != "tryon-v1.6" || in["category"] != "bottoms" || in["return_base64"] != true || in["mode"] != "balanced" {
		t.Fatalf("request: %v", got)
	}
	if !strings.HasPrefix(in["model_image"].(string), "data:image/jpeg;base64,") || !strings.HasPrefix(in["garment_image"].(string), "data:image/png;base64,") {
		t.Fatalf("images must be sent as data URIs: %v / %v", in["model_image"], in["garment_image"])
	}
}

func TestFashnFailureIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"p2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"p2","status":"failed","error":{"name":"PoseError","message":"no pose detected"}}`))
	}))
	defer srv.Close()
	f := &FashnVTON{APIKey: "k", BaseURL: srv.URL, PollInterval: 5 * time.Millisecond}
	_, err := f.Generate(context.Background(), VTONRequest{PersonImage: []byte("a"), GarmentImage: []byte("b"), Category: "upper_body"})
	if err == nil || !strings.Contains(err.Error(), "PoseError") {
		t.Fatalf("expected PoseError, got %v", err)
	}
}

func TestFashnCategoryMapping(t *testing.T) {
	for slot, want := range map[string]string{"upper_body": "tops", "outerwear": "tops", "lower_body": "bottoms", "x": "auto"} {
		if got := fashnCategory(slot); got != want {
			t.Errorf("%s -> %s, want %s", slot, got, want)
		}
	}
}
