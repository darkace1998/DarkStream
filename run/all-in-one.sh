#!/usr/bin/env bash
# DarkStream all-in-one local launcher (master + worker on one host).
# Usage:  ./run/all-in-one.sh start | stop | status | logs | reset
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN="$ROOT/run"
BIN="$ROOT/bin"

build() {
  # Real Vulkan device detection lives behind the "vulkan" build tag; without it
  # the worker links a stub that always reports "Vulkan support not compiled in".
  # It is opt-in because it needs the Vulkan dev headers at build time:
  #   apt-get install -y libvulkan-dev pkg-config
  # "vk_headless" (golang-vulkan-api >= v1.2.1) drops the X11/Wayland surface
  # backends, which DarkStream never uses — a transcoding server has no windows —
  # so no windowing dev packages are required.
  # Enable with:  VULKAN=1 ./run/all-in-one.sh start
  local tags=()
  if [ "${VULKAN:-0}" = "1" ]; then
    tags=(-tags vulkan,vk_headless)
    echo "==> building (vulkan detection enabled, headless)"
  else
    echo "==> building"
  fi
  (cd "$ROOT/video-converter-master" && go build -o "$BIN/darkstream-master" .) || exit 1
  (cd "$ROOT/video-converter-worker" && go build "${tags[@]}" -o "$BIN/darkstream-worker" .) || exit 1
  (cd "$ROOT/video-converter-cli"    && go build "${tags[@]}" -o "$BIN/darkstream-cli" .)    || exit 1
}

start() {
  # Refuse to stack a second copy on top of a running one. Both processes would
  # register under the same worker ID, so the dashboard shows one worker while
  # two compete for jobs and for the machine's cores — which looks like a
  # phantom extra worker and oversubscribes the box.
  if running_pids | read -r _; then
    echo "already running (use '''$0 restart''' or '''$0 stop''' first):"
    running_pids | while read -r pid; do echo "  pid $pid"; done
    exit 1
  fi

  mkdir -p "$RUN/videos" "$RUN/converted" "$RUN/cache" "$RUN/db"
  # Seed sample videos on first run.
  if ! ls "$RUN/videos"/*.mp4 >/dev/null 2>&1; then
    cp "$ROOT"/testvideo*.mp4 "$RUN/videos/" 2>/dev/null || true
  fi

  # The master no longer needs a YAML file: bootstrap comes from environment
  # variables and everything else is configured in the Web UI. Set NO_YAML=1 to
  # exercise that path; otherwise the sample YAML seeds the store on first run.
  echo "==> starting master"
  if [ "${NO_YAML:-0}" = "1" ]; then
    DARKSTREAM_DB_PATH="$RUN/db/jobs.db" \
      nohup "$BIN/darkstream-master" -config /nonexistent.yaml >"$RUN/master.console.log" 2>&1 &
  else
    nohup "$BIN/darkstream-master" -config "$RUN/master.yaml" >"$RUN/master.console.log" 2>&1 &
  fi
  echo $! >"$RUN/master.pid"

  # Wait for the master to accept connections before starting the worker.
  for _ in $(seq 1 30); do
    curl -sf localhost:8080/healthz >/dev/null 2>&1 && break
    sleep 0.5
  done

  echo "==> starting worker"
  nohup "$BIN/darkstream-worker" -config "$RUN/worker.yaml" >"$RUN/worker.console.log" 2>&1 &
  echo $! >"$RUN/worker.pid"

  echo
  echo "Dashboard : http://localhost:8080/"
  echo "Metrics   : http://localhost:8080/metrics"
  echo "Libraries : http://localhost:8080/ -> Libraries tab"
  echo "CLI       : $BIN/darkstream-cli status --watch"
}

# running_pids lists the PIDs of any master or worker started from this
# directory, one per line. It matches on the binary path rather than a name, so
# it never matches this script or an unrelated process.
running_pids() {
  # Matched on the "-config" argument as well as the binary name, so a copy
  # started by hand with a relative path is found too, while this script (which
  # takes no such argument) never matches itself.
  pgrep -f "darkstream-(master|worker) -config" 2>/dev/null || true
}

stop() {
  # The pid files are the fast path, but a process started by hand — or one
  # whose pid file was lost — would otherwise survive and keep taking jobs, so
  # sweep for strays afterwards.
  for p in master worker; do
    if [ -f "$RUN/$p.pid" ]; then
      kill "$(cat "$RUN/$p.pid")" 2>/dev/null && echo "stopped $p"
      rm -f "$RUN/$p.pid"
    fi
  done

  local strays
  strays=$(running_pids)
  if [ -n "$strays" ]; then
    echo "$strays" | while read -r pid; do
      kill "$pid" 2>/dev/null && echo "stopped stray pid $pid"
    done
  fi

  # Give a conversion in flight a moment to be cancelled, so FFmpeg is not left
  # holding the GPU.
  for _ in $(seq 1 20); do
    running_pids | read -r _ || break
    sleep 0.5
  done
}

status() {
  pgrep -af "darkstream-master|darkstream-worker" | grep -v "all-in-one" || echo "not running"
  echo
  "$BIN/darkstream-cli" status 2>&1 | tail -n 12
}

case "${1:-start}" in
  start)  build && start ;;
  stop)   stop ;;
  status) status ;;
  logs)   tail -n 50 "$RUN/master.console.log" "$RUN/worker.console.log" ;;
  reset)  stop; rm -rf "$RUN/db" "$RUN/cache" "$RUN/converted" "$RUN"/*.log "$RUN"/*.console.log
          echo "state cleared (videos kept)" ;;
  *)      echo "usage: $0 {start|stop|status|logs|reset}"; exit 1 ;;
esac
