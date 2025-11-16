package ratelimiter

import (
	"context"
	"testing"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/internal/storage"
)

func TestAllowRespectsConfiguredLimit(t *testing.T) {
	store := storage.NewMemoryConfigStore()
	limiter := New(WithConfigStore(store), WithClock(fixedClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))))

	if err := limiter.ConfigureLimit(context.Background(), storage.RateLimitConfig{Key: "user:1", MaxTokens: 2, RefillRate: 0}); err != nil {
		t.Fatalf("configure limit: %v", err)
	}

	res, err := limiter.Allow(context.Background(), &pbcodec.AllowRequest{Key: "user:1", Tokens: 1, Source: "192.0.2.5"})
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if !res.Allowed || res.AllowedHits != 1 || res.DeniedHits != 0 {
		t.Fatalf("unexpected first response: %+v", res)
	}

	res, _ = limiter.Allow(context.Background(), &pbcodec.AllowRequest{Key: "user:1", Tokens: 1, Source: "192.0.2.5"})
	if !res.Allowed || res.AllowedHits != 2 {
		t.Fatalf("expected second request allowed, got %+v", res)
	}

	res, _ = limiter.Allow(context.Background(), &pbcodec.AllowRequest{Key: "user:1", Tokens: 1, Source: "192.0.2.5"})
	if res.Allowed || res.DeniedHits != 1 {
		t.Fatalf("expected third request denied, got %+v", res)
	}
}

func TestRequiresConfiguration(t *testing.T) {
	limiter := New()
	res, err := limiter.Allow(context.Background(), &pbcodec.AllowRequest{Key: "missing", Tokens: 1})
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if res.Allowed {
		t.Fatalf("expected request to be denied due to missing config")
	}
}

func fixedClock(ts time.Time) func() time.Time {
	current := ts
	return func() time.Time {
		val := current
		current = current.Add(250 * time.Millisecond)
		return val
	}
}
