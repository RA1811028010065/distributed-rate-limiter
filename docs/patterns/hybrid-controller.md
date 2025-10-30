# Hybrid selector internals

The hybrid selector fuses real-time telemetry into strategy decisions. Every request updates a lightweight set of metrics on the
`statsEntry`, and the selector evaluates those metrics against heuristics tuned for modern microservice traffic.

## Metrics captured per key/source

* **Mean inter-arrival time** uses an exponential moving average to react quickly without forgetting long-term behaviour.【F:internal/ratelimiter/service.go†L161-L207】
* **Inter-arrival variance** estimates jitter, separating bursty flows from steady ones.
* **Burst score** rises when requests arrive faster than 200 ms apart and decays otherwise, highlighting sudden spikes.
* **Sustain score** rises with sub-second intervals, favouring flows that are consistently high-volume.【F:internal/ratelimiter/service.go†L161-L207】
* **Last switch time** prevents strategy thrash by enforcing a three-second cooldown between transitions.【F:internal/ratelimiter/service.go†L208-L257】

These metrics propagate through the sync bus so every replica evaluates the same telemetry snapshot before making a decision.【F:internal/ratelimiter/service.go†L343-L413】

## Decision table

| Condition | Strategy | Reason |
|-----------|----------|--------|
| Burst score > 0.65 AND stddev > 0.25 | Leaky bucket | `absorbing burst` |
| Sustain score > 0.65 AND stddev < 0.15 | Sliding window | `sustained flow smoothing` |
| otherwise | Token bucket | `balanced throughput` |

Once a strategy is selected the helper resets state (backlog, window counters, timestamps) so the new mode starts with clean
history.【F:internal/ratelimiter/service.go†L258-L343】

## Observability

Every response now includes `algorithm` and `strategy_reason`, enabling clients and tests to assert the active mode. Structured
logs mirror this metadata so the CI pipeline surfaces strategy shifts alongside pass/fail status.【F:internal/ratelimiter/service.go†L119-L140】【F:internal/ratelimiter/service.go†L451-L470】

## Tests and automation

* `TestHybridSwitchesToLeakyBucketOnBurst` drives synthetic bursts to confirm the leaky bucket selection and backlog management.
* `TestHybridSwitchesToSlidingWindowOnSustainedFlow` validates the sustained-flow path and statistical markers.
* The GitHub Actions workflow runs `go test -v ./...`, so these tests emit human-readable logs in the job output, making the
strategy decisions visible in CI.【F:internal/ratelimiter/service_test.go†L104-L186】【F:.github/workflows/ci.yml†L1-L60】
