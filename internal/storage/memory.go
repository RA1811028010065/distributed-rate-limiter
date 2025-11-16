package storage

import (
	"context"
	"sync"
)

type memoryStore struct {
	mu     sync.RWMutex
	limits map[string]RateLimitConfig
}

// NewMemoryConfigStore returns an in-memory implementation suitable for tests and development.
func NewMemoryConfigStore() ConfigStore {
	return &memoryStore{limits: make(map[string]RateLimitConfig)}
}

func (m *memoryStore) Get(_ context.Context, key string) (*RateLimitConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.limits[key]
	if !ok {
		return nil, ErrNotFound
	}
	copy := cfg
	return &copy, nil
}

func (m *memoryStore) Set(_ context.Context, cfg RateLimitConfig) error {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limits[cfg.Key] = cfg
	return nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.limits, key)
	return nil
}

func (m *memoryStore) List(_ context.Context) ([]RateLimitConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RateLimitConfig, 0, len(m.limits))
	for _, cfg := range m.limits {
		out = append(out, cfg)
	}
	return out, nil
}
