#!/bin/sh
# All-in-one entrypoint.
#
# With no arguments: starts the master, waits for it to become healthy, then
# starts a worker pointed at the in-container master. The worker registers
# itself with the master via its heartbeat, so a single container can scan
# /mnt/storage/videos and process jobs end to end.
#
# With arguments: execs them directly, e.g.
#   docker run <image> ./video-converter-cli --help
set -eu

if [ "$#" -gt 0 ]; then
    exec "$@"
fi

APP_DIR=/app
MASTER_CONFIG="$APP_DIR/config-master.yaml"
WORKER_CONFIG="$APP_DIR/config-worker.yaml"
PORT="${DARKSTREAM_SERVER_PORT:-8080}"
WORKER_ID="${DARKSTREAM_WORKER_ID:-all-in-one-worker}"
MASTER_URL="${DARKSTREAM_MASTER_URL:-http://localhost:$PORT}"

# The master picks up DARKSTREAM_API_KEY / DARKSTREAM_SERVER_* env overrides on
# its own; inject the same API key and master URL into the worker config so the
# worker can authenticate. (The master refuses an api_key over plain HTTP
# unless TLS cert/key are also set.)
if [ -n "${DARKSTREAM_API_KEY:-}" ]; then
    # The key is injected into the worker config via sed below; reject
    # characters that would corrupt the generated YAML or the sed
    # expression, instead of failing auth later with no clue why.
    case "$DARKSTREAM_API_KEY" in
        *[!A-Za-z0-9._-]*)
            echo "DARKSTREAM_API_KEY contains characters outside [A-Za-z0-9._-]; refusing to inject it into the worker config" >&2
            exit 1
            ;;
    esac
    sed -i "s|^  api_key: .*|  api_key: \"$DARKSTREAM_API_KEY\"|" "$WORKER_CONFIG"
fi
sed -i "s|^  master_url: .*|  master_url: \"$MASTER_URL\"|" "$WORKER_CONFIG"
sed -i "s|^  id: .*|  id: \"$WORKER_ID\"|" "$WORKER_CONFIG"

echo "Starting master (config: $MASTER_CONFIG)..."
"$APP_DIR/video-converter-master" -config "$MASTER_CONFIG" &
MASTER_PID=$!

# Wait until the master answers health checks. healthz only needs the database;
# readyz additionally requires a running scan location and may lag behind.
i=0
until curl -fsS "http://localhost:$PORT/healthz" >/dev/null 2>&1; do
    i=$((i + 1))
    if ! kill -0 "$MASTER_PID" 2>/dev/null; then
        echo "Master exited before becoming healthy" >&2
        exit 1
    fi
    if [ "$i" -ge 60 ]; then
        echo "Master did not become healthy within 60s" >&2
        kill "$MASTER_PID" 2>/dev/null || true
        exit 1
    fi
    sleep 1
done
echo "Master is up on port $PORT."

echo "Starting worker '$WORKER_ID' -> $MASTER_URL (self-registers via heartbeat)..."
"$APP_DIR/video-converter-worker" -config "$WORKER_CONFIG" -url "$MASTER_URL" -id "$WORKER_ID" &
WORKER_PID=$!

trap 'kill "$WORKER_PID" "$MASTER_PID" 2>/dev/null || true' TERM INT

# If either the master or the worker exits, bring the container down so the
# restart policy starts a clean one: a lone master serves /healthz but never
# converts anything, and a lone worker cannot reach its master. (sleep runs in
# the background so a trapped TERM/INT interrupts the wait immediately.)
while kill -0 "$MASTER_PID" 2>/dev/null && kill -0 "$WORKER_PID" 2>/dev/null; do
    sleep 5 & wait $! || true
done
kill "$WORKER_PID" "$MASTER_PID" 2>/dev/null || true
wait 2>/dev/null || true
