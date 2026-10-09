package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ErrNotFound is returned when a session does not exist or has expired.
var ErrNotFound = errors.New("session not found")

// Store persists sessions. Get returns a private copy; Update applies a
// read-modify-write under a per-session lock so background jobs and request
// handlers never clobber each other.
type Store interface {
	Get(ctx context.Context, id string) (*State, error)
	Save(ctx context.Context, s *State) error
	Update(ctx context.Context, id string, fn func(*State) error) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore keeps JSON-encoded sessions in memory with a TTL.
type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memEntry
	locks map[string]*sync.Mutex
	ttl   time.Duration
	done  chan struct{}
}

type memEntry struct {
	data      []byte
	updatedAt time.Time
}

// NewMemoryStore creates a store whose sessions expire ttl after last write.
func NewMemoryStore(ttl time.Duration) *MemoryStore {
	s := &MemoryStore{
		items: map[string]memEntry{},
		locks: map[string]*sync.Mutex{},
		ttl:   ttl,
		done:  make(chan struct{}),
	}
	go s.cleanup()
	return s
}

func (s *MemoryStore) Get(_ context.Context, id string) (*State, error) {
	s.mu.Lock()
	e, ok := s.items[id]
	if ok && s.ttl > 0 && time.Since(e.updatedAt) > s.ttl {
		delete(s.items, id)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	var st State
	if err := json.Unmarshal(e.data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *MemoryStore) Save(_ context.Context, st *State) error {
	st.UpdatedAt = time.Now()
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.items[st.ID] = memEntry{data: data, updatedAt: st.UpdatedAt}
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Update(ctx context.Context, id string, fn func(*State) error) error {
	l := s.lockFor(id)
	l.Lock()
	defer l.Unlock()
	st, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return s.Save(ctx, st)
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.items, id)
	delete(s.locks, id)
	s.mu.Unlock()
	return nil
}

// Stop ends the cleanup goroutine.
func (s *MemoryStore) Stop() { close(s.done) }

func (s *MemoryStore) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

func (s *MemoryStore) cleanup() {
	if s.ttl <= 0 {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for id, e := range s.items {
				if now.Sub(e.updatedAt) > s.ttl {
					delete(s.items, id)
					delete(s.locks, id)
				}
			}
			s.mu.Unlock()
		case <-s.done:
			return
		}
	}
}
