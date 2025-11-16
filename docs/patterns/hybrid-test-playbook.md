# Hybrid strategy test playbook

This guide lists positive and negative tests that demonstrate how quickly the hybrid selector pivots between algorithms, how it preserves or resets token accounting, and how combined scenarios behave. Each scenario is meant to be executable as-is with `go test ./...` while giving newcomers an intuition for how requests are handled.

## Why these tests matter
- **Token safety first** – every request normalises tokens, capacity, and refill rate before touching shared state, so tests must verify both allowed and denied paths keep counters coherent.【F:internal/ratelimiter/service.go†L172-L227】
- **Strategy speed with guardrails** – the selector samples burstiness and sustain scores but enforces a three-second cooldown to prevent thrashing; tests should show both swift pivots when the cooldown expires and stability while it is in effect.【F:internal/ratelimiter/service.go†L284-L360】
- **State continuity** – bucket refills/backlog drains continue across algorithm switches because state is rebased when a new strategy activates; validation should confirm whether tokens carry over or are reset for a clean slate.【F:internal/ratelimiter/service.go†L365-L494】【F:internal/ratelimiter/service.go†L592-L635】
- **Cluster agreement** – sync events propagate counters and strategy decisions so replicas deny or allow identically after a burst; multi-node tests keep regressions visible.【F:internal/ratelimiter/service.go†L245-L281】【F:internal/ratelimiter/service.go†L650-L720】

## Positive coverage (expected to allow)
1. **Fresh bucket happy path** – first and second requests on a new key consume tokens from the token bucket and leave counters/last-allowed timestamps populated. Mirrors `TestAllowEnforcesLimitAndUpdatesStats`’s opening assertions.【F:internal/ratelimiter/service_test.go†L12-L52】
2. **Refill-aware allowance** – after a brief idle period, ensureBucket refills tokens based on elapsed time so a follow-up request is accepted without resetting counters. Inspect `RemainingTokens` in the stats table to confirm the refill surfaced.【F:internal/ratelimiter/service.go†L592-L635】【F:internal/ratelimiter/service.go†L523-L553】
3. **Burst pivot to leaky bucket** – drive sub-200 ms intervals until the burst score crosses the threshold and assert the algorithm flips to leaky bucket with a backlog that still honours capacity. This mirrors `TestHybridSwitchesToLeakyBucketOnBurst` and proves the selector reacts quickly once the cooldown allows.【F:internal/ratelimiter/service.go†L284-L360】【F:internal/ratelimiter/service_test.go†L138-L199】
4. **Sustained flow pivot to sliding window** – run steady 300 ms intervals so the sustain score dominates and the strategy changes to sliding window, keeping window counts intact across requests. Based on `TestHybridSwitchesToSlidingWindowOnSustainedFlow`.【F:internal/ratelimiter/service.go†L284-L360】【F:internal/ratelimiter/service.go†L425-L461】【F:internal/ratelimiter/service_test.go†L200-L240】
5. **Cross-replica agreement** – start traffic on replica A, wait for sync, then confirm replica B denies once the shared bucket empties, proving that algorithm choice and counters replicate. Captured in `TestSyncAcrossInstancesPropagatesBucketsAndStats`.【F:internal/ratelimiter/service.go†L245-L281】【F:internal/ratelimiter/service.go†L650-L720】【F:internal/ratelimiter/service_test.go†L87-L136】

## Negative coverage (expected to deny)
1. **Capacity exhaustion** – a third consecutive request on a tiny bucket should be denied, increment `DeniedHits`, and record `LastDeniedAt` without losing prior allow counts, as asserted in `TestAllowEnforcesLimitAndUpdatesStats`.【F:internal/ratelimiter/service_test.go†L53-L85】
2. **Validation failure** – an empty `key` should be rejected immediately with an explanatory message while leaving stats untouched. See `TestAllowRejectsMissingKey`.【F:internal/ratelimiter/service.go†L174-L223】【F:internal/ratelimiter/service_test.go†L242-L254】
3. **Leaky backlog overflow** – when burst traffic keeps backlog near capacity, extra requests should be denied while `Backlog` continues draining at `RefillRate` so tokens resume once the queue clears.【F:internal/ratelimiter/service.go†L390-L423】【F:internal/ratelimiter/service.go†L463-L494】
4. **Sliding window saturation** – if the window count hits capacity before the window rolls over, remaining tokens drop to zero and requests are denied until the window resets.【F:internal/ratelimiter/service.go†L425-L461】

## Hybrid scenario matrix
- **Token bucket → leaky bucket**: start with balanced traffic, then inject a short burst so `BurstScore` climbs above 0.65 and variance rises; expect a strategy flip after the three-second cooldown and a backlog seeded from the current token balance.【F:internal/ratelimiter/service.go†L284-L360】【F:internal/ratelimiter/service.go†L463-L494】
- **Leaky bucket → token bucket recovery**: once burst pressure subsides (longer gaps lower burst score), cooldown expiry should return the algorithm to token bucket with tokens capped at capacity and backlog cleared to avoid double-counting queued work.【F:internal/ratelimiter/service.go†L284-L360】【F:internal/ratelimiter/service.go†L463-L494】
- **Token bucket → sliding window**: sustained near-capacity flow with low variance pushes `SustainScore` above threshold, switching to sliding window while resetting counters so window calculations start fresh, avoiding inherited jitter.【F:internal/ratelimiter/service.go†L284-L360】【F:internal/ratelimiter/service.go†L425-L461】
- **Three-way hop (burst → steady → balanced)**: simulate a burst that triggers leaky bucket, then transition to steady cadence to slide into the window strategy, and finally idle long enough for scores to decay so token bucket returns as the default. This confirms metric decay, cooldown enforcement, and state resets across multiple pivots.【F:internal/ratelimiter/service.go†L284-L360】【F:internal/ratelimiter/service.go†L365-L423】【F:internal/ratelimiter/service.go†L463-L494】

## How to read outcomes
- **Per-request responses** include `algorithm`, `strategy_reason`, and counters so tests can assert the active path without peeking into internals.【F:internal/ratelimiter/service.go†L240-L279】
- **Stats table snapshots** expose remaining tokens, burst/sustain scores, and interval variance, making it easy to confirm whether tokens were refilled, backlogged, or windowed during hybrid transitions.【F:internal/ratelimiter/service.go†L523-L553】
- **Debug state** shows per-key internals (`backlog`, `last_leak`, `window_count`), helpful for hybrid sequences that combine two or three strategies in a single run.【F:internal/ratelimiter/service.go†L555-L576】

Use this playbook as a checklist when adding new features or transports: if a change affects token accounting, strategy thresholds, or sync propagation, it should keep every scenario here green.
