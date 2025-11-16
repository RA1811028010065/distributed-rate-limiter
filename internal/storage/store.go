package storage

import (
	"context"
	"errors"
)

// RateLimitConfig defines the allowed capacity and refill behaviour for a key.
type RateLimitConfig struct {
	Key        string  `json:"key"`
	MaxTokens  int64   `json:"max_tokens"`
	RefillRate float64 `json:"refill_rate"`
}

var ErrNotFound = errors.New("rate limit not configured")

// ConfigStore persists rate limit definitions.
type ConfigStore interface {
	Get(ctx context.Context, key string) (*RateLimitConfig, error)
	Set(ctx context.Context, cfg RateLimitConfig) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]RateLimitConfig, error)
}
