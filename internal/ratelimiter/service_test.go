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
	if res.Algorithm != strategyTokenBucket {
		t.Fatalf("expected token bucket strategy, got %s", res.Algorithm)
	}
	t.Logf("first response: %+v", res)

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
	t.Logf("denied response: %+v", res)

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
	if row.Algorithm != strategyTokenBucket {
		t.Fatalf("expected stats to reflect token bucket, got %s", row.Algorithm)
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
				t.Logf("synced row: %+v", rows[0])
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
	t.Logf("limiterB denial: %+v", res)
	if res.AllowedHits != 2 || res.DeniedHits != 1 {
		t.Fatalf("unexpected counters on limiterB after denial: %+v", res)
	}
}

func TestHybridSwitchesToLeakyBucketOnBurst(t *testing.T) {
	times := []time.Time{}
	base := time.Date(2025, 10, 30, 2, 0, 0, 0, time.UTC)
	offsets := []time.Duration{
		0,
		40 * time.Millisecond,
		90 * time.Millisecond,
		130 * time.Millisecond,
		180 * time.Millisecond,
		1 * time.Second,
		1*time.Second + 30*time.Millisecond,
		1*time.Second + 60*time.Millisecond,
		1*time.Second + 90*time.Millisecond,
		1*time.Second + 110*time.Millisecond,
		1*time.Second + 140*time.Millisecond,
		2*time.Second + 10*time.Millisecond,
		2*time.Second + 40*time.Millisecond,
	}
	for _, off := range offsets {
		times = append(times, base.Add(off))
	}
	clock := sequenceClock(times)
	limiter := New(nil, WithClock(clock))

	req := &pbcodec.AllowRequest{Key: "burst", Tokens: 1, MaxTokens: 3, RefillRate: 1, Source: "198.51.100.9"}
	var leakySeen bool
	for i := 0; i < len(offsets); i++ {
		res, err := limiter.Allow(context.Background(), req)
		if err != nil {
			t.Fatalf("burst allow returned error: %v", err)
		}
		t.Logf("burst attempt %d -> allowed=%v strategy=%s reason=%s remaining=%d", i, res.Allowed, res.Algorithm, res.StrategyReason, res.RemainingTokens)
		if res.Algorithm == strategyLeakyBucket {
			leakySeen = true
			if i > 6 && res.Allowed {
				// flood a few more to force backlog saturation.
				for j := 0; j < 3; j++ {
					res, err = limiter.Allow(context.Background(), req)
					if err != nil {
						t.Fatalf("follow-up allow returned error: %v", err)
					}
					t.Logf("extra burst attempt %d -> allowed=%v remaining=%d", j, res.Allowed, res.RemainingTokens)
				}
			}
		}
	}
	if !leakySeen {
		t.Fatalf("expected leaky bucket strategy to be selected for burst pattern")
	}
	rows := limiter.StatsTable()
	if len(rows) != 1 {
		t.Fatalf("expected one stats row, got %d", len(rows))
	}
	row := rows[0]
	if row.Algorithm != strategyLeakyBucket {
		t.Fatalf("expected stats to show leaky bucket, got %s", row.Algorithm)
	}
	if row.BurstScore <= row.SustainScore {
		t.Fatalf("expected burst score > sustain score, got burst=%f sustain=%f", row.BurstScore, row.SustainScore)
	}
}

func TestHybridSwitchesToSlidingWindowOnSustainedFlow(t *testing.T) {
	base := time.Date(2025, 10, 30, 3, 0, 0, 0, time.UTC)
	times := []time.Time{}
	for i := 0; i < 20; i++ {
		times = append(times, base.Add(time.Duration(i)*300*time.Millisecond))
	}
	clock := sequenceClock(times)
	limiter := New(nil, WithClock(clock))

	req := &pbcodec.AllowRequest{Key: "steady", Tokens: 1, MaxTokens: 5, RefillRate: 5, Source: "203.0.113.4"}

	var slidingSeen bool
	for i := 0; i < 15; i++ {
		res, err := limiter.Allow(context.Background(), req)
		if err != nil {
			t.Fatalf("steady allow returned error: %v", err)
		}
		t.Logf("steady attempt %d -> allowed=%v strategy=%s remaining=%d", i, res.Allowed, res.Algorithm, res.RemainingTokens)
		if res.Algorithm == strategySlidingWindow {
			slidingSeen = true
			break
		}
	}
	if !slidingSeen {
		t.Fatalf("expected sliding window strategy for steady flow pattern")
	}
	rows := limiter.StatsTable()
	if len(rows) != 1 {
		t.Fatalf("expected one stats row, got %d", len(rows))
	}
	row := rows[0]
	if row.Algorithm != strategySlidingWindow {
		t.Fatalf("expected stats to show sliding window, got %s", row.Algorithm)
	}
	if row.SustainScore <= 0.5 {
		t.Fatalf("expected sustain score to be high, got %f", row.SustainScore)
	}
	if row.IntervalStdDev >= 0.2 {
		t.Fatalf("expected low variance during steady flow, got %f", row.IntervalStdDev)
	}
}

func TestAllowRejectsMissingKey(t *testing.T) {
	limiter := New(nil)
	res, err := limiter.Allow(context.Background(), &pbcodec.AllowRequest{Key: "", Tokens: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatalf("expected request with empty key to be rejected")
	}
	if res.Message == "" {
		t.Fatalf("expected validation error message")
	}
}

func sequenceClock(times []time.Time) func() time.Time {
	idx := 0
	last := times[len(times)-1]
	return func() time.Time {
		if idx >= len(times) {
			last = last.Add(100 * time.Millisecond)
			return last
		}
		value := times[idx]
		idx++
		last = value
		return value
	}
}
