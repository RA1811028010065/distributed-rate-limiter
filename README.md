# Distributed Rate Limiter

This project showcases a self-contained distributed rate-limiter implemented in Go. It exposes both REST and gRPC-style endpoints, synchronises state via NATS or an in-memory bus, and is fully containerised for deployment on Kubernetes. For a concise overview with sample interactions, see [`docs/overview.md`](docs/overview.md); for a deep architectural explanation covering every component and technology choice, read [`docs/product.md`](docs/product.md). Algorithm-by-algorithm deep dives live under [`docs/patterns/`](docs/patterns/).

## Features

- **Hybrid rate limiting** that auto-selects between token bucket, leaky bucket, and sliding window techniques based on live traffic telemetry, keeping bursty and sustained workloads in check.【F:internal/ratelimiter/service.go†L84-L343】
- **Token bucket rate limiting** with distributed synchronisation through NATS or an in-memory fallback.
- **gRPC-style API** transported over HTTP using a lightweight server implementation compatible with protobuf-encoded messages.
- **REST API** for simple integration and observability endpoints, including live statistics and health checks.
- **Structured decision logging** that fans out to stdout and an on-disk log file for auditability.
- **NATS integration** implemented directly over the NATS protocol with an in-memory substitute for local development.
- **Docker & Kubernetes manifests** for containerised deployments.
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

With tooling ready, clone the repository and follow the flows in [the manual testing guide](test%20file) to exercise the service locally, via Docker Compose, and on Kubernetes.

### Running locally

```bash
# Run automated tests
go test -v ./...

# Start the service with the in-memory event bus
go run ./cmd/ratelimiter
```

The REST API defaults to `http://localhost:8080`:

- `POST /api/v1/allow` – body: `{ "key": "user-1", "tokens": 1, "max_tokens": 10, "refill_rate": 5 }`. The server automatically annotates the caller's source IP when omitted.
- `GET /api/v1/stats` – returns the distributed statistics table (per key and per source).
- `GET /api/v1/debug` – returns the bucket state plus the same statistics snapshot for deeper diagnostics.
- `GET /healthz` – liveness endpoint.

The gRPC-style endpoint is available on `localhost:8081` at the method path `/ratelimiter.v1.RateLimiter/Allow` and expects protobuf framed payloads as described in `proto/rate_limiter.proto`.

### Running with Docker Compose

```
make compose-up
```

This launches a NATS server and the rate-limiter service (which will synchronise buckets using the shared NATS instance) in detached mode, so your shell remains usable during the run. The Makefile now shells out to `hack/compose-up.sh`, a wrapper that prefers BuildKit for faster rebuilds but automatically retries without it if Docker reports the `unsupported shim version (3)` error seen on older containerd builds. Set `COMPOSE_DISABLE_BUILDKIT=1 make compose-up` to permanently pin the stack to the classic builder in restrictive environments. Once the containers start, `hack/wait-compose.sh` polls the health checks defined in `deploy/docker-compose.yml`, streaming updates until everything is healthy. Tail the stack output any time with `make compose-logs` and stop it with `make compose-down`.

To prevent Docker's default bridge network from hijacking existing routes (which could momentarily sever SSH connectivity on remote hosts), the Compose file now provisions a dedicated bridge network named `ratelimiter_net` with the default subnet `172.31.255.0/28`. Override `RATE_LIMITER_NETWORK` and/or `RATE_LIMITER_SUBNET` when invoking `make compose-up` if that range clashes with your infrastructure.

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
