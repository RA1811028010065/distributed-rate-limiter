package storage

import (
	"context"
	"log"
)

// PostgresConfigStore mirrors the ConfigStore contract and can be backed by a real database driver later.
type PostgresConfigStore struct {
	delegate ConfigStore
	logger   *log.Logger
}

// NewPostgresConfigStore returns a memory-backed placeholder that logs persistence intents.
func NewPostgresConfigStore(logger *log.Logger) ConfigStore {
	return &PostgresConfigStore{delegate: NewMemoryConfigStore(), logger: logger}
}

func (p *PostgresConfigStore) Get(ctx context.Context, key string) (*RateLimitConfig, error) {
	p.log("get", key)
	return p.delegate.Get(ctx, key)
}

func (p *PostgresConfigStore) Set(ctx context.Context, cfg RateLimitConfig) error {
	p.log("set", cfg.Key)
	return p.delegate.Set(ctx, cfg)
}

func (p *PostgresConfigStore) Delete(ctx context.Context, key string) error {
	p.log("delete", key)
	return p.delegate.Delete(ctx, key)
}

func (p *PostgresConfigStore) List(ctx context.Context) ([]RateLimitConfig, error) {
	p.log("list", "*")
	return p.delegate.List(ctx)
}

func (p *PostgresConfigStore) log(action, key string) {
	if p.logger != nil {
		p.logger.Printf("[postgres-placeholder] %s %s", action, key)
	}
}
