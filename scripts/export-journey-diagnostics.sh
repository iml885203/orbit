#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: export-journey-diagnostics.sh <journey> <status> <test-root>" >&2
  exit 2
fi

journey="$1"
status="$2"
test_root="$3"

if [ "$status" -eq 0 ] || [ -z "${ORBIT_JOURNEY_ARTIFACT_DIR:-}" ]; then
  exit 0
fi

attempt="${ORBIT_JOURNEY_ATTEMPT:-1}"
"$(dirname "$0")/export-journey-diagnostics.py" \
  "$journey" "$attempt" "$test_root" "$ORBIT_JOURNEY_ARTIFACT_DIR"
