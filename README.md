# Distributed Rate Limiter

This project showcases a self-contained distributed rate-limiter implemented in Go. It exposes both REST and gRPC-style endpoints, synchronises state via NATS or an in-memory bus, and is fully containerised for deployment on Kubernetes.

## Features

- **Token bucket rate limiting** with distributed synchronisation through NATS or an in-memory fallback.
- **gRPC-style API** transported over HTTP using a lightweight server implementation compatible with protobuf-encoded messages.
- **REST API** for simple integration and observability endpoints.
- **NATS integration** implemented directly over the NATS protocol with an in-memory substitute for local development.
- **Docker & Kubernetes manifests** for containerised deployments.
- **Automated tests** covering protobuf codecs and rate-limiter synchronisation semantics.

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

## Getting started

### Prerequisites

- Go 1.21+
- Docker (for container builds)
- kubectl & Kubernetes cluster (for deployment)

### Running locally

```bash
# Run automated tests
go test ./...

# Start the service with the in-memory event bus
go run ./cmd/ratelimiter
```

The REST API defaults to `http://localhost:8080`:

- `POST /api/v1/allow` – body: `{ "key": "user-1", "tokens": 1, "max_tokens": 10, "refill_rate": 5 }`
- `GET /api/v1/debug` – returns the in-memory bucket state.

The gRPC-style endpoint is available on `localhost:8081` at the method path `/ratelimiter.v1.RateLimiter/Allow` and expects protobuf framed payloads as described in `proto/rate_limiter.proto`.

### Running with Docker Compose

```
make compose-up
```

This launches a NATS server and the rate-limiter service (which will synchronise buckets using the shared NATS instance). Stop the stack with `make compose-down`.

### Building a container

```
make docker-build
```

### Deploying to Kubernetes

Apply the manifests under `deploy/kubernetes` (update the container image reference in `rate-limiter.yaml` to match your registry):

```
kubectl apply -f deploy/kubernetes/nats.yaml
kubectl apply -f deploy/kubernetes/rate-limiter.yaml
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
go test ./...
```

The tests validate the protobuf codec round-trip logic and verify that multiple rate-limiter instances stay synchronised through the event bus.

## Extending

- Swap the in-memory bus with a real NATS connection by setting `NATS_URL`.
- Add more RPC methods by extending the protobuf schema and wiring handlers into the gRPC server.
- Layer a caching or persistence backend by augmenting the `bucket` management in `internal/ratelimiter`.

## License

MIT
