#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: register-journey-namespace.sh <namespace>" >&2
  exit 2
fi

registry="${ORBIT_JOURNEY_NAMESPACE_REGISTRY:-}"
[ -n "$registry" ] || exit 0
printf '%s\n' "$1" >>"$registry"
