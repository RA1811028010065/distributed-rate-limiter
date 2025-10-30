# Distributed Rate Limiter Overview

This document gives a concise view of the capabilities that ship with the project so you can explain the
system to others and quickly exercise its behaviour from a clean Ubuntu 24.04 workstation.

## What the application does

The service enforces hybrid rate limits (token bucket, leaky bucket, and sliding window) across multiple instances while keeping per-source accounting data in sync via NATS. Every decision is logged with structured metadata so that operators can trace how requests were handled and which algorithm was active. The API surface exposes:

- `POST /api/v1/allow` — evaluates whether a caller identified by `key` and optional `source` may consume tokens.
- `GET /api/v1/stats` — returns live counters for each tracked key/source pair.
- `GET /healthz` — lightweight probe for Kubernetes readiness checks.

Under the hood each instance publishes bucket mutations to the message bus and applies incoming events with
version checks. This allows Kubernetes pods, Docker Compose services, or bare processes to keep a consistent
view of limits.

## Example flows

### Single request

```bash
curl -X POST http://localhost:8080/api/v1/allow \
  -H 'Content-Type: application/json' \
  -d '{"key":"demo-user","source":"1.2.3.4","tokens":1,"max_tokens":10,"refill_rate":10}'
```

The response contains the decision and the updated bucket state:

```json
{
  "allowed": true,
  "remaining_tokens": 9,
  "message": "allowed",
  "allowed_hits": 1,
  "denied_hits": 0,
  "last_allowed_at": "2025-10-30T00:00:00Z",
  "last_denied_at": "",
  "algorithm": "token_bucket",
  "strategy_reason": "balanced throughput"
}
```

### Inspect accumulated statistics

After sending traffic, query the stats endpoint to see per-source counters:

```bash
curl http://localhost:8080/api/v1/stats | jq
```

The JSON response includes the total number of allowed and denied requests, the active algorithm, burst/sustain scores, and the last activity timestamp for each `(key, source)` tuple.

### Observe structured logs

Logs use JSON lines so they are easy to ingest into tools such as Loki or Splunk. To read them locally:

```bash
tail -f logs/runtime.log | jq
```

Example entry:

```json
{
  "level": "info",
  "ts": "2024-10-30T12:34:56Z",
  "component": "ratelimiter",
  "message": "decision",
  "key": "demo-user",
  "source": "1.2.3.4",
  "allowed": true,
  "remaining": 9,
  "algorithm": "token_bucket",
  "reason": "balanced throughput"
}
```

Use these snippets as templates when demonstrating the system or integrating it into wider validation scripts.
