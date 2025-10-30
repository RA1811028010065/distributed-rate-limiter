# Sliding window strategy

Sliding window mode guarantees that steady flows never exceed their per-second allotment, even when the cluster receives requests
from multiple replicas simultaneously. The implementation tracks the start of the current observation window and counts the number
of successful admissions within that window. When the counter exceeds the configured capacity the request is denied until the next
window begins.【F:internal/ratelimiter/service.go†L318-L343】

## Activation criteria

* Sustain score above 0.65 indicates that inter-arrival times are consistently short.
* Low inter-arrival variance (< 0.15) shows that the flow is steady rather than bursty.
* When both conditions are met, the controller switches to sliding window with the `sustained flow smoothing` reason and resets the
window counters.【F:internal/ratelimiter/service.go†L214-L257】【F:internal/ratelimiter/service.go†L329-L343】

## State synchronisation

Sync messages carry the window count, window start time, and strategy metadata so replicas can keep the same window boundaries. As
soon as the window expires, each instance resets the counter locally, ensuring deterministic behaviour regardless of which pod
handles the next request.【F:internal/ratelimiter/service.go†L343-L413】

## Diagnostics

* `/api/v1/stats` reports the sustain score and standard deviation of the inter-arrival time, which should drop sharply when the
sliding window engages.【F:internal/ratelimiter/service.go†L199-L213】
* Logs expose `algorithm=sliding_window` so you can correlate fairness enforcement with upstream traffic patterns.【F:internal/ratelimiter/service.go†L451-L470】

## Reproducing the behaviour

1. Launch the stack (`make run`).
2. Issue a sustained stream, e.g. `hey -c5 -q20 -z10s -H 'Content-Type: application/json' -m POST http://localhost:8080/api/v1/allow -d '{"key":"steady","max_tokens":5,"refill_rate":5}'`.
3. Responses will gradually report `"algorithm":"sliding_window"`; once the window counter fills, extra requests during the same
interval receive `allowed=false` until the next window begins.
4. `TestHybridSwitchesToSlidingWindowOnSustainedFlow` covers this path with a deterministic clock, verifying both the strategy
switch and the statistical markers.【F:internal/ratelimiter/service_test.go†L148-L186】
