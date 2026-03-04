package db

import (
	"sync"
	"time"
)

type SessionTracker struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]time.Time),
	}
}

func (s *SessionTracker) Start(provider, profile string) {
	if s == nil {
		return
	}
	key := sessionKey(provider, profile)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]time.Time)
	}
	s.sessions[key] = time.Now()
}

func (s *SessionTracker) End(provider, profile string) time.Duration {
	if s == nil {
		return 0
	}
	key := sessionKey(provider, profile)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	start, ok := s.sessions[key]
	if !ok {
		return 0
	}
	delete(s.sessions, key)

	if start.IsZero() {
		return 0
	}
	if now.Before(start) {
		return 0
	}
	return now.Sub(start)
}

func sessionKey(provider, profile string) string {
	return provider + "/" + profile
}
