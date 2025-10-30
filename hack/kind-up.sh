#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
NETWORK_NAME=${KIND_NETWORK_NAME:-ratelimiter-kind}
NETWORK_SUBNET=${KIND_NETWORK_SUBNET:-10.240.0.0/16}
CLUSTER_NAME=${KIND_CLUSTER_NAME:-rate-limiter}
KIND_CONFIG=${KIND_CONFIG_PATH:-${ROOT}/deploy/kind/cluster.yaml}

create_network() {
  if docker network inspect "${NETWORK_NAME}" >/dev/null 2>&1; then
    echo "kind network '${NETWORK_NAME}' already exists"
    return
  fi

  echo "creating docker network '${NETWORK_NAME}' (${NETWORK_SUBNET}) for kind"
  docker network create "${NETWORK_NAME}" --driver bridge --subnet "${NETWORK_SUBNET}"
}

create_cluster() {
  echo "creating kind cluster '${CLUSTER_NAME}' using network '${NETWORK_NAME}'"
  KIND_EXPERIMENTAL_DOCKER_NETWORK="${NETWORK_NAME}" kind create cluster \
    --name "${CLUSTER_NAME}" \
    --config "${KIND_CONFIG}" \
    "${@}"
}

create_network
create_cluster "$@"
