package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"stylerag/internal/session"
)

type fakeSpeaker struct {
	calls atomic.Int32
	fail  bool
}

func (f *fakeSpeaker) Speak(_ context.Context, text string) ([]byte, error) {
	f.calls.Add(1)
	if f.fail {
		return nil, errors.New("elevenlabs returned 402")
	}
	return []byte("mp3:" + text), nil
}

func ttsPost(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestTTSReturnsAudioAndCachesRepeats(t *testing.T) {
	sp := &fakeSpeaker{}
	srv := httptest.NewServer(NewRouter(Deps{Store: session.NewMemoryStore(time.Hour), TTS: &TTSDeps{Speaker: sp}}))
	defer srv.Close()

	resp, body := ttsPost(t, srv.URL+"/tts", `{"text":"¿Para qué ocasión?"}`)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/mpeg" || body != "mp3:¿Para qué ocasión?" {
		t.Fatalf("%d %s %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	ttsPost(t, srv.URL+"/tts", `{"text":"¿Para qué ocasión?"}`)
	if sp.calls.Load() != 1 {
		t.Fatalf("a repeated text must not be billed twice, calls=%d", sp.calls.Load())
	}
	for _, bad := range []string{`{}`, `{"text":"  "}`, `nope`, `{"text":"` + strings.Repeat("a", maxTTSChars+1) + `"}`} {
		if resp, _ := ttsPost(t, srv.URL+"/tts", bad); resp.StatusCode != 400 {
			t.Errorf("%.30s → %d, want 400", bad, resp.StatusCode)
		}
	}
}

func TestTTSProviderErrorIs502AndMissingIs404(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Deps{Store: session.NewMemoryStore(time.Hour), TTS: &TTSDeps{Speaker: &fakeSpeaker{fail: true}}}))
	defer srv.Close()
	if resp, _ := ttsPost(t, srv.URL+"/tts", `{"text":"Hola"}`); resp.StatusCode != 502 {
		t.Fatalf("provider failure → %d, want 502", resp.StatusCode)
	}
	off := httptest.NewServer(NewRouter(Deps{Store: session.NewMemoryStore(time.Hour)}))
	defer off.Close()
	if resp, _ := ttsPost(t, off.URL+"/tts", `{"text":"Hola"}`); resp.StatusCode != 404 && resp.StatusCode != 405 {
		t.Fatalf("without a key the route is not registered, got %d", resp.StatusCode)
	}
}
