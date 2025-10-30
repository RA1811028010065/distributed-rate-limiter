# Distributed Rate Limiter Product Guide

This guide explains every moving part in the distributed rate limiter, how they interact end to end, and why each technology was selected. It is written so that an engineer encountering the repository for the first time can understand the full product story without reading any code.

## 1. High-level journey

1. A caller submits a request to the REST endpoint (`POST /api/v1/allow`) or to the gRPC-style endpoint (`/ratelimiter.v1.RateLimiter/Allow`).
2. The API layer validates the payload, annotates the source metadata, and passes the request to the core rate limiter.
3. The rate limiter enforces a hybrid policy (token bucket, leaky bucket, or sliding window), records detailed statistics, and publishes state changes.
4. State updates fan out over the messaging bus (NATS in production, in-memory during tests). Each replica keeps its local cache consistent by applying those events.
5. The service responds to the caller and writes structured decision logs for observability.
6. Automated smoke tests and GitHub Actions verify the full journey by building an image, deploying to a Kind cluster, and executing real traffic against the Kubernetes Service.

The remaining sections go deep on each component involved in the journey.

## 2. Application entrypoint (`cmd/ratelimiter`)

* **Responsibility:** Bootstrap configuration, connect to NATS when enabled, instantiate the rate limiter, and expose both HTTP and gRPC-style servers.
* **Key behaviours:**
  - Loads environment variables/flags, including optional file logging path and NATS URL.
  - Builds the `natsutil` client if a URL is provided; otherwise falls back to the in-memory bus implementation for single-node runs.
  - Wires the rate limiter into the HTTP handlers and starts listeners on ports 8080 (REST) and 8081 (protobuf/gRPC style).
* **Why Go?** Go’s standard library covers HTTP, concurrency, and logging with minimal dependencies, keeping the entrypoint compact. Its binary distribution model is ideal for container shipping.

See `cmd/ratelimiter/main.go` for the full setup logic.【F:cmd/ratelimiter/main.go†L1-L236】

## 3. API surface (`internal/server`)

* **REST handlers:** Implemented in `internal/server/http.go`, they translate JSON requests into protobuf-compatible structs, populate the caller IP when missing, and format human-friendly JSON responses.【F:internal/server/http.go†L23-L205】
* **gRPC-style transport:** Uses the lightweight server in `pkg/simplegrpc` to accept HTTP/2 requests carrying length-prefixed protobuf payloads. This avoids pulling the full gRPC stack while keeping compatibility with common tooling.【F:pkg/simplegrpc/simplegrpc.go†L1-L180】
* **Health & diagnostics:** Additional endpoints (`/healthz`, `/api/v1/stats`, `/api/v1/debug`) expose the statistics table and internal bucket state so operators can inspect behaviour without diving into logs.【F:internal/server/http.go†L95-L205】
* **Why dual interface?** REST simplifies manual testing and curl usage, while the protobuf framing keeps the service interoperable with strongly typed clients and other microservices. The lightweight `simplegrpc` layer provides just enough of gRPC for this single-method service without the complexity of generated stubs.

## 4. Core rate limiter (`internal/ratelimiter`)

* **Hybrid controller:** `updateFlowMetrics` maintains exponential moving averages, variance, and burst/sustain scores for every key/source pair. `applyStrategyLocked` evaluates those metrics, enforces a cooldown, and selects the best strategy (token bucket, leaky bucket, or sliding window) while resetting algorithm-specific state.【F:internal/ratelimiter/service.go†L140-L257】【F:internal/ratelimiter/service.go†L258-L343】
* **Token bucket baseline:** When no pattern dominates, the limiter falls back to a classic token bucket, refilling tokens over time and keeping burst tolerance high.【F:internal/ratelimiter/service.go†L84-L139】【F:internal/ratelimiter/service.go†L288-L306】
* **Leaky bucket smoothing:** Bursty traffic activates backlog tracking and drip-rate enforcement so replicas throttle uniformly based on the shared backlog state.【F:internal/ratelimiter/service.go†L258-L317】
* **Sliding window fairness:** Sustained traffic switches to a sliding-window counter that guarantees a hard cap per interval and replicates window metadata across nodes.【F:internal/ratelimiter/service.go†L317-L343】
* **Distributed synchronisation:** Each state transition is published as a `syncEvent`. Peers subscribe to the same subject and reconcile using version numbers to avoid stale writes.【F:internal/ratelimiter/service.go†L343-L413】
* **Statistics table:** The service records per-key and per-source counters, last request timestamps, algorithm labels, and the hybrid metrics. This data powers the `/stats` and `/debug` endpoints and feeds into decision logs.【F:internal/ratelimiter/service.go†L18-L75】【F:internal/ratelimiter/service.go†L199-L213】【F:internal/ratelimiter/service.go†L418-L443】
* **Structured logging:** Every decision emits JSON logs detailing the request, outcome, algorithm, and updated counters, aiding auditing and anomaly detection.【F:internal/ratelimiter/service.go†L444-L470】
* **Why custom implementation?** Owning the bucket, stats, and sync logic keeps the learning surface clear for this sample application and avoids introducing heavier dependencies like Redis. It also enables deterministic unit tests and precise documentation of the algorithm.

## 5. Protobuf layer (`internal/pbcodec` and `proto/rate_limiter.proto`)

* **Schema:** `proto/rate_limiter.proto` defines the `AllowRequest`/`AllowResponse` messages with metadata for key, source, stats counters, and the active algorithm plus strategy reason so clients can observe hybrid decisions.【F:proto/rate_limiter.proto†L1-L93】
* **Codec:** `internal/pbcodec` provides a tiny protobuf encoder/decoder tailored to these messages. The unit tests confirm round-trip integrity and compatibility with the manually constructed frames.【F:internal/pbcodec/codec.go†L1-L318】【F:internal/pbcodec/codec_test.go†L1-L126】
* **Why hand-written?** The repository intentionally demonstrates how protobuf wire types work without relying on `protoc`. This keeps the toolchain lightweight for readers and gives full control over encoded payloads.

## 6. Messaging bus (`pkg/natsutil`)

* **NATS client:** The project ships a minimal TCP client that speaks the core NATS protocol (CONNECT, SUB, PUB, PING/PONG). It is sufficient for publishing small synchronisation messages while keeping dependencies minimal.【F:pkg/natsutil/client.go†L1-L222】
* **In-memory bus:** For local development and unit tests the `Bus` interface has an in-memory implementation so that the service can run without an external broker.【F:pkg/natsutil/bus.go†L1-L182】
* **Why NATS?** NATS offers lightweight pub/sub semantics with at-most-once delivery, perfect for fanning out state updates. It is widely available as a container, has a simple text protocol, and integrates cleanly with Kubernetes via a single deployment. Other brokers (Kafka, RabbitMQ) would be heavier for this use case.

## 7. Deployment assets (`deploy/`)

* **Docker Compose (`deploy/docker-compose.yml`):** Spins up a NATS container and the rate limiter, mirroring the production topology on a single machine. The Makefile target performs a build step first so both the Compose plugin and legacy binary work on Ubuntu 24.04.【F:deploy/docker-compose.yml†L1-L88】【F:Makefile†L1-L126】
* **Kubernetes manifests (`deploy/kubernetes/*`):** Provide Deployments and Services for both NATS and the application. They include readiness probes, resource requests, and labels consumed by the smoke harness.【F:deploy/kubernetes/nats.yaml†L1-L102】【F:deploy/kubernetes/rate-limiter.yaml†L1-L162】
* **Why Kubernetes + Kind?** Kubernetes is the target deployment platform, and Kind (Kubernetes in Docker) offers a reproducible test environment that integrates with CI runners. This allows end-to-end verification without requiring cloud infrastructure.

## 8. Observability assets (`logs/`)

Sample log transcripts live under `logs/` so operators can compare their runtime output with known-good sessions (local binary, Compose stack, CI smoke). These demonstrate the structured JSON format and the kinds of messages emitted during steady state and during rate limiting.【F:logs/runtime.log†L1-L2】【F:logs/ci-smoke.log†L1-L22】

## 9. Documentation set (`docs/` and `test file`)

* **`docs/overview.md`:** A concise story with example requests and responses for quick orientation.【F:docs/overview.md†L1-L76】
* **`docs/tooling.md`:** Install guides for Go, Docker, NATS CLI, jq, and more across multiple platforms, ensuring operators know the prerequisites.【F:docs/tooling.md†L1-L95】
* **`docs/patterns/`:** Detailed pages for each rate-limiting strategy (token bucket, leaky bucket, sliding window) plus the hybrid controller decision logic.【F:docs/patterns/README.md†L1-L12】【F:docs/patterns/hybrid-controller.md†L1-L33】
* **`test file`:** A step-by-step manual testing harness covering local runs, Docker Compose, Kubernetes, and cleanup workflows.【F:test file†L1-L155】
* **`docs/product.md` (this guide):** The full architectural deep dive.

## 10. Continuous integration (`.github/workflows/ci.yml` + `hack/ci/k8s-smoke.sh`)

* **Workflow stages:** Lint (`gofmt` check), `go vet`, unit tests, Docker build, Kind cluster provisioning, manifest apply, smoke test, and teardown.【F:.github/workflows/ci.yml†L1-L235】
* **Smoke harness:** Waits for deployments to roll out, ensures the Service has endpoints, issues REST requests with retries, and gathers diagnostics on failure. This script mirrors the manual smoke steps and guarantees the environment is healthy before traffic is sent.【F:hack/ci/k8s-smoke.sh†L1-L233】
* **Why GitHub Actions?** It offers integrated runners capable of running Docker + Kind, aligning with the open-source tooling stack and providing transparent logs for contributors.

## 11. How the pieces fit together

| Layer | Components | Purpose | Reason for choice |
| ----- | ---------- | ------- | ----------------- |
| Interface | REST handlers, simple gRPC, pbcodec | Accept traffic from curl or protobuf clients | REST for simplicity, protobuf for typed integrations |
| Core logic | Rate limiter service, stats table | Enforce quotas, track per-source analytics | Custom logic keeps example self-contained |
| Messaging | NATS client, in-memory bus | Synchronise state across replicas | NATS provides lightweight pub/sub |
| Persistence | In-memory buckets/stats | Fast, transient storage; no external DB required | Rate limiting data is short-lived and replicated |
| Observability | Structured logs, `/stats` & `/debug` endpoints, sample logs | Operational insight and debugging | JSON logs integrate with modern log stacks |
| Packaging | Dockerfile, Makefile | Reproducible builds, consistent developer commands | Docker is ubiquitous for shipping Go services |
| Orchestration | Kubernetes manifests, Kind smoke | Scalable deployment and automated verification | Matches production-style environments |
| Automation | GitHub Actions workflow, smoke script | Catch regressions automatically | Ensures local and CI parity |

## 12. Extending or customising the product

* **Swap transport:** Replace `simplegrpc` with the official gRPC stack if you need streaming or interceptors. The core rate limiter API is already defined in protobuf.
* **Persist stats:** Introduce a database or external cache by extending `statsEntry` updates; the current design centralises mutation in one place for easy adaptation.
* **Alternate messaging:** Implement the `natsutil.Bus` interface for brokers like Redis Streams if your infrastructure standardises on another message bus.
* **Security:** Add auth middleware at the HTTP layer; the handlers already centralise request parsing, so injecting validation there is straightforward.

## 13. Recap

The distributed rate limiter combines Go services, protobuf messaging, a NATS-backed event bus, and Kubernetes automation to deliver consistent quota enforcement across replicas. Each technology was chosen to keep the example approachable while still reflecting production-grade patterns: lightweight protocols, structured logs, containerised deployment, and automated smoke coverage. With this guide and the accompanying tooling instructions, even a newcomer can bring the stack online, observe its behaviour, and iterate confidently.
