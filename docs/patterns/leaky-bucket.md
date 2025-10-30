# Leaky bucket strategy

The leaky bucket mode activates when the controller detects bursty arrivals that would otherwise exhaust shared capacity. Rather
than admitting requests at full speed, the bucket maintains a backlog counter and drains it at the configured refill rate,
smoothing the downstream load.【F:internal/ratelimiter/service.go†L258-L317】【F:internal/ratelimiter/service.go†L329-L343】

## Activation criteria

* Burst score above 0.65 and inter-arrival variance above 0.25 indicate highly irregular traffic.
* When these conditions are met and at least three seconds have passed since the last switch, the strategy flips to leaky bucket
with the `absorbing burst` reason and resets the backlog tracker.【F:internal/ratelimiter/service.go†L214-L257】【F:internal/ratelimiter/service.go†L318-L328】

## State synchronisation

Each synchronisation event ships the backlog size, last leak timestamp, and strategy metadata so other replicas can mirror the
queue depth and leak schedule. The receiving node updates those fields whenever the event version is newer, guaranteeing that
denials stay consistent across pods.【F:internal/ratelimiter/service.go†L343-L413】

## Diagnostics

* `/api/v1/debug` reveals the `backlog`, `last_leak`, and `algorithm` values, making it easy to confirm the slower drip rate in
real time.【F:internal/ratelimiter/service.go†L421-L443】
* Decision logs include the strategy reason, so you can search CI or production logs for `reason="absorbing burst"` to audit the
transition.【F:internal/ratelimiter/service.go†L451-L470】
* The stats table exposes the burst score and the remaining token approximation (capacity minus backlog) so dashboards can alert
when bursts persist.【F:internal/ratelimiter/service.go†L199-L213】

## Reproducing the behaviour

1. Run the service locally (`make run`).
2. Issue a rapid burst, for example `hey -c20 -z5s -H 'Content-Type: application/json' -m POST http://localhost:8080/api/v1/allow -d '{"key":"burst","max_tokens":3,"refill_rate":1}'`.
3. Inspect the responses; once the queue fills, later responses will report `"algorithm":"leaky_bucket"` and `"strategy_reason":"absorbing burst"`.
4. The dedicated unit test `TestHybridSwitchesToLeakyBucketOnBurst` replicates this pattern deterministically with a scripted
clock.【F:internal/ratelimiter/service_test.go†L104-L146】
