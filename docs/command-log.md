# Command Verification Log

_Last updated: 2025-11-15 04:48:12 UTC._

Each entry captures the exact command that was executed in this repository, what it proves, and the raw terminal output so you can reproduce the same workflow locally.

## 1. `go test ./...`

**Purpose:** Compile every package and run the unit tests.

**Command & output:**
```bash
$ go test ./...
?       github.com/example/distributed-rate-limiter/cmd/grpcclient      [no test files]
?       github.com/example/distributed-rate-limiter/cmd/ratelimiter     [no test files]
ok      github.com/example/distributed-rate-limiter/internal/pbcodec    (cached)
ok      github.com/example/distributed-rate-limiter/internal/ratelimiter        (cached)
?       github.com/example/distributed-rate-limiter/internal/server     [no test files]
?       github.com/example/distributed-rate-limiter/pkg/natsutil        [no test files]
?       github.com/example/distributed-rate-limiter/pkg/simplegrpc      [no test files]
```

## 2. `make grpc-smoke`

**Purpose:** Spin up a temporary rate-limiter instance, run the bundled gRPC client against it, and clean everything up.

**Command & output:**
```bash
$ make grpc-smoke
./hack/grpc-smoke.sh
[grpc-smoke] starting temporary rate-limiter (HTTP :38080, gRPC :38081)
[grpc-smoke] parsed temporary endpoint 127.0.0.1:38081
[grpc-smoke] waiting for 127.0.0.1:38081
+ go run ./cmd/grpcclient -addr http://127.0.0.1:38081 -key grpc-smoke -tokens 1 -max-tokens 5 -refill-rate 5 -source cli
{
  "allowed": true,
  "remaining_tokens": 4,
  "message": "allowed",
  "allowed_hits": 1,
  "denied_hits": 0,
  "last_allowed_at": "2025-11-15T04:47:56.227725083Z",
  "last_denied_at": "",
  "algorithm": "token_bucket",
  "strategy_reason": "balanced throughput"
}
+ set +x
[grpc-smoke] success
```
