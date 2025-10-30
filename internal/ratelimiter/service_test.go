package ratelimiter

import (
	"context"
	"testing"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/pkg/natsutil"
)

func TestAllowEnforcesLimitAndUpdatesStats(t *testing.T) {
	bus := natsutil.NewInMemoryBus()
	current := time.Date(2025, 10, 30, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		now := current
		current = current.Add(250 * time.Millisecond)
		return now
	}
	limiter := New(bus, WithClock(clock))

	req := &pbcodec.AllowRequest{Key: "user:1", Tokens: 1, MaxTokens: 2, RefillRate: 0, Source: "192.0.2.5"}

	res, err := limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("expected first request to be allowed")
	}
	if res.AllowedHits != 1 || res.DeniedHits != 0 {
		t.Fatalf("unexpected counters after first call: %+v", res)
	}
	if res.LastAllowedAt == "" {
		t.Fatalf("expected last allowed timestamp to be populated")
	}

	res, err = limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("expected second request within capacity to be allowed")
	}
	if res.AllowedHits != 2 || res.DeniedHits != 0 {
		t.Fatalf("unexpected counters after second call: %+v", res)
	}

	res, err = limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("allow returned error: %v", err)
	}
	if res.Allowed {
		t.Fatalf("expected third request to be denied")
	}
	if res.AllowedHits != 2 || res.DeniedHits != 1 {
		t.Fatalf("unexpected counters after denial: %+v", res)
	}
	if res.LastDeniedAt == "" {
		t.Fatalf("expected last denied timestamp to be populated")
	}

	rows := limiter.StatsTable()
	if len(rows) != 1 {
		t.Fatalf("expected one stats row, got %d", len(rows))
	}
	row := rows[0]
	if row.Allowed != 2 || row.Denied != 1 {
		t.Fatalf("unexpected stats row: %+v", row)
	}
	if row.Source != "192.0.2.5" {
		t.Fatalf("expected source to be recorded, got %q", row.Source)
	}
	if row.RemainingTokens != 0 {
		t.Fatalf("expected remaining tokens to be zero, got %f", row.RemainingTokens)
	}
}

func TestSyncAcrossInstancesPropagatesBucketsAndStats(t *testing.T) {
	bus := natsutil.NewInMemoryBus()

	clockA := func() func() time.Time {
		current := time.Date(2025, 10, 30, 1, 0, 0, 0, time.UTC)
		return func() time.Time {
			now := current
			current = current.Add(100 * time.Millisecond)
			return now
		}
	}()
	limiterA := New(bus, WithClock(clockA))
	limiterB := New(bus)

	req := &pbcodec.AllowRequest{Key: "team:42", Tokens: 1, MaxTokens: 2, RefillRate: 0, Source: "10.1.0.8"}

	if res, err := limiterA.Allow(context.Background(), req); err != nil || !res.Allowed {
		t.Fatalf("limiterA first allow should succeed, got res=%+v err=%v", res, err)
	}
	if res, err := limiterA.Allow(context.Background(), req); err != nil || !res.Allowed {
		t.Fatalf("limiterA second allow should succeed, got res=%+v err=%v", res, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		rows := limiterB.StatsTable()
		if len(rows) == 1 {
			if rows[0].Allowed == 2 && rows[0].RemainingTokens <= 0.01 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("state did not synchronize before timeout; rows=%v", rows)
		}
		time.Sleep(10 * time.Millisecond)
	}

	res, err := limiterB.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("limiterB allow returned error: %v", err)
	}
	if res.Allowed {
		t.Fatalf("limiterB should deny once tokens are depleted")
	}
	if res.AllowedHits != 2 || res.DeniedHits != 1 {
		t.Fatalf("unexpected counters on limiterB after denial: %+v", res)
	}
}
