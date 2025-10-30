#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
IMAGE=${IMAGE:-ratelimiter:ci}
NAMESPACE=${NAMESPACE:-rate-limiter}

log() {
  echo "[k8s-smoke] $*"
}

dump_diagnostics() {
  local rc=$1
  log "collecting diagnostics because the smoke test failed with exit code ${rc}"
  kubectl get all -n "${NAMESPACE}" || true
  for pod in $(kubectl get pods -n "${NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
    log "--- logs for pod ${pod} ---"
    kubectl logs "${pod}" -n "${NAMESPACE}" --tail=200 || true
  done
}

cleanup() {
  local rc=$?
  if [[ ${rc} -ne 0 ]]; then
    dump_diagnostics "${rc}"
  fi
}

trap cleanup EXIT

wait_for_ready_pods() {
  local timeout=$1
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    local desired
    desired=$(kubectl get deployment/rate-limiter -n "${NAMESPACE}" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)
    if [[ -z "${desired}" ]]; then
      sleep 5
      continue
    fi

    local ready
    ready=$(kubectl get deployment/rate-limiter -n "${NAMESPACE}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)
    if [[ -z "${ready}" ]]; then
      ready=0
    fi

    if [[ "${ready}" -eq "${desired}" ]]; then
      log "${ready}/${desired} rate-limiter pods ready"
      return 0
    fi

    sleep 5
  done

  log "Timed out waiting for rate-limiter pods to become ready"
  kubectl get pods -l app=rate-limiter -n "${NAMESPACE}" || true
  return 1
}

wait_for_service_endpoints() {
  local svc=$1
  local timeout=$2
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    local endpoints
    endpoints=$(kubectl get endpoints "${svc}" -n "${NAMESPACE}" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true)
    if [[ -n "${endpoints}" ]]; then
      log "Service '${svc}' has active endpoints"
      return 0
    fi
    sleep 5
  done
  log "Timed out waiting for service '${svc}' endpoints"
  kubectl get endpoints "${svc}" -n "${NAMESPACE}" || true
  return 1
}

log "Ensuring namespace '${NAMESPACE}' exists"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

log "Applying NATS manifest"
kubectl apply -n "${NAMESPACE}" -f "${ROOT}/deploy/kubernetes/nats.yaml"

log "Waiting for NATS deployment rollout"
kubectl rollout status deployment/nats -n "${NAMESPACE}" --timeout=120s
kubectl wait --for=condition=ready pod -l app=nats -n "${NAMESPACE}" --timeout=120s

log "Applying rate-limiter manifest"
kubectl apply -n "${NAMESPACE}" -f "${ROOT}/deploy/kubernetes/rate-limiter.yaml"

log "Setting deployment image to ${IMAGE}"
kubectl set image deployment/rate-limiter rate-limiter="${IMAGE}" -n "${NAMESPACE}"

log "Waiting for rate-limiter rollout"
kubectl rollout status deployment/rate-limiter -n "${NAMESPACE}" --timeout=240s
wait_for_ready_pods 240
wait_for_service_endpoints rate-limiter 120

log "Executing smoke test request"
response=$(kubectl run rate-limiter-smoke --restart=Never --rm -i \
  --image=curlimages/curl:8.7.1 -n "${NAMESPACE}" -- \
  curl -fsS --max-time 10 --retry 5 --retry-all-errors \
    -X POST http://rate-limiter:8080/api/v1/allow \
    -H 'Content-Type: application/json' \
    -d '{"key":"ci-user","tokens":1,"max_tokens":5,"refill_rate":5,"source":"ci"}')

echo "Smoke test response: ${response}"

echo "${response}" | grep '"allowed"'
echo "${response}" | grep '"allowed":true'

log "Kubernetes smoke test completed successfully"
