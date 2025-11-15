# Distributed Rate Limiter

This project showcases a self-contained distributed rate-limiter implemented in Go. It exposes both REST and gRPC-style endpoints, synchronises state via NATS or an in-memory bus, and is fully containerised for deployment on Kubernetes. For a concise overview with sample interactions, see [`docs/overview.md`](docs/overview.md); for a deep architectural explanation covering every component and technology choice, read [`docs/product.md`](docs/product.md). Algorithm-by-algorithm deep dives live under [`docs/patterns/`](docs/patterns/).

## Features

- **Hybrid rate limiting** that auto-selects between token bucket, leaky bucket, and sliding window techniques based on live traffic telemetry, keeping bursty and sustained workloads in check.【F:internal/ratelimiter/service.go†L84-L343】
- **Token bucket rate limiting** with distributed synchronisation through NATS or an in-memory fallback.
- **gRPC-style API** transported over HTTP using a lightweight server implementation compatible with protobuf-encoded messages.
- **REST API** for simple integration and observability endpoints, including live statistics and health checks.
- **Structured decision logging** that fans out to stdout and an on-disk log file for auditability.
- **NATS integration** implemented directly over the NATS protocol with an in-memory substitute for local development.
- **Docker, Helm & Kubernetes manifests** for containerised deployments.
- **Automated tests** covering protobuf codecs, hybrid strategy switching, and rate-limiter synchronisation semantics.【F:internal/ratelimiter/service_test.go†L9-L186】
- **GitHub Actions pipeline** that builds, tests, deploys to a Kind cluster, and runs a REST smoke test.

## Project layout

```
.
├── cmd/ratelimiter        # Service entrypoint (REST + gRPC servers)
├── internal/
│   ├── pbcodec            # Lightweight protobuf encoder/decoder
│   ├── ratelimiter        # Core rate-limiter implementation
│   └── server             # REST HTTP handlers
├── pkg/
│   ├── natsutil           # Minimal NATS client + in-memory bus
│   └── simplegrpc         # Lightweight unary gRPC server
├── proto/                 # Protobuf schema definition
├── deploy/
│   ├── docker-compose.yml # Local development stack with NATS
│   └── kubernetes/        # Example Deployment + Service manifests
└── README.md
```

### Component deep dive

- **`cmd/ratelimiter/main.go`** wires the HTTP router (`internal/server`) and the unary gRPC endpoint (`pkg/simplegrpc`) togethe
r, loads configuration from the `ConfigMap` / environment, initialises the distributed bus, and keeps the process alive with hea
lth logging.【F:cmd/ratelimiter/main.go†L15-L123】
- **`cmd/grpcclient/main.go`** is a minimal CLI that encodes protobuf requests via `internal/pbcodec`, posts them over HTTP (us
ing the gRPC-over-HTTP transport), and prints the decoded response so you can script load tests or smoke checks without third-pa
rty tooling.【F:cmd/grpcclient/main.go†L17-L173】
- **`internal/ratelimiter/service.go`** owns the hybrid algorithms (token bucket, leaky bucket, sliding window) plus replication
hooks so state changes are announced on the bus and reconciled when other nodes respond.【F:internal/ratelimiter/service.go†L84-L
343】
- **`pkg/natsutil`** abstracts the publish/subscribe plane; the production path speaks the NATS protocol (`NATS_URL=nats://...`),
while the fallback uses an in-memory channel fan-out for single-process development. Both options expose the same interface so t
he rest of the code stays oblivious.【F:pkg/natsutil/nats.go†L13-L120】
- **`pkg/simplegrpc`** implements just enough of unary gRPC over HTTP/2 semantics to let the service parse protobuf frames withou
t bringing in the full gRPC stack – perfect for resource-constrained demos.【F:pkg/simplegrpc/server.go†L15-L155】
- **`internal/pbcodec`** hand-rolls the protobuf marshaler/unmarshaler for the request/response messages defined in [`proto/rate_
limiter.proto`](proto/rate_limiter.proto), ensuring gRPC and REST share the same structs.【F:internal/pbcodec/codec.go†L14-L167】
- **`deploy/docker-compose.yml`** starts two containers (rate-limiter + NATS) on a dedicated bridge network so you can try distrib
uted coordination locally; override environment variables inside the YAML or via `docker compose` to experiment with parameters.
- **`deploy/kubernetes/nats.yaml`** provides a namespaced Deployment + Service for NATS, and **`deploy/kubernetes/rate-limiter.ya
ml`** contains the ConfigMap, Service, and multi-replica Deployment for the Go service. Both manifests are intentionally verbose,
showing readiness probes, environment bindings, and service ports so newcomers can map YAML to runtime behaviour.【F:deploy/kuber
netes/nats.yaml†L1-L44】【F:deploy/kubernetes/rate-limiter.yaml†L1-L48】
- **`deploy/helm/rate-limiter`** mirrors the raw manifests but exposes every knob (replicas, probes, env overrides, NATS toggles,
image tags) as chart values. Each template contains inline comments explaining what the block controls to make Helm approachable
for first-time users.【F:deploy/helm/rate-limiter/templates/deployment.yaml†L1-L93】
- **`hack/*.sh` helpers** bundle the repetitive workflows: Compose orchestration, Kind lifecycle, Helm install, and the gRPC smok
e test that temporarily launches the binary, waits for readiness, runs the protobuf client, and captures structured logs.

### How the stack fits together

1. **Request ingress** – REST callers hit `internal/server` handlers while SDKs can call `/ratelimiter.v1.RateLimiter/Allow` via
 the gRPC transport. Both paths normalise into a shared `AllowRequest` struct produced by `internal/pbcodec` so business logic i
s reused.
2. **Decision engine** – `internal/ratelimiter/service.go` inspects current bucket state (in-memory map with per-key telemetry),
 chooses the best algorithm, updates counters, and emits a structured `Decision` both to stdout and the optional audit log under
 `logs/`.
3. **Event propagation** – whenever state mutates, the service publishes delta events through `pkg/natsutil`. In clustered runs (D
ocker Compose, Kubernetes, Helm) every replica subscribes to the same topic so burst traffic handled by one pod still updates th
e others in milliseconds. When `NATS_URL` is omitted the in-memory bus keeps behaviour identical for single-node development.
4. **Persistence and observability** – runtime stats surface via `/api/v1/stats` and `/api/v1/debug` (rendered by `internal/serve
r/handlers.go`), letting you watch tokens drain/refill in real time regardless of transport.
5. **Container orchestration** – Docker images created by `make docker-build` embed the statically linked Go binary. Docker Compo
se, Kubernetes YAML, and the Helm chart all mount the same image and inject configuration through environment variables and `Conf
igMap` keys, meaning the exact same binary powers local CLIs, containers, and pods.

Because everything – from protobuf definitions and transport adapters to deployment YAML – lives in this repository, you can fol
low the data flow end-to-end without switching contexts.

## Continuous integration pipeline

The repository ships with a GitHub Actions workflow located at [`.github/workflows/ci.yml`](.github/workflows/ci.yml). It runs formatting, `go vet`, unit tests, and provisions a temporary [Kind](https://kind.sigs.k8s.io/) cluster to validate the Kubernetes manifests. During the smoke test the container image is built, loaded into the cluster, deployed alongside NATS, and a REST request is executed against the service to ensure end-to-end functionality. The smoke harness now waits for each deployment to report readiness, confirms the `rate-limiter` service has active endpoints before issuing traffic, retries the verification request with bounded timeouts, and emits cluster diagnostics automatically on failure so problems can be triaged quickly both in CI and on local machines.

## Getting started

### Prerequisites

- Go 1.21+
- Docker Engine **with either the Compose plugin (`docker compose`) or the standalone `docker-compose` binary**
- kubectl & access to a Kubernetes cluster (Kind works well for local tests)
- `jq` for formatting JSON output (optional)

For platform-specific tooling installation commands refer to [`docs/tooling.md`](docs/tooling.md).

#### Ubuntu 24.04 quickstart

From a pristine Ubuntu 24.04 install you can bootstrap everything with:

```bash
sudo apt update
sudo apt install -y curl git jq make docker-compose-plugin
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"
curl -LO https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
rm go1.21.5.linux-amd64.tar.gz
curl -Lo kubectl https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl
chmod +x kubectl && sudo mv kubectl /usr/local/bin/
curl -Lo kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
chmod +x kind && sudo mv kind /usr/local/bin/
newgrp docker  # refresh group membership without logging out
```

The `docker-compose-plugin` package enables the `docker compose` sub-command. If you prefer the standalone binary you can
install it afterwards via `sudo apt install docker-compose`; the Makefile targets detect either flavour automatically.

Verify the tooling:

```bash
go version
docker version
docker compose version || docker-compose --version
kubectl version --client
kind version
```

### Reproducible command log

Every change set now comes with a verbatim CLI transcript so you can see exactly which commands passed locally before any pull
request was opened. The latest log lives at [`docs/command-log.md`](docs/command-log.md) and is updated alongside the code whene
ver a new workflow (tests, smoke helpers, etc.) is exercised. Each entry documents the purpose of the command, the invocation, a
nd the raw stdout/stderr for easy reproduction.

The gRPC-style endpoint is available on `localhost:8081` at the method path `/ratelimiter.v1.RateLimiter/Allow` and expects protobuf framed payloads as described in `proto/rate_limiter.proto`.

To exercise the protobuf/gRPC stack without a third-party client install the bundled helper:

```bash
make grpc-smoke          # defaults to http://localhost:8081

# or customise the payload
go run ./cmd/grpcclient -addr http://localhost:8081 \
  -key demo -tokens 1 -max-tokens 5 -refill-rate 5 -source cli
```

The utility composes a protobuf payload with `internal/pbcodec`, wraps it in the gRPC wire framing that `pkg/simplegrpc` expects, and prints the decoded response so you can verify both codecs end-to-end without needing `grpcurl`.

### Running with Docker Compose

Every change set now comes with a verbatim CLI transcript so you can see exactly which commands passed locally before any pull
request was opened. The latest log lives at [`docs/command-log.md`](docs/command-log.md) and is updated alongside the code whene
ver a new workflow (tests, smoke helpers, etc.) is exercised. Each entry documents the purpose of the command, the invocation, a
nd the raw stdout/stderr for easy reproduction.

With tooling ready, clone the repository and follow the flows in [the manual testing guide](test%20file) to exercise the service locally, via Docker Compose, and on Kubernetes.

### Running locally (start → test → cleanup)

1. **Run tests**
   ```bash
   go test -v ./...
   ```
2. **Start the service** (only necessary if you want a long-lived instance):
   ```bash
   # In-memory bus by default – set NATS_URL to talk to a real broker
   go run ./cmd/ratelimiter
   ```
   Runtime decisions land in `logs/runtime.log`, so you can keep another terminal tailing it while you run traffic.
3. **Positive REST request** – the first hit should be allowed:
   ```bash
   curl -sS -X POST http://localhost:8080/api/v1/allow \
     -H "Content-Type: application/json" \
     -d '{"key":"demo","tokens":1,"max_tokens":10,"refill_rate":5}' | jq
   ```
4. **Negative REST request** – intentionally exceed the allowance:
   ```bash
   curl -sS -X POST http://localhost:8080/api/v1/allow \
     -H "Content-Type: application/json" \
     -d '{"key":"demo","tokens":25,"max_tokens":10,"refill_rate":5}' | jq
   ```
   Expect `allowed: false` once the bucket depletes.
5. **Inspect statistics and bucket state**
   ```bash
   curl -sS http://localhost:8080/api/v1/stats | jq
   curl -sS http://localhost:8080/api/v1/debug | jq
   ```
   Both endpoints update in real time and reflect the per-key counters plus the active hybrid strategy.
6. **Exercise the gRPC/protobuf path**
   ```bash
   make grpc-smoke
   ```
   The smoke helper now boots a temporary rate-limiter instance bound to `HTTP :38080` / `gRPC :38081`, waits for it to accept traffic, runs the Go client against `http://127.0.0.1:38081/ratelimiter.v1.RateLimiter/Allow`, prints the decoded protobuf response, and tears everything down automatically. You can still target an already-running service by overriding `GRPC_CLIENT_ADDR` and skipping the ephemeral server:
   ```bash
   GRPC_CLIENT_ADDR=http://localhost:8081 \
   GRPC_SMOKE_SKIP_SERVER=1 \
   make grpc-smoke
   ```
   Additional knobs include:
   - `GRPC_SMOKE_HTTP_ADDR` / `GRPC_SMOKE_GRPC_ADDR` – customise the temporary server's bind addresses (defaults `:38080` / `:38081`).
   - `GRPC_SMOKE_CLIENT_ADDR` – URL the helper should hit when it spawns its own server (defaults `http://127.0.0.1:38081`).
   - `GRPC_SMOKE_KEY`, `GRPC_SMOKE_TOKENS`, `GRPC_SMOKE_MAX_TOKENS`, `GRPC_SMOKE_REFILL_RATE`, `GRPC_SMOKE_SOURCE` – tweak the payload without editing the code.
7. **Clean up** – stop the long-running Go process with `Ctrl+C` when you no longer need it; the smoke helper already cleans up the temporary instance for you.

The REST API stays on `http://localhost:8080` by default with `/healthz`, `/api/v1/allow`, `/api/v1/stats`, and `/api/v1/debug` endpoints, and the gRPC-style method lives at `/ratelimiter.v1.RateLimiter/Allow` over port `8081` when you run the full service manually.

### Running with Docker Compose (start → test → cleanup)

1. **Start the stack** – rate-limiter + NATS in detached mode:
   ```bash
   make compose-up
   ```
   The wrapper under `hack/compose-up.sh` prefers BuildKit for faster rebuilds, automatically retries without it if Docker reports the `unsupported shim version (3)` issue, and provisions a dedicated `ratelimiter_net` bridge on `172.31.255.0/28` so your SSH routes stay intact. Override `COMPOSE_DISABLE_BUILDKIT=1` or `RATE_LIMITER_NETWORK=/RATE_LIMITER_SUBNET` as needed.
2. **Watch logs (optional)**
   ```bash
   make compose-logs
   ```
3. **Positive REST case**
   ```bash
   curl -sS -X POST http://localhost:8080/api/v1/allow \
     -H "Content-Type: application/json" \
     -d '{"key":"compose","tokens":1,"max_tokens":5,"refill_rate":2}' | jq
   ```
4. **Negative REST case** – loop until the allowance is exhausted:
   ```bash
   for i in $(seq 1 6); do
     curl -sS -X POST http://localhost:8080/api/v1/allow \
       -H "Content-Type: application/json" \
       -d '{"key":"compose","tokens":1,"max_tokens":5,"refill_rate":1}' | jq
   done
   ```
5. **Inspect stats or container logs**
   ```bash
   curl -sS http://localhost:8080/api/v1/stats | jq
   (docker compose logs rate-limiter 2>/dev/null || docker-compose logs rate-limiter) | tail -n 20
   ```
6. **Clean up**
   ```bash
   make compose-down
   ```
   `hack/wait-compose.sh` automatically reports health status while the stack starts, and the teardown target removes containers, the bridge network, and the shared volume so repeated runs stay deterministic.

### Building a container

```
make docker-build
```

### Deploying to Kubernetes

First build and push an image that your cluster can access (or load it into a local cluster such as Kind). If you are using Kind and do not yet have a cluster running, create one first with the hardened wrapper that provisions an isolated Docker network and uses the repo's subnet-safe cluster configuration. This prevents Kind from creating the default `172.18.0.0/16` bridge that can temporarily hijack routes (and therefore SSH sessions) on remote machines:

```bash
make kind-up
```

The wrapper is a thin shim over:

```bash
KIND_EXPERIMENTAL_DOCKER_NETWORK=ratelimiter-kind \
  hack/kind-up.sh --wait 120s
```

It creates (or reuses) the `ratelimiter-kind` Docker network on the `10.240.0.0/16` subnet, then invokes `kind create cluster --name rate-limiter --config deploy/kind/cluster.yaml` so both the container and Kubernetes-level subnets avoid conflicts with common datacentre ranges.

Then build the image:

```bash
make docker-build IMAGE=ghcr.io/your-user/rate-limiter:latest
```

When targeting the bundled Kind cluster (or any other local Kind instance) load the freshly built image into the nodes before applying manifests so Kubernetes does not try to pull it from a public registry:

```bash
make kind-load IMAGE=rate-limiter:local KIND_CLUSTER=rate-limiter
```

If you are publishing the image to a remote registry, reuse that fully qualified reference when calling `make kind-load` or skip the load step entirely if your cluster can reach the registry directly.

Apply the manifests under `deploy/kubernetes` (update the container image reference in `rate-limiter.yaml` to match your registry):

```bash
kubectl apply -f deploy/kubernetes/nats.yaml
kubectl apply -f deploy/kubernetes/rate-limiter.yaml
```

Wait for the pods to become ready and inspect their status:

```bash
kubectl get pods -l app=rate-limiter
kubectl rollout status deployment/rate-limiter
```

To send a REST request against the cluster from your workstation, port-forward the HTTP service and execute a `curl` request:

```bash
kubectl port-forward service/rate-limiter 8080:8080 &
PORT_FORWARD_PID=$!
sleep 2
curl -sS -X POST http://localhost:8080/api/v1/allow \
  -H "Content-Type: application/json" \
  -d '{"key":"demo","tokens":1,"max_tokens":10,"refill_rate":5}' | jq
kill $PORT_FORWARD_PID
```

For automated verification you can rely on the provided smoke test script (which is what the CI pipeline runs):

```bash
make kube-smoke IMAGE=rate-limiter:local KUBE_NAMESPACE=ratelimiter-demo
```

The deployment exposes port `8080` for REST and `8081` for the gRPC endpoint. Update the `ConfigMap` inside the manifest to customise limits or environment variables.

### Deploying with Helm

If you prefer Helm to manage releases, the chart in `deploy/helm/rate-limiter` packages the ConfigMap, Deployment, Service, optional NATS dependency, and a smoke-test pod. The defaults mirror the raw manifests above, so you only need to override the container image and namespace:

```bash
IMAGE=ghcr.io/your-user/rate-limiter:latest make helm-install KUBE_NAMESPACE=ratelimiter-demo HELM_RELEASE=ratelimiter-demo

# verify and remove when finished
helm test ratelimiter-demo -n ratelimiter-demo
make helm-uninstall KUBE_NAMESPACE=ratelimiter-demo HELM_RELEASE=ratelimiter-demo
```

Set `nats.enabled=false` if your cluster already runs a shared NATS instance and pass additional env pairs through `values.yaml` or `--set-json env='[{"name":"LOG_PATH","value":"/data/runtime.log"}]'` to tweak runtime behaviour.

## Configuration

Key environment variables recognised by the service:

- `HTTP_ADDR` – HTTP bind address (default `:8080`).
- `GRPC_ADDR` – gRPC bind address (default `:8081`).
- `NATS_URL` – NATS connection string (e.g. `nats://nats:4222`). When unset the in-memory bus is used.

## Protobuf contract

The protobuf schema is defined in [`proto/rate_limiter.proto`](proto/rate_limiter.proto). Because this repository runs in a constrained environment, a lightweight codec in [`internal/pbcodec`](internal/pbcodec) manually encodes and decodes the request/response messages without external dependencies. The gRPC transport is implemented in [`pkg/simplegrpc`](pkg/simplegrpc) and understands protobuf-framed unary requests.

## Testing

```
go test -v ./...
```

The tests validate the protobuf codec round-trip logic, ensure the distributed statistics table stays in sync, and verify that the hybrid controller selects the best strategy for bursty and sustained workloads while multiple rate-limiter instances remain co-ordinated through the event bus.【F:internal/ratelimiter/service_test.go†L9-L186】

Detailed manual validation flows for local binaries, Docker Compose, and Kubernetes are documented in [`test file`](test%20file). Each flow now includes example outputs captured in the [`logs/`](logs) directory so you can compare your run against a known-good baseline.

## Extending

- Swap the in-memory bus with a real NATS connection by setting `NATS_URL`.
- Add more RPC methods by extending the protobuf schema and wiring handlers into the gRPC server.
- Layer a caching or persistence backend by augmenting the `bucket` management in `internal/ratelimiter`.

## License

MIT
