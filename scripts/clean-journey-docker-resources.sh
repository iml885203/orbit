#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: clean-journey-docker-resources.sh <namespace-registry>" >&2
  exit 2
fi

registry="$1"
[ -f "$registry" ] || exit 0
if [ ! -s "$registry" ]; then
  exit 0
fi
if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "Docker is unavailable; registered journey resources cannot be verified or cleaned." >&2
  exit 1
fi

cleanup_failed=0
while IFS= read -r namespace; do
  [ -z "$namespace" ] && continue
  case "$namespace" in
    *[!A-Za-z0-9_.-]*|.*|-*)
      echo "Refusing to clean invalid journey namespace: $namespace" >&2
      cleanup_failed=1
      continue
      ;;
  esac

  container_ids="$(docker ps -aq --filter "label=orbit.namespace=$namespace")"
  if [ -n "$container_ids" ]; then
    docker rm -f -v $container_ids >/dev/null || cleanup_failed=1
  fi

  network_name="orbit-$namespace"
  network_ids="$(docker network ls -q --filter "name=^${network_name}$")"
  if [ -n "$network_ids" ]; then
    while IFS= read -r network_id; do
      [ -z "$network_id" ] && continue
      actual_name="$(docker network inspect -f '{{.Name}}' "$network_id")"
      [ "$actual_name" = "$network_name" ] || continue
      docker network rm "$network_id" >/dev/null || cleanup_failed=1
    done <<EOF
$network_ids
EOF
  fi

  if docker ps -aq --filter "label=orbit.namespace=$namespace" | grep -q . ||
     docker network ls --format '{{.Name}}' --filter "name=^${network_name}$" | grep -Fxq "$network_name"; then
    echo "Journey namespace $namespace still owns Docker resources after cleanup." >&2
    cleanup_failed=1
  fi
done < <(sort -u "$registry")

exit "$cleanup_failed"
