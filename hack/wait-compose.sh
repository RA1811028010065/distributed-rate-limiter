#!/usr/bin/env bash
set -euo pipefail
PROJECT_NAME=${COMPOSE_PROJECT:-deploy}
TIMEOUT=${COMPOSE_WAIT_TIMEOUT:-150}
INTERVAL=${COMPOSE_WAIT_INTERVAL:-3}
CMD=${DOCKER_COMPOSE:-"docker compose"}
PROJECT_DIR=${COMPOSE_PROJECT_DIR:-deploy}
CMD_BIN=${CMD%% *}
if ! command -v "$CMD_BIN" >/dev/null 2>&1; then
  echo "[compose-wait] command $CMD_BIN not found" >&2
  exit 1
fi
end=$((SECONDS + TIMEOUT))
log(){
  echo "[compose-wait] $*"
}
log "waiting for containers in project '${PROJECT_NAME}' to become healthy (timeout ${TIMEOUT}s)"
while [ $SECONDS -lt $end ]; do
  mapfile -t containers < <(docker ps --filter "label=com.docker.compose.project=${PROJECT_NAME}" --format '{{.ID}}')
  if [ ${#containers[@]} -eq 0 ]; then
    sleep "$INTERVAL"
    continue
  fi
  unhealthy=0
  for id in "${containers[@]}"; do
    status=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id") || status="unknown"
    name=$(docker inspect --format '{{.Name}}' "$id" | sed 's#^/##') || name="$id"
    log "container ${name} status=${status}"
    case "$status" in
      healthy|running)
        ;;
      starting)
        unhealthy=1
        ;;
      *)
        log "container ${name} reported status ${status}; streaming logs"
        docker logs "$id" || true
        exit 1
        ;;
    esac
  done
  if [ $unhealthy -eq 0 ]; then
    log "all containers healthy"
    exit 0
  fi
  sleep "$INTERVAL"
done
log "timeout waiting for containers to become healthy"
(cd "$PROJECT_DIR" && $CMD ps)
(cd "$PROJECT_DIR" && $CMD logs)
exit 1
