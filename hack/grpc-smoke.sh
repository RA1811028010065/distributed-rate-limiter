#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DEFAULT_CLIENT_ADDR="http://localhost:8081"
CLIENT_ADDR=${GRPC_CLIENT_ADDR:-$DEFAULT_CLIENT_ADDR}
SMOKE_HTTP_ADDR=${GRPC_SMOKE_HTTP_ADDR:-:38080}
SMOKE_GRPC_ADDR=${GRPC_SMOKE_GRPC_ADDR:-:38081}
SMOKE_CLIENT_ADDR=${GRPC_SMOKE_CLIENT_ADDR:-http://127.0.0.1:38081}
START_SERVER=0
if [[ ${GRPC_SMOKE_SKIP_SERVER:-0} == 1 ]]; then
  START_SERVER=0
elif [[ ${GRPC_SMOKE_FORCE_SERVER:-0} == 1 ]]; then
  START_SERVER=1
elif [[ ${GRPC_SMOKE_MODE:-auto} == server ]]; then
  START_SERVER=1
elif [[ ${GRPC_SMOKE_MODE:-auto} == external ]]; then
  START_SERVER=0
elif [[ $CLIENT_ADDR == "$DEFAULT_CLIENT_ADDR" ]]; then
  START_SERVER=1
fi
SERVER_PID=
LOG_FILE=
cleanup(){
  if [[ -n ${SERVER_PID:-} ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [[ -n ${LOG_FILE:-} && -f $LOG_FILE ]]; then
    rm -f "$LOG_FILE"
  fi
}
trap cleanup EXIT
wait_for_port(){
  local host=$1
  local port=$2
  local attempts=${3:-120}
  local sleep_interval=${4:-0.5}
  for ((i=0;i<attempts;i++)); do
    if python - "$host" "$port" >/dev/null 2>&1 <<'PY'; then
import socket
import sys
host = sys.argv[1]
port = int(sys.argv[2])
sock = socket.socket()
sock.settimeout(0.5)
try:
    sock.connect((host, port))
except OSError:
    sys.exit(1)
else:
    sock.close()
PY
      return 0
    fi
    sleep "$sleep_interval"
  done
  echo "[grpc-smoke] timeout waiting for $host:$port" >&2
  if [[ -n ${LOG_FILE:-} && -f $LOG_FILE ]]; then
    echo "--- rate-limiter logs ---" >&2
    cat "$LOG_FILE" >&2
    echo "-------------------------" >&2
  fi
  exit 1
}
if [[ $START_SERVER -eq 1 ]]; then
  CLIENT_ADDR=$SMOKE_CLIENT_ADDR
  LOG_FILE=$(mktemp)
  echo "[grpc-smoke] starting temporary rate-limiter (HTTP ${SMOKE_HTTP_ADDR}, gRPC ${SMOKE_GRPC_ADDR})"
  (
    cd "$ROOT_DIR"
    HTTP_ADDR=$SMOKE_HTTP_ADDR \
    GRPC_ADDR=$SMOKE_GRPC_ADDR \
    LOG_PATH= \
    go run ./cmd/ratelimiter
  ) >"$LOG_FILE" 2>&1 &
  SERVER_PID=$!
  client_host_port=${CLIENT_ADDR#http://}
  client_host_port=${client_host_port#https://}
  client_host_port=${client_host_port%%/*}
  client_host=${client_host_port%%:*}
  client_port=${client_host_port##*:}
  if [[ -z $client_host || $client_host == $client_port ]]; then
    client_host=127.0.0.1
  fi
  if [[ -z $client_port || $client_port == $client_host ]]; then
    client_port=80
  fi
  echo "[grpc-smoke] parsed temporary endpoint ${client_host}:${client_port}"
  echo "[grpc-smoke] waiting for $client_host:$client_port"
  wait_for_port "$client_host" "$client_port"
else
  echo "[grpc-smoke] using existing service at $CLIENT_ADDR"
fi
cd "$ROOT_DIR"
set -x
go run ./cmd/grpcclient \
  -addr "$CLIENT_ADDR" \
  -key ${GRPC_SMOKE_KEY:-grpc-smoke} \
  -tokens ${GRPC_SMOKE_TOKENS:-1} \
  -max-tokens ${GRPC_SMOKE_MAX_TOKENS:-5} \
  -refill-rate ${GRPC_SMOKE_REFILL_RATE:-5} \
  -source ${GRPC_SMOKE_SOURCE:-cli}
set +x
echo "[grpc-smoke] success"
