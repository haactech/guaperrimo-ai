package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// maxTTSChars bounds one request: a stylist message is a few sentences, and
// every character is billed by ElevenLabs.
const maxTTSChars = 1200

// Speaker turns text into MP3 audio.
type Speaker interface {
	Speak(ctx context.Context, text string) ([]byte, error)
}

// TTSDeps wires POST /tts. The provider key stays on the server.
type TTSDeps struct {
	Speaker Speaker
	Timeout time.Duration

	mu    sync.Mutex
	cache map[string][]byte // recent texts, so a replay is not billed twice
	order []string
}

const ttsCacheSize = 64

type ttsRequest struct {
	Text string `json:"text"`
}

func ttsHandler(d *TTSDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ttsRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		text := strings.TrimSpace(req.Text)
		if text == "" {
			writeError(w, http.StatusBadRequest, "missing field: text")
			return
		}
		if utf8.RuneCountInString(text) > maxTTSChars {
			writeError(w, http.StatusBadRequest, "text too long")
			return
		}
		if audio, ok := d.cached(text); ok {
			writeAudio(w, audio)
			return
		}
		timeout := d.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		audio, err := d.Speaker.Speak(ctx, text)
		if err != nil {
			slog.ErrorContext(ctx, "tts: speak failed", "chars", utf8.RuneCountInString(text), "error", err)
			writeError(w, http.StatusBadGateway, "text to speech failed")
			return
		}
		d.store(text, audio)
		writeAudio(w, audio)
	}
}

func writeAudio(w http.ResponseWriter, audio []byte) {
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

func (d *TTSDeps) cached(text string) ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a, ok := d.cache[text]
	return a, ok
}

func (d *TTSDeps) store(text string, audio []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		d.cache = map[string][]byte{}
	}
	if _, ok := d.cache[text]; ok {
		return
	}
	if len(d.order) >= ttsCacheSize {
		delete(d.cache, d.order[0])
		d.order = d.order[1:]
	}
	d.cache[text] = audio
	d.order = append(d.order, text)
}
