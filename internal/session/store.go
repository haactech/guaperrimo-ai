package session

import (
	"fmt"
	"sync"
	"time"
)

// SessionStore defines the interface for session persistence.
type SessionStore interface {
	Get(id string) (*SessionState, error)
	Save(state *SessionState) error
	Delete(id string) error
}

// InMemoryStore is a sync.Map-backed session store with TTL-based cleanup.
type InMemoryStore struct {
	data sync.Map
	ttl  time.Duration
	done chan struct{}
}

func NewInMemoryStore(ttl time.Duration) *InMemoryStore {
	s := &InMemoryStore{
		ttl:  ttl,
		done: make(chan struct{}),
	}
	go s.cleanup()
	return s
}

func (s *InMemoryStore) Get(id string) (*SessionState, error) {
	v, ok := s.data.Load(id)
	if !ok {
		return nil, fmt.Errorf("session not found or expired")
	}
	state := v.(*SessionState)
	if time.Since(state.UpdatedAt) > s.ttl {
		s.data.Delete(id)
		return nil, fmt.Errorf("session not found or expired")
	}
	return state, nil
}

func (s *InMemoryStore) Save(state *SessionState) error {
	state.UpdatedAt = time.Now()
	s.data.Store(state.ID, state)
	return nil
}

func (s *InMemoryStore) Delete(id string) error {
	s.data.Delete(id)
	return nil
}

func (s *InMemoryStore) Stop() {
	close(s.done)
}

func (s *InMemoryStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			s.data.Range(func(key, value any) bool {
				state := value.(*SessionState)
				if now.Sub(state.UpdatedAt) > s.ttl {
					s.data.Delete(key)
				}
				return true
			})
		case <-s.done:
			return
		}
	}
}
