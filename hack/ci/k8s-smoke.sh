#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
IMAGE=${IMAGE:-ratelimiter:ci}
NAMESPACE=${NAMESPACE:-rate-limiter}

log() {
  echo "[k8s-smoke] $*"
}

log "Ensuring namespace '${NAMESPACE}' exists"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

log "Applying NATS manifest"
kubectl apply -n "${NAMESPACE}" -f "${ROOT}/deploy/kubernetes/nats.yaml"

log "Applying rate-limiter manifest"
kubectl apply -n "${NAMESPACE}" -f "${ROOT}/deploy/kubernetes/rate-limiter.yaml"

log "Setting deployment image to ${IMAGE}"
kubectl set image deployment/rate-limiter rate-limiter="${IMAGE}" -n "${NAMESPACE}"

log "Waiting for rollout to finish"
kubectl rollout status deployment/rate-limiter -n "${NAMESPACE}" --timeout=240s

log "Waiting for pods to become ready"
kubectl wait --for=condition=ready pod -l app=rate-limiter -n "${NAMESPACE}" --timeout=240s

log "Executing smoke test request"
response=$(kubectl run rate-limiter-smoke --restart=Never --rm -i \
  --image=curlimages/curl:8.7.1 -n "${NAMESPACE}" -- \
  curl -sS -X POST http://rate-limiter:8080/api/v1/allow \
    -H 'Content-Type: application/json' \
    -d '{"key":"ci-user","tokens":1,"max_tokens":5,"refill_rate":5}'
)

echo "Smoke test response: ${response}"

echo "${response}" | grep '"allowed"'
echo "${response}" | grep '"allowed":true'

log "Kubernetes smoke test completed successfully"
