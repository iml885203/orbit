#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

for readme in README.md README.zh-TW.md; do
  first_run_section="$(
    awk '
      /^## (First 5 minutes|五分鐘上手)$/ { capture = 1; next }
      capture && /^## / { exit }
      capture { print }
    ' "$repo_root/$readme"
  )"

  for command in "orbit init --yes" "orbit up" "orbit status" "orbit open demo-api"; do
    if ! grep -Fx "$command" <<<"$first_run_section" >/dev/null; then
      echo "$readme first-run section is missing: $command" >&2
      exit 1
    fi
  done

  if grep -E 'orbit .*docs/examples/|orbit .*README' <<<"$first_run_section" >/dev/null; then
    echo "$readme first-run section depends on a source checkout." >&2
    exit 1
  fi
done

if [ "${ORBIT_DOCS_ONLY:-}" = "1" ]; then
  echo "README first-run commands are location-independent"
  exit 0
fi

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
export ORBIT_HOME="$test_root/orbit-home"
export ORBIT_NAMESPACE="first-run-$$"
export ORBIT_DASHBOARD_PORT="$((23000 + ($$ % 1000)))"

cleanup() {
  "$orbit_bin" down --json >/dev/null 2>&1 || true
  "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  if [ -n "${port_guard_pid:-}" ]; then
    kill "$port_guard_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT

mkdir -p "$test_root/empty-directory"
cd "$test_root/empty-directory"

python3 -c '
import socket
import time

listeners = []
for port in (26379, 28080):
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("127.0.0.1", port))
    listener.listen()
    listeners.append(listener)
time.sleep(600)
' &
port_guard_pid=$!

"$orbit_bin" init --yes
if ! "$orbit_bin" up --json >"$test_root/up.json" 2>"$test_root/up.stderr"; then
  echo "orbit up failed during the first-five-minutes acceptance test." >&2
  if [ -s "$test_root/up.json" ]; then
    echo "orbit up JSON:" >&2
    sed 's/^/  /' "$test_root/up.json" >&2
  fi
  if [ -s "$test_root/up.stderr" ]; then
    echo "orbit up stderr:" >&2
    sed 's/^/  /' "$test_root/up.stderr" >&2
  fi
  echo "orbit status after the failure:" >&2
  "$orbit_bin" status --json >&2 || true
  if [ -s "$ORBIT_HOME/daemon.log" ]; then
    echo "daemon log tail:" >&2
    tail -n 80 "$ORBIT_HOME/daemon.log" | sed 's/^/  /' >&2
  fi
  exit 1
fi
"$orbit_bin" status --json >"$test_root/status.json"
browser_stub_dir="$test_root/browser-stub"
mkdir -p "$browser_stub_dir"
printf '#!/bin/sh\nexit 0\n' >"$browser_stub_dir/open"
cp "$browser_stub_dir/open" "$browser_stub_dir/xdg-open"
chmod +x "$browser_stub_dir/open" "$browser_stub_dir/xdg-open"
PATH="$browser_stub_dir:$PATH" "$orbit_bin" open demo-api --json >"$test_root/open-service.json"
PATH="$browser_stub_dir:$PATH" "$orbit_bin" open --json >"$test_root/open-dashboard.json"
demo_url="$(
  python3 -c '
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))["data"]
resources = {resource["name"]: resource for resource in payload["resources"]}
assert resources["redis"]["ports"]["redis"] != 26379
assert resources["demo-api"]["ports"]["http"] != 28080
print(resources["demo-api"]["url"])
' "$test_root/status.json"
)"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "${demo_url%/}/health" >"$test_root/health.json"
curl --fail --silent --show-error \
  "${demo_url%/}/api/visits" >"$test_root/visits-first.json"
curl --fail --silent --show-error \
  "${demo_url%/}/api/visits" >"$test_root/visits-second.json"

python3 - "$test_root" "$demo_url" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
demo_url = sys.argv[2]

def read(name):
    return json.loads((root / name).read_text(encoding="utf-8"))

up = read("up.json")
status = read("status.json")
health = read("health.json")
service = read("open-service.json")
dashboard = read("open-dashboard.json")
first = read("visits-first.json")
second = read("visits-second.json")

assert up["ok"] is True
assert status["ok"] is True
assert health["ok"] is True
assert up["recommended_actions"] == [{
    "command": "orbit open demo-api --json",
    "reason": f"Open demo-api at {demo_url}.",
    "destructive": False,
}]
assert status["recommended_actions"][0]["command"] == "orbit open demo-api --json"
assert service["data"] == {
    "url": demo_url,
    "target": "service",
    "service": "demo-api",
    "opened": True,
}
assert dashboard["data"]["target"] == "dashboard"
assert dashboard["data"]["url"] != demo_url
assert second["visits"] == first["visits"] + 1
PY

echo "README first five minutes reaches the service value before the dashboard"
