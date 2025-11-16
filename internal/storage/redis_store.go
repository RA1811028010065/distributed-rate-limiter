package storage

import (
	"context"
	"log"
)

// RedisConfigStore is a thin wrapper that can be swapped for a real Redis client.
type RedisConfigStore struct {
	delegate ConfigStore
	logger   *log.Logger
}

// NewRedisConfigStore returns a redis-backed store placeholder that persists data in memory while logging intents.
func NewRedisConfigStore(logger *log.Logger) ConfigStore {
	return &RedisConfigStore{delegate: NewMemoryConfigStore(), logger: logger}
}

func (r *RedisConfigStore) Get(ctx context.Context, key string) (*RateLimitConfig, error) {
	r.log("get", key)
	return r.delegate.Get(ctx, key)
}

func (r *RedisConfigStore) Set(ctx context.Context, cfg RateLimitConfig) error {
	r.log("set", cfg.Key)
	return r.delegate.Set(ctx, cfg)
}

func (r *RedisConfigStore) Delete(ctx context.Context, key string) error {
	r.log("delete", key)
	return r.delegate.Delete(ctx, key)
}

func (r *RedisConfigStore) List(ctx context.Context) ([]RateLimitConfig, error) {
	r.log("list", "*")
	return r.delegate.List(ctx)
}

func (r *RedisConfigStore) log(action, key string) {
	if r.logger != nil {
		r.logger.Printf("[redis-placeholder] %s %s", action, key)
	}
}
