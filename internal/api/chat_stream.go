package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// streamPingInterval is how often an SSE comment keeps proxies from closing a
// connection that is waiting on the model. A variable so tests can shorten it.
var streamPingInterval = 10 * time.Second

// sseStream writes server-sent events. Progress reports arrive from several
// goroutines, so every write holds mu. Headers go out with the first event, so
// a request rejected before any progress can still answer with a plain status.
type sseStream struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	mu       sync.Mutex
	started  bool
	finished bool
}

func newSSEStream(w http.ResponseWriter) *sseStream {
	return &sseStream{w: w, rc: http.NewResponseController(w)}
}

// begin sends the headers once. Callers hold mu.
func (s *sseStream) begin() {
	if s.started {
		return
	}
	s.started = true
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(http.StatusOK)
	_ = s.rc.Flush()
}

// event writes one event unless the stream already ended. final ends it.
func (s *sseStream) event(name string, v any, final bool) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("chat stream: marshal event", "event", name, "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.begin()
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data)
	_ = s.rc.Flush()
	if final {
		s.finished = true
	}
}

func (s *sseStream) progress(stage, detail string) {
	p := map[string]string{"stage": stage}
	if detail != "" {
		p["detail"] = detail
	}
	s.event("status", p, false)
}

// ping writes a comment line, only once the stream has started.
func (s *sseStream) ping() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished || !s.started {
		return
	}
	_, _ = fmt.Fprint(s.w, ": ping\n\n")
	_ = s.rc.Flush()
}

// chatStreamHandler is POST /session/{id}/chat/stream: the same turn as /chat,
// reported as server-sent events (status..., then turn or error).
func chatStreamHandler(deps *ChatDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, req, aerr := parseChatRequest(w, r)
		if aerr != nil {
			writeError(w, aerr.Status, aerr.Message)
			return
		}
		s := newSSEStream(w)

		stopPing := make(chan struct{})
		pingDone := make(chan struct{})
		go func() {
			defer close(pingDone)
			t := time.NewTicker(streamPingInterval)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					s.ping()
				case <-stopPing:
					return
				}
			}
		}()
		resp, aerr := runChatTurn(r.Context(), deps, sessionID, req, s.progress)
		close(stopPing)
		<-pingDone

		if r.Context().Err() != nil {
			return // the client went away; nobody is listening
		}
		if aerr != nil {
			s.mu.Lock()
			started := s.started
			s.mu.Unlock()
			if !started {
				writeError(w, aerr.Status, aerr.Message)
				return
			}
			s.event("error", map[string]any{"status": aerr.Status, "message": aerr.Message}, true)
			return
		}
		s.event("turn", resp, true)
	}
}
