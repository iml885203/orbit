#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

for readme in README.md README.zh-TW.md; do
  install_line="$(grep -n '^curl -fsSL .*scripts/install.sh' "$repo_root/$readme" | cut -d: -f1)"
  for requirement in Git Docker "Python 3"; do
    requirement_line="$(grep -n -m1 "\\[$requirement\\]" "$repo_root/$readme" | cut -d: -f1)"
    if [ -z "$requirement_line" ] || [ "$requirement_line" -ge "$install_line" ]; then
      echo "$readme must name $requirement before the install command." >&2
      exit 1
    fi
  done

  first_run_section="$(
    awk '
      /^## (First 5 minutes|五分鐘上手)$/ { capture = 1; next }
      capture && /^## / { exit }
      capture { print }
    ' "$repo_root/$readme"
  )"

  for command in "orbit init --yes" "orbit up" "orbit status" "orbit open demo-shop" "orbit down"; do
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

"$orbit_bin" --help >"$test_root/root-help.txt"
"$orbit_bin" version --json >"$test_root/version.json"
"$orbit_bin" env --help >"$test_root/env-help.txt"
"$orbit_bin" logs --help >"$test_root/logs-help.txt"
"$orbit_bin" open --help >"$test_root/open-help.txt"
"$orbit_bin" switch --help >"$test_root/switch-help.txt"
"$orbit_bin" up --help >"$test_root/up-help.txt"
"$orbit_bin" down --help >"$test_root/down-help.txt"
grep -F "add orbit.yaml to the project root" "$test_root/root-help.txt" >/dev/null
for command in doctor down env init logs open restart status switch uninstall up update version; do
  grep -E "^[[:space:]]+$command[[:space:]]" "$test_root/root-help.txt" >/dev/null
done
for hidden in daemon edge history inspect service settings tracing; do
  if grep -E "^[[:space:]]+$hidden[[:space:]]" "$test_root/root-help.txt" >/dev/null; then
    echo "advanced command $hidden leaked into installed-user root help." >&2
    exit 1
  fi
done
for command in list sync; do
  grep -E "^[[:space:]]+$command[[:space:]]" "$test_root/env-help.txt" >/dev/null
done
grep -F -- "--tail" "$test_root/logs-help.txt" >/dev/null
grep -F "orbit up" "$test_root/switch-help.txt" >/dev/null
for help_file in open switch up down; do
  if grep -i "daemon" "$test_root/$help_file-help.txt" >/dev/null; then
    echo "$help_file help exposes daemon implementation details." >&2
    exit 1
  fi
done

missing_runtime_root="$test_root/missing-runtime"
mkdir -p "$missing_runtime_root/bin" "$missing_runtime_root/project/envs"
ln -s "$(command -v git)" "$missing_runtime_root/bin/git"
ln -s "$(command -v docker)" "$missing_runtime_root/bin/docker"
cat >"$missing_runtime_root/project/envs/quickstart.yaml" <<'YAML'
version: "3"
services:
  demo:
    type: python
    path: .
    command: python3 -m http.server 28080
YAML
set +e
(
  cd "$missing_runtime_root/project"
  PATH="$missing_runtime_root/bin" \
    ORBIT_HOME="$missing_runtime_root/home" \
    "$orbit_bin" init --yes
) >"$missing_runtime_root/init.out" 2>&1
missing_runtime_status=$?
set -e
if [ "$missing_runtime_status" -eq 0 ]; then
  echo "orbit init accepted an environment whose required Python runtime was missing." >&2
  exit 1
fi
for expected in \
  "Setup saved — one prerequisite remains" \
  "Next: Install Python 3:" \
  "Then: orbit up"; do
  if ! grep -F "$expected" "$missing_runtime_root/init.out" >/dev/null; then
    echo "missing-runtime init did not explain the next useful step: $expected" >&2
    cat "$missing_runtime_root/init.out" >&2
    exit 1
  fi
done

mkdir -p "$test_root/empty-directory"
cd "$test_root/empty-directory"

set +e
"$orbit_bin" doctor --json >"$test_root/doctor-before-init.json"
doctor_before_init_status=$?
set -e
if [ "$doctor_before_init_status" -eq 0 ]; then
  echo "orbit doctor unexpectedly accepted a clean home before setup." >&2
  exit 1
fi
python3 - "$test_root/doctor-before-init.json" <<'PY'
import json
import pathlib
import sys

response = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert response["ok"] is False
assert response["error"]["code"] == "setup_required"
assert [action["command"] for action in response["recommended_actions"]] == [
    "orbit init --yes --json"
]
PY

python3 -c '
import socket
import time

listeners = []
for port in (26379, 28080, 28101, 28102, 28103):
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
"$orbit_bin" status >"$test_root/status.txt"
"$orbit_bin" env list --json >"$test_root/env-list.json"
browser_stub_dir="$test_root/browser-stub"
mkdir -p "$browser_stub_dir"
printf '#!/bin/sh\nexit 0\n' >"$browser_stub_dir/open"
cp "$browser_stub_dir/open" "$browser_stub_dir/xdg-open"
chmod +x "$browser_stub_dir/open" "$browser_stub_dir/xdg-open"
PATH="$browser_stub_dir:$PATH" "$orbit_bin" open demo-shop --json >"$test_root/open-service.json"
PATH="$browser_stub_dir:$PATH" "$orbit_bin" open --json >"$test_root/open-dashboard.json"
demo_url="$(
  python3 -c '
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))["data"]
resources = {resource["name"]: resource for resource in payload["resources"]}
assert set(resources) == {
    "demo-shop",
    "shop-catalog-api",
    "shop-inventory-api",
    "shop-order-api",
    "redis",
}
assert resources["redis"]["ports"]["redis"] != 26379
assert resources["demo-shop"]["ports"]["http"] != 28080
assert resources["shop-catalog-api"]["ports"]["http"] != 28101
assert resources["shop-inventory-api"]["ports"]["http"] != 28102
assert resources["shop-order-api"]["ports"]["http"] != 28103
assert all(resource["state"] == "healthy" for resource in resources.values())
print(resources["demo-shop"]["url"])
' "$test_root/status.json"
)"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "${demo_url%/}/health" >"$test_root/health.json"

PATH="$(dirname "$orbit_bin"):$PATH" \
  python3 "$ORBIT_HOME/envs/seeds/mini-shop/smoke.py" \
  >"$test_root/mini-shop-smoke.txt"

expected_demo_ref="$(
  python3 -c 'import json, sys; print("v" + json.load(open(sys.argv[1], encoding="utf-8"))["version"])' \
    "$repo_root/plugins/orbit-agent/.codex-plugin/plugin.json"
)"

python3 - "$test_root" "$demo_url" "$expected_demo_ref" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
demo_url = sys.argv[2]
expected_demo_ref = sys.argv[3]

def read(name):
    return json.loads((root / name).read_text(encoding="utf-8"))

up = read("up.json")
status = read("status.json")
env_list = read("env-list.json")
version = read("version.json")
health = read("health.json")
service = read("open-service.json")
dashboard = read("open-dashboard.json")

assert up["ok"] is True
assert status["ok"] is True
assert env_list["ok"] is True
assert version["ok"] is True
assert version["schema_version"] == "orbit.cli.v1"
assert version["data"]["version"]
assert health["ok"] is True
status_source = status["data"]["environment"]["managed_source"]
list_source = env_list["data"]["environment"]["managed_source"]
assert status_source == list_source
assert status_source["url"] == "https://github.com/iml885203/orbit-demo.git"
assert status_source["ref"] == expected_demo_ref
assert len(status_source["commit"]) == 40
assert up["recommended_actions"] == [{
    "command": "orbit open demo-shop --json",
    "reason": f"Open demo-shop at {demo_url}.",
    "destructive": False,
}]
assert status["recommended_actions"][0]["command"] == "orbit open demo-shop --json"
assert service["data"] == {
    "url": demo_url,
    "target": "service",
    "service": "demo-shop",
    "opened": True,
}
assert dashboard["data"]["target"] == "dashboard"
assert dashboard["data"]["url"] != demo_url
PY

english_handoff="https://github.com/iml885203/orbit/blob/$expected_demo_ref/docs/local-first.md"
handoff_file="$ORBIT_HOME/envs/seeds/mini-shop/index.html"
if ! grep -F "$english_handoff" "$handoff_file" >/dev/null; then
  echo "demo handoff in $handoff_file does not match the Orbit release: $english_handoff" >&2
  exit 1
fi

if [ "$(grep -c "open in browser" "$test_root/status.txt")" -ne 1 ] ||
    ! grep -E 'orbit open[[:space:]]+demo-shop[[:space:]]+open in browser' \
      "$test_root/status.txt" >/dev/null; then
  echo "healthy status must lead to only the primary demo application." >&2
  cat "$test_root/status.txt" >&2
  exit 1
fi
if grep -E 'Source:|https://github.com/|[0-9a-f]{12}' "$test_root/status.txt" >/dev/null; then
  echo "daily status exposed environment-repository provenance." >&2
  cat "$test_root/status.txt" >&2
  exit 1
fi

grep -F \
  "failure added +0 reservations and +0 orders while preserving stock; compensation restored stock" \
  "$test_root/mini-shop-smoke.txt"

"$orbit_bin" down shop-inventory-api --json >"$test_root/down-inventory.json"
for _ in {1..20}; do
  "$orbit_bin" status --json >"$test_root/runtime-failure.json"
  if python3 - "$test_root/runtime-failure.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
resources = {resource["name"]: resource for resource in payload["data"]["resources"]}
assert resources["shop-inventory-api"]["state"] == "stopped"
assert resources["shop-order-api"]["state"] == "degraded"
assert resources["shop-order-api"]["blocked_by"] == "shop-inventory-api"
assert resources["demo-shop"]["state"] == "degraded"
assert resources["demo-shop"]["blocked_by"] in {"shop-inventory-api", "shop-order-api"}
assert payload["recommended_actions"] == [{
    "command": "orbit up shop-inventory-api --json",
    "reason": "Start shop-inventory-api, which is blocking dependent services.",
    "destructive": False,
}]
PY
  then
    break
  fi
  sleep 1
done

python3 - "$test_root/runtime-failure.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
resources = {resource["name"]: resource for resource in payload["data"]["resources"]}
assert resources["shop-inventory-api"]["state"] == "stopped"
assert resources["shop-order-api"]["state"] == "degraded"
assert resources["shop-order-api"]["blocked_by"] == "shop-inventory-api"
assert resources["demo-shop"]["state"] == "degraded"
assert resources["demo-shop"]["blocked_by"] in {"shop-inventory-api", "shop-order-api"}
assert payload["recommended_actions"][0]["command"] == "orbit up shop-inventory-api --json"
PY

sleep 4
"$orbit_bin" status --json >"$test_root/runtime-threshold.json"
python3 - "$test_root/runtime-threshold.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
resources = {resource["name"]: resource for resource in payload["data"]["resources"]}
assert resources["shop-order-api"]["state"] == "degraded"
assert resources["shop-order-api"]["blocked_by"] == "shop-inventory-api"
assert resources["shop-order-api"]["state_reason"] == "dependency shop-inventory-api is stopped"
assert resources["demo-shop"]["state"] == "degraded"
assert payload["recommended_actions"] == [{
    "command": "orbit up shop-inventory-api --json",
    "reason": "Start shop-inventory-api, which is blocking dependent services.",
    "destructive": False,
}]
PY
"$orbit_bin" status >"$test_root/runtime-threshold.txt"
grep -F "orbit up shop-inventory-api" "$test_root/runtime-threshold.txt"
if grep -F "orbit logs shop-order-api" "$test_root/runtime-threshold.txt" >/dev/null; then
  echo "runtime status recommends diagnosing a dependent before restoring its known root dependency" >&2
  exit 1
fi

dashboard_url="$(
  python3 -c '
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["data"]["url"])
' "$test_root/open-dashboard.json"
)"
curl --fail --silent --show-error "${dashboard_url%/}/api/graph" >"$test_root/runtime-graph.json"
python3 - "$test_root/runtime-graph.json" <<'PY'
import json
import sys

nodes = {node["name"]: node for node in json.load(open(sys.argv[1], encoding="utf-8"))["nodes"]}
assert nodes["shop-order-api"]["state"] == "degraded"
assert nodes["shop-order-api"]["blockedBy"] == "shop-inventory-api"
assert nodes["shop-order-api"]["stateReason"] == "dependency shop-inventory-api is stopped"
assert nodes["demo-shop"]["state"] == "degraded"
PY

"$orbit_bin" up shop-inventory-api --json >"$test_root/recover-inventory.json"
for _ in {1..20}; do
  "$orbit_bin" status --json >"$test_root/runtime-recovered.json"
  if python3 - "$test_root/runtime-recovered.json" <<'PY'
import json
import sys

resources = json.load(open(sys.argv[1], encoding="utf-8"))["data"]["resources"]
assert all(resource["state"] == "healthy" for resource in resources)
assert all("blocked_by" not in resource for resource in resources)
PY
  then
    break
  fi
  sleep 1
done

python3 - "$test_root/runtime-recovered.json" <<'PY'
import json
import sys

resources = json.load(open(sys.argv[1], encoding="utf-8"))["data"]["resources"]
assert all(resource["state"] == "healthy" for resource in resources)
PY

"$orbit_bin" down --json >"$test_root/down.json"
"$orbit_bin" daemon stop --json >"$test_root/control-stop.json"
"$orbit_bin" down --json >"$test_root/down-again.json"
python3 - "$test_root/down.json" "$test_root/down-again.json" <<'PY'
import json
import sys

down = json.load(open(sys.argv[1], encoding="utf-8"))
again = json.load(open(sys.argv[2], encoding="utf-8"))
assert down["ok"] is True
assert down["data"]["message"] == "Environment stopped. Orbit is ready for the next 'orbit up'."
assert again["ok"] is True
assert again["data"] == {
    "operation": "down",
    "message": "Environment is already stopped. Orbit is ready for the next 'orbit up'.",
    "requested_resources": [],
    "resources": [],
    "degraded_resources": [],
    "timed_out_resources": [],
}
assert again["recommended_actions"] == [{
    "command": "orbit up --json",
    "reason": "Start the environment when you are ready.",
    "destructive": False,
}]
PY

echo "README first five minutes completes a linked checkout and reports runtime dependency failures truthfully"
