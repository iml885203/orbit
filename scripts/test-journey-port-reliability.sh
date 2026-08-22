#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
retry_runner="$repo_root/scripts/retry-journey-port-conflict.sh"
port_fixture="$repo_root/scripts/hold-test-ports.py"
resource_cleaner="$repo_root/scripts/clean-journey-docker-resources.sh"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"
test_root="$(mktemp -d)"
existing_network="orbit-harness-existing-$$"
new_network="orbit-harness-new-$$"
cleanup() {
  if [ -n "${release_watcher_pid:-}" ]; then
    kill "$release_watcher_pid" >/dev/null 2>&1 || true
    wait "$release_watcher_pid" >/dev/null 2>&1 || true
  fi
  if [ -n "${relocation_guard_pid:-}" ]; then
    kill "$relocation_guard_pid" >/dev/null 2>&1 || true
    wait "$relocation_guard_pid" >/dev/null 2>&1 || true
  fi
  docker network rm "$new_network" "$existing_network" >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap cleanup EXIT

cat >"$test_root/transient-conflict.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
count=0
if [ -f "$1" ]; then
  count="$(cat "$1")"
fi
count=$((count + 1))
printf '%s\n' "$count" >"$1"
if [ "$count" -eq 1 ]; then
  printf '%s\n' '{"error":{"code":"resource_port_conflict"}}'
  exit 1
fi
echo "journey recovered"
SH
chmod +x "$test_root/transient-conflict.sh"

"$retry_runner" "$test_root/transient-conflict.sh" "$test_root/transient-count" \
  >"$test_root/transient-output" 2>&1
grep -F "journey recovered" "$test_root/transient-output" >/dev/null
grep -F "retrying the isolated journey (2/2)" "$test_root/transient-output" >/dev/null
test "$(cat "$test_root/transient-count")" = "2"

cat >"$test_root/dashboard-conflict.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
count=0
if [ -f "$1" ]; then
  count="$(cat "$1")"
fi
count=$((count + 1))
printf '%s\n' "$count" >"$1"
if [ "$count" -eq 1 ]; then
  echo "Error: dashboard port 19800 already in use; port 19801 is available"
  exit 1
fi
echo "dashboard recovered"
SH
chmod +x "$test_root/dashboard-conflict.sh"

"$retry_runner" "$test_root/dashboard-conflict.sh" "$test_root/dashboard-count" \
  >"$test_root/dashboard-output" 2>&1
grep -F "dashboard recovered" "$test_root/dashboard-output" >/dev/null
test "$(cat "$test_root/dashboard-count")" = "2"

cat >"$test_root/product-failure.sh" <<'SH'
#!/usr/bin/env bash
echo '{"error":{"code":"invalid_environment"}}'
exit 7
SH
chmod +x "$test_root/product-failure.sh"

set +e
"$retry_runner" "$test_root/product-failure.sh" >"$test_root/product-output" 2>&1
product_status=$?
set -e
test "$product_status" -eq 7
test "$(grep -c 'invalid_environment' "$test_root/product-output")" -eq 1
if grep -F "retrying the isolated journey" "$test_root/product-output" >/dev/null; then
  echo "retry runner retried a non-port product failure." >&2
  exit 1
fi

cat >"$test_root/mixed-failure.sh" <<'SH'
#!/usr/bin/env bash
echo "earlier expected fixture: address already in use"
echo '{"error":{"code":"invalid_environment"}}'
exit 9
SH
chmod +x "$test_root/mixed-failure.sh"
set +e
"$retry_runner" "$test_root/mixed-failure.sh" >"$test_root/mixed-output" 2>&1
mixed_status=$?
set -e
test "$mixed_status" -eq 9
test "$(grep -c 'invalid_environment' "$test_root/mixed-output")" -eq 1
if grep -F "retrying the isolated journey" "$test_root/mixed-output" >/dev/null; then
  echo "retry runner used an earlier port message to mask the terminal product failure." >&2
  exit 1
fi

cat >"$test_root/stale-structured-conflict.sh" <<'SH'
#!/usr/bin/env bash
echo '{"error":{"code":"resource_port_conflict"}}'
for line in $(seq 1 13); do
  echo "later assertion context $line"
done
echo "assertion failed"
exit 8
SH
chmod +x "$test_root/stale-structured-conflict.sh"
set +e
"$retry_runner" "$test_root/stale-structured-conflict.sh" >"$test_root/stale-structured-output" 2>&1
stale_structured_status=$?
set -e
test "$stale_structured_status" -eq 8
if grep -F "retrying the isolated journey" "$test_root/stale-structured-output" >/dev/null; then
  echo "retry runner used a stale structured port error to mask a later assertion." >&2
  exit 1
fi

set +e
"$port_fixture" hold "$test_root/fixture-ready" "$test_root/fixture-error" \
  28070 28070 >"$test_root/fixture-output" 2>&1
fixture_status=$?
set -e
test "$fixture_status" -eq 1
test ! -e "$test_root/fixture-ready"
grep -F "test fixture could not own port 28070" "$test_root/fixture-error" >/dev/null

if [ -x "$orbit_bin" ]; then
  "$port_fixture" hold "$test_root/relocation-ready" "$test_root/relocation-error" 21081 &
  relocation_guard_pid=$!
  for _ in $(seq 1 50); do
    [ -f "$test_root/relocation-ready" ] && break
    if [ -f "$test_root/relocation-error" ]; then
      cat "$test_root/relocation-error" >&2
      exit 1
    fi
    sleep 0.1
  done
  test -f "$test_root/relocation-ready"
  (
    for _ in $(seq 1 100); do
      if grep -F "checks_failed" "$test_root/relocation-output" >/dev/null 2>&1; then
        kill "$relocation_guard_pid" >/dev/null 2>&1 || true
        exit 0
      fi
      sleep 0.05
    done
    exit 1
  ) &
  release_watcher_pid=$!
  ORBIT_BIN="$orbit_bin" ORBIT_JOURNEY_RETRY_DELAY_SECONDS=0.2 \
    "$retry_runner" "$repo_root/scripts/test-runtime-adoption.sh" \
    >"$test_root/relocation-output" 2>&1
  wait "$release_watcher_pid"
  grep -F "Python, Node, and Go projects adopt Orbit" "$test_root/relocation-output" >/dev/null
  test "$(grep -c 'retrying the isolated journey (2/2)' "$test_root/relocation-output")" -eq 1
  wait "$relocation_guard_pid" >/dev/null 2>&1 || true
  relocation_guard_pid=""
fi

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  docker network create "$existing_network" >/dev/null
  docker network create "$new_network" >/dev/null
  printf '%s\n' "harness-new-$$" >"$test_root/namespaces"
  "$resource_cleaner" "$test_root/namespaces"
  docker network inspect "$existing_network" >/dev/null
  if docker network inspect "$new_network" >/dev/null 2>&1; then
    echo "journey resource cleanup retained a network created after its snapshot." >&2
    exit 1
  fi
fi

cat >"$test_root/cleanup-failure.sh" <<'SH'
#!/usr/bin/env bash
printf '%s\n' '../unsafe' >>"$ORBIT_JOURNEY_NAMESPACE_REGISTRY"
exit 0
SH
chmod +x "$test_root/cleanup-failure.sh"
set +e
"$retry_runner" "$test_root/cleanup-failure.sh" >"$test_root/cleanup-failure-output" 2>&1
cleanup_failure_status=$?
set -e
test "$cleanup_failure_status" -ne 0
grep -F "Refusing to clean invalid journey namespace" "$test_root/cleanup-failure-output" >/dev/null

echo "Journey harness retries only bounded port conflicts and removes only newly created Docker resources"
