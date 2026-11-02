// Package tts turns the stylist's message into speech. The ElevenLabs key
// lives only on the server: the app asks POST /tts for the audio.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ElevenLabs calls the text-to-speech API and returns MP3 audio.
type ElevenLabs struct {
	APIKey     string
	VoiceID    string
	Model      string // eleven_multilingual_v2 by default
	BaseURL    string // tests override it
	HTTPClient *http.Client
}

// Speak returns the MP3 audio for text.
func (e *ElevenLabs) Speak(ctx context.Context, text string) ([]byte, error) {
	model := e.Model
	if model == "" {
		model = "eleven_multilingual_v2"
	}
	base := e.BaseURL
	if base == "" {
		base = "https://api.elevenlabs.io"
	}
	body, err := json.Marshal(map[string]any{
		"text":           text,
		"model_id":       model,
		"voice_settings": map[string]float64{"stability": 0.5, "similarity_boost": 0.75},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/text-to-speech/"+e.VoiceID, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", e.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	client := e.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading elevenlabs audio: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("elevenlabs returned %d: %s", resp.StatusCode, msg)
	}
	return data, nil
}
