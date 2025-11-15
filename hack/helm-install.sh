#!/usr/bin/env bash
set -euo pipefail
HELM_RELEASE=${HELM_RELEASE:-rate-limiter}
KUBE_NAMESPACE=${KUBE_NAMESPACE:-rate-limiter}
HELM_CHART=${HELM_CHART:-deploy/helm/rate-limiter}
IMAGE=${IMAGE:-rate-limiter:local}
HELM_EXTRA_ARGS=${HELM_EXTRA_ARGS:-}
if [[ -z ${HELM_RELEASE} ]]; then
  echo "[helm-install] HELM_RELEASE must be set" >&2
  exit 1
fi
if [[ -z ${KUBE_NAMESPACE} ]]; then
  echo "[helm-install] KUBE_NAMESPACE must be set" >&2
  exit 1
fi
if [[ -z ${HELM_CHART} ]]; then
  echo "[helm-install] HELM_CHART must be set" >&2
  exit 1
fi
IMAGE_REPO=$IMAGE
IMAGE_TAG=latest
if [[ ${IMAGE_REPO} == *":"* ]]; then
  IMAGE_TAG=${IMAGE_REPO##*:}
  IMAGE_REPO=${IMAGE_REPO%:*}
fi
if [[ -z ${IMAGE_REPO} ]]; then
  echo "[helm-install] unable to derive repository from IMAGE='${IMAGE}'" >&2
  exit 1
fi
read -r -a extra_args <<<"${HELM_EXTRA_ARGS}"
if [[ -z ${HELM_EXTRA_ARGS// } ]]; then
  extra_args=()
fi
set -x
helm upgrade --install "${HELM_RELEASE}" "${HELM_CHART}" \
  --namespace "${KUBE_NAMESPACE}" --create-namespace \
  --set image.repository="${IMAGE_REPO}" \
  --set image.tag="${IMAGE_TAG}" \
  "${extra_args[@]}"
set +x
echo "[helm-install] release '${HELM_RELEASE}' installed in namespace '${KUBE_NAMESPACE}'"
