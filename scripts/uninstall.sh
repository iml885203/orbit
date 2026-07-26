#!/usr/bin/env bash
# Uninstall the Orbit CLI.
#
# Stops services/containers via `orbit down`, then removes the binary and
# ~/.orbit state dir. Docker images and $WORKSPACE_ROOT repo clones are left
# alone — they may be shared with other projects / are user-owned.
#
# Usage:
#   curl -fsSL <url>/uninstall.sh | bash              # dry-run (preview)
#   curl -fsSL <url>/uninstall.sh | bash -s -- --yes  # actually remove
#   ./scripts/uninstall.sh --yes

set -euo pipefail

DRY_RUN=1
for arg in "$@"; do
  case "$arg" in
    -y|--yes) DRY_RUN=0 ;;
    -h|--help)
      sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

ORBIT_HOME="${HOME}/.orbit"
DOWN_TIMEOUT=30

find_binaries() {
  local seen=""
  local candidates=(
    "${HOME}/.local/bin/orbit"
    "/usr/local/bin/orbit"
  )
  if command -v orbit >/dev/null 2>&1; then
    candidates+=("$(command -v orbit)")
  fi
  for p in "${candidates[@]}"; do
    [ -e "$p" ] || continue
    local resolved
    resolved="$(readlink -f "$p" 2>/dev/null || echo "$p")"
    case ":$seen:" in
      *":${resolved}:"*) continue ;;
    esac
    seen="${seen}:${resolved}"
    echo "$p"
  done
}

daemon_running() {
  [ -S "${ORBIT_HOME}/orbit.sock" ] || return 1
  [ -f "${ORBIT_HOME}/orbit.pid" ] || return 1
  local pid
  pid="$(cat "${ORBIT_HOME}/orbit.pid" 2>/dev/null || true)"
  [ -n "$pid" ] || return 1
  kill -0 "$pid" 2>/dev/null
}

daemon_traces_present() {
  [ -S "${ORBIT_HOME}/orbit.sock" ] || [ -f "${ORBIT_HOME}/orbit.pid" ]
}

bring_down() {
  local orbit_cmd
  orbit_cmd="$(command -v orbit 2>/dev/null || true)"
  if [ -z "$orbit_cmd" ]; then
    # No orbit on PATH; try the first binary we found.
    orbit_cmd="$(find_binaries | head -n1)"
  fi
  if [ -z "$orbit_cmd" ]; then
    echo "  no orbit binary found — cannot bring down cleanly"
    return 1
  fi
  echo "Bringing orbit down (stops services and containers)..."
  "$orbit_cmd" down >/dev/null 2>&1 &
  local down_pid=$!
  local waited=0
  local down_ok=0
  while [ "$waited" -lt "$DOWN_TIMEOUT" ]; do
    if ! kill -0 "$down_pid" 2>/dev/null; then
      wait "$down_pid" 2>/dev/null || true
      echo "  done."
      down_ok=1
      break
    fi
    sleep 1
    waited=$((waited + 1))
  done
  if [ "$down_ok" -eq 0 ]; then
    kill "$down_pid" 2>/dev/null || true
    echo "  timed out after ${DOWN_TIMEOUT}s."
    echo "  Containers may still be running — inspect with 'docker ps'."
  fi

  echo "Stopping orbit daemon..."
  if "$orbit_cmd" daemon stop >/dev/null 2>&1; then
    echo "  done."
  else
    echo "  failed — daemon may already be stopped."
  fi
  [ "$down_ok" -eq 1 ] || return 1
  return 0
}

list_plan() {
  local bins
  bins="$(find_binaries || true)"
  if [ -z "$bins" ] && [ ! -e "$ORBIT_HOME" ]; then
    echo "Nothing to remove — orbit is not installed."
    return 1
  fi
  echo "This will remove:"
  if [ -n "$bins" ]; then
    while IFS= read -r p; do
      echo "  binary:  $p"
    done <<< "$bins"
  else
    echo "  binary:  (none found)"
  fi
  if [ -e "$ORBIT_HOME" ]; then
    echo "  config:  $ORBIT_HOME"
  else
    echo "  config:  (not present)"
  fi
  if daemon_running; then
    local pid
    pid="$(cat "${ORBIT_HOME}/orbit.pid" 2>/dev/null || true)"
    echo "  daemon:  running (pid ${pid}) — 'orbit down' + 'daemon stop' will run first"
  elif daemon_traces_present; then
    echo "  daemon:  stale traces found — 'orbit down' + 'daemon stop' will run first"
  fi
  return 0
}

main() {
  if ! list_plan; then
    exit 0
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    echo
    echo "Dry run. Re-run with --yes to actually remove."
    exit 0
  fi

  echo

  if daemon_traces_present; then
    bring_down || true
  fi

  while IFS= read -r p; do
    [ -n "$p" ] || continue
    echo "Removing $p..."
    if rm -f "$p" "${p}.prev" "${p}.prev.failed" 2>/dev/null; then
      echo "  done."
    else
      echo "  failed (permission denied?). Try: sudo rm -f $p ${p}.prev ${p}.prev.failed"
    fi
  done < <(find_binaries || true)

  if [ -e "$ORBIT_HOME" ]; then
    echo "Removing $ORBIT_HOME..."
    rm -rf "$ORBIT_HOME"
    echo "  done."
  fi

  echo
  echo "Orbit uninstalled."
  echo
  echo "Note: Docker images (example.db:latest, sql-server, redis, kafka, ...)"
  echo "      were not removed — other projects may share them."
  echo "      Run 'docker image prune' or remove specific images manually if desired."
  if [ -n "${WORKSPACE_ROOT:-}" ]; then
    echo
    echo "Note: \$WORKSPACE_ROOT (${WORKSPACE_ROOT}) was not touched — it's your git checkout."
  fi
}

main "$@"
