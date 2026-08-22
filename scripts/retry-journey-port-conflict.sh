#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: retry-journey-port-conflict.sh <command> [args...]" >&2
  exit 2
fi

max_attempts="${ORBIT_JOURNEY_PORT_ATTEMPTS:-2}"
case "$max_attempts" in
  ''|*[!0-9]*|0)
    echo "ORBIT_JOURNEY_PORT_ATTEMPTS must be a positive integer." >&2
    exit 2
    ;;
esac

attempt=1
output="$(mktemp)"
resource_state="$(mktemp -d /tmp/orbit-journey-resources.XXXXXX)"
namespace_registry="$resource_state/namespaces"
: >"$namespace_registry"
cleanup() {
  command_status="$?"
  cleanup_status=0
  trap - EXIT
  if [ "${ORBIT_JOURNEY_SHARED_RESOURCE_CLEANUP:-0}" != "1" ]; then
    "$(dirname "$0")/clean-journey-docker-resources.sh" "$namespace_registry" || cleanup_status="$?"
  fi
  rm -f "$output"
  rm -rf "$resource_state"
  if [ "$command_status" -eq 0 ] && [ "$cleanup_status" -ne 0 ]; then
    command_status="$cleanup_status"
  fi
  exit "$command_status"
}
trap cleanup EXIT

if [ "${ORBIT_JOURNEY_SHARED_RESOURCE_CLEANUP:-0}" != "1" ]; then
  export ORBIT_JOURNEY_NAMESPACE_REGISTRY="$namespace_registry"
fi

while [ "$attempt" -le "$max_attempts" ]; do
  : >"$output"
  set +e
  ORBIT_JOURNEY_ATTEMPT="$attempt" "$@" 2>&1 | tee "$output"
  command_status="${PIPESTATUS[0]}"
  set -e

  if [ "$command_status" -eq 0 ]; then
    exit 0
  fi
  terminal_line="$(awk 'NF { line=$0 } END { sub(/^[[:space:]]+/, "", line); sub(/[[:space:]]+$/, "", line); print line }' "$output")"
  classification_lines=12
  if [ "$terminal_line" = "}" ]; then
    classification_lines=100
  fi
  last_error_code="$(tail -n "$classification_lines" "$output" | sed -nE 's/.*"code"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' | tail -n 1)"
  retryable=1
  if [ -n "$last_error_code" ]; then
    case "$last_error_code" in
      resource_port_conflict|dashboard_port_conflict) retryable=0 ;;
      checks_failed)
        if tail -n 100 "$output" | grep -Eiq 'port [0-9]+.*already in use'; then
          retryable=0
        fi
        ;;
    esac
  elif tail -n 12 "$output" |
    grep -Eiq 'dashboard port [0-9]+ already in use|address already in use|port is already allocated'; then
    retryable=0
  fi
  if [ "$attempt" -ge "$max_attempts" ] || [ "$retryable" -ne 0 ]; then
    exit "$command_status"
  fi

  echo "Journey hit a transient port conflict; retrying the isolated journey ($((attempt + 1))/$max_attempts)." >&2
  sleep "${ORBIT_JOURNEY_RETRY_DELAY_SECONDS:-0}"
  attempt=$((attempt + 1))
done
