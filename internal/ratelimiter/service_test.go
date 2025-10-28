package ratelimiter

import (
	"context"
	"testing"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/pkg/natsutil"
)

func TestAllowEnforcesLimit(t *testing.T) {
	bus := natsutil.NewInMemoryBus()
	limiter := New(bus)

	req := &pbcodec.AllowRequest{Key: "user:1", Tokens: 1, MaxTokens: 2, RefillRate: 1}
	res, err := limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("expected first request to be allowed")
	}

	res, err = limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("expected second request to be allowed due to capacity")
	}

	res, err = limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if res.Allowed {
		t.Fatalf("expected third request to be rate limited")
	}
}

func TestSyncAcrossInstances(t *testing.T) {
	bus := natsutil.NewInMemoryBus()
	limiterA := New(bus)
	limiterB := New(bus)

	req := &pbcodec.AllowRequest{Key: "team:42", Tokens: 1, MaxTokens: 2, RefillRate: 0}
	if res, _ := limiterA.Allow(context.Background(), req); !res.Allowed {
		t.Fatalf("limiterA should allow first request")
	}
	if res, _ := limiterA.Allow(context.Background(), req); !res.Allowed {
		t.Fatalf("limiterA should allow second request within capacity")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	var state map[string]map[string]interface{}
	for {
		state = limiterB.DebugState()
		if bucket, ok := state["team:42"]; ok {
			if tokens, ok := bucket["tokens"].(float64); ok && tokens <= 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("state did not synchronize before timeout: %#v", state)
		}
		time.Sleep(10 * time.Millisecond)
	}

	res, err := limiterB.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("limiterB allow error: %v", err)
	}
	if res.Allowed {
		t.Fatalf("limiterB should see depleted bucket and reject request")
	}
}
