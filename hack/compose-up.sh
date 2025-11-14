#!/usr/bin/env bash
set -euo pipefail
CMD_RAW=${DOCKER_COMPOSE:-"docker compose"}
read -r -a CMD_ARR <<<"$CMD_RAW"
PROJECT_DIR=${COMPOSE_PROJECT_DIR:-deploy}
EXTRA_ARGS=${COMPOSE_EXTRA_ARGS:-}
ARGS=(up --detach --build --quiet-pull --remove-orphans)
if [ -n "$EXTRA_ARGS" ]; then
  # shellcheck disable=SC2206
  EXTRA_SPLIT=($EXTRA_ARGS)
  ARGS+=("${EXTRA_SPLIT[@]}")
fi
run_compose(){
  local buildkit=$1
  shift || true
  local env_prefix=(COMPOSE_DOCKER_CLI_BUILD=$buildkit DOCKER_BUILDKIT=$buildkit)
  (cd "$PROJECT_DIR" && "${env_prefix[@]}" "${CMD_ARR[@]}" "${ARGS[@]}" "$@")
}
tmp_log=$(mktemp)
trap 'rm -f "$tmp_log"' EXIT
if [ "${COMPOSE_DISABLE_BUILDKIT:-0}" = "1" ]; then
  run_compose 0 "$@"
  exit 0
fi
set +e
run_compose 1 "$@" 2>&1 | tee "$tmp_log"
status=${PIPESTATUS[0]}
set -e
if [ $status -eq 0 ]; then
  exit 0
fi
if grep -qi "unsupported shim version (3)" "$tmp_log"; then
  echo "[compose-up] detected shim incompatibility; retrying without BuildKit" >&2
  run_compose 0 "$@"
else
  exit $status
fi
