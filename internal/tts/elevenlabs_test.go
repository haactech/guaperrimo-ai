package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpeakSendsTheVoiceAndReturnsAudio(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != "k" || r.URL.Path != "/v1/text-to-speech/voz" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3"))
	}))
	defer srv.Close()

	e := &ElevenLabs{APIKey: "k", VoiceID: "voz", BaseURL: srv.URL}
	audio, err := e.Speak(context.Background(), "Hola")
	if err != nil || string(audio) != "mp3" {
		t.Fatalf("audio=%q err=%v", audio, err)
	}
	if got["text"] != "Hola" || got["model_id"] != "eleven_multilingual_v2" {
		t.Fatalf("request body: %v", got)
	}
}

func TestSpeakReportsPlanErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		_, _ = w.Write([]byte(`{"detail":{"code":"paid_plan_required"}}`))
	}))
	defer srv.Close()
	_, err := (&ElevenLabs{APIKey: "k", VoiceID: "v", BaseURL: srv.URL}).Speak(context.Background(), "Hola")
	if err == nil || !strings.Contains(err.Error(), "402") || !strings.Contains(err.Error(), "paid_plan_required") {
		t.Fatalf("err = %v", err)
	}
}
