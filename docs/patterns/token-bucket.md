# Token bucket strategy

The token bucket remains the default strategy because it delivers the best compromise between burst tolerance and deterministic
limits. The implementation inside `internal/ratelimiter/service.go` maintains a token balance per logical key/source pair, adds
tokens at the configured refill rate, and consumes tokens when requests arrive.【F:internal/ratelimiter/service.go†L84-L139】【F:internal/ratelimiter/service.go†L302-L343】

## Activation criteria

The hybrid selector keeps the token bucket active when traffic is balanced (moderate burst score, moderate variance) and when
no other pattern is dominant. Specifically, the controller:

* Resets a new bucket to this strategy with the `balanced throughput` reason, ensuring consistent startup behaviour.
* Returns to token bucket mode when the burst score drops below the leaky-bucket threshold and the sustained score does not
demand sliding-window fairness.【F:internal/ratelimiter/service.go†L214-L257】

## State synchronisation

When token bucket is active, replicas synchronise the `Tokens`, `Capacity`, `RefillRate`, and `LastRefill` fields via NATS
messages so all nodes share the same balance. Because the bucket state is idempotent, the receiving replica simply replaces its
local snapshot when it observes a higher version number.【F:internal/ratelimiter/service.go†L343-L413】

## Diagnostics

* `/api/v1/debug` exposes the live token count, refill rate, and algorithm label so you can verify the bucket is in the expected
mode.【F:internal/ratelimiter/service.go†L421-L443】
* Decision logs include `algorithm=token_bucket` and the associated `reason="balanced throughput"` tag, making it easy to filter
for token-bucket decisions in log storage.【F:internal/ratelimiter/service.go†L451-L470】

## Reproducing the behaviour

1. `make run` and issue ten evenly spaced requests (`hey -c1 -n10 http://localhost:8080/api/v1/allow -d '{"key":"demo"}'`).
2. Observe that all responses report `"algorithm":"token_bucket"` and the `/api/v1/stats` endpoint shows a high sustain score but
below the sliding-window threshold.
3. The unit test `TestAllowEnforcesLimitAndUpdatesStats` exercises this path end to end, ensuring counters, timestamps, and
algorithm labels line up.【F:internal/ratelimiter/service_test.go†L9-L74】
