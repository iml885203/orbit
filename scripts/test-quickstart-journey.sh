#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

for readme in README.md README.zh-TW.md; do
  demo_section="$(
    awk '
      /^## (Try Orbit|試玩 Orbit)$/ { capture = 1; next }
      capture && /^## / { exit }
      capture { print }
    ' "$repo_root/$readme"
  )"

  for requirement in Git Docker "Python 3"; do
    if ! grep -F "$requirement" <<<"$demo_section" >/dev/null; then
      echo "$readme demo section must name $requirement." >&2
      exit 1
    fi
  done

  for command in \
    "git clone https://github.com/iml885203/orbit-demo.git" \
    "cd orbit-demo" \
    "orbit up" \
    "orbit status" \
    "orbit open demo-shop"; do
    if ! grep -Fx "$command" <<<"$demo_section" >/dev/null; then
      echo "$readme demo section is missing: $command" >&2
      exit 1
    fi
  done

  if ! grep -F '`orbit down`' <<<"$demo_section" >/dev/null; then
    echo "$readme demo section must tell the user how to stop the demo." >&2
    exit 1
  fi

  if grep -E 'orbit .*docs/examples/|orbit .*README' <<<"$demo_section" >/dev/null; then
    echo "$readme demo section depends on a source checkout." >&2
    exit 1
  fi
done

if [ "${ORBIT_DOCS_ONLY:-}" = "1" ]; then
  echo "README demo section matches the clone-based quickstart"
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
for command in daemon doctor down env init logs open restart service settings status switch tracing uninstall up update version; do
  grep -E "^[[:space:]]+$command[[:space:]]" "$test_root/root-help.txt" >/dev/null
done
grep -F "orbit inspect --json" "$test_root/root-help.txt" >/dev/null
grep -F "https://orbit.dotw.me/docs/agent-cli" "$test_root/root-help.txt" >/dev/null
if grep -E "^[[:space:]]+inspect[[:space:]]" "$test_root/root-help.txt" >/dev/null; then
  echo "agent-only inspect command appeared in the human command list instead of its dedicated signpost." >&2
  exit 1
fi
"$orbit_bin" help inspect >"$test_root/inspect-help.txt"
grep -F "machine-readable runtime readiness" "$test_root/inspect-help.txt" >/dev/null
for command in apply info list sync; do
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

"$orbit_bin" down --json >"$test_root/down-before-setup.json"
if "$orbit_bin" up --grop mini-shop --json >"$test_root/flag-typo.json"; then
  echo "unknown flag unexpectedly succeeded." >&2
  exit 1
fi
python3 - "$test_root/down-before-setup.json" "$test_root/flag-typo.json" <<'PY'
import json
import sys

result = json.load(open(sys.argv[1], encoding="utf-8"))
flag_typo = json.load(open(sys.argv[2], encoding="utf-8"))
assert result["ok"] is True
assert result["data"]["message"] == "Nothing is running because Orbit is not set up yet."
assert result["recommended_actions"] == [{
    "command": "orbit init --yes --json",
    "reason": "Set up Orbit before starting an environment.",
    "destructive": False,
}]
assert flag_typo["error"] == {
    "code": "invalid_argument",
    "message": "unknown flag: --grop (did you mean --group?)",
    "hint": "Retry with the closest supported flag.",
    "retryable": False,
    "next_command": "orbit up --group mini-shop --json",
}
assert flag_typo["recommended_actions"] == [{
    "command": "orbit up --group mini-shop --json",
    "reason": "Retry with the supported flag --group.",
    "destructive": False,
}]
PY

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

mkdir -p "$test_root/empty-directory/envs"
cat >"$test_root/empty-directory/envs/trap.yaml" <<'YAML'
version: "3"
services:
  trap:
    type: python
    path: /path/that/must/not/be-used
    command: python3 trap.py
YAML
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


"$orbit_bin" init --yes | tee "$test_root/init.txt"
for expected in "Step 1: Quickstart" "Preparing the Orbit demo" "Demo environment ready"; do
  if ! grep -F "$expected" "$test_root/init.txt" >/dev/null; then
    echo "default init did not present the value-first quickstart: $expected" >&2
    cat "$test_root/init.txt" >&2
    exit 1
  fi
done
if grep -F "trap" "$test_root/init.txt" >/dev/null; then
  echo "default init was hijacked by an incidental cwd/envs directory." >&2
  cat "$test_root/init.txt" >&2
  exit 1
fi
if grep -E 'Environment source|https://github.com/|@[[:space:]]+v[0-9]|/envs' \
  "$test_root/init.txt" >/dev/null; then
  echo "default init exposed environment-repository mechanics." >&2
  cat "$test_root/init.txt" >&2
  exit 1
fi
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
import pathlib, sys
pathlib.Path(sys.argv[1]).touch()
time.sleep(600)
' "$test_root/ports-ready" &
port_guard_pid=$!
for _ in $(seq 1 100); do
  if [ -f "$test_root/ports-ready" ]; then
    break
  fi
  sleep 0.05
done
if [ ! -f "$test_root/ports-ready" ]; then
  echo "Timed out waiting for the port guard." >&2
  exit 1
fi

# Declared ports are the contract: while the guard owns them, up must
# refuse loudly with the failed checks instead of starting anything.
if "$orbit_bin" up --json >"$test_root/up-conflict.json" 2>/dev/null; then
  echo "orbit up started while the demo's declared ports were owned by another process." >&2
  cat "$test_root/up-conflict.json" >&2
  exit 1
fi
python3 - "$test_root/up-conflict.json" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
assert payload["ok"] is False
message = payload["error"]["message"]
assert any(port in message for port in ("26379", "28080", "28101", "28102", "28103")), message
PY

kill "$port_guard_pid" >/dev/null 2>&1 || true
wait "$port_guard_pid" 2>/dev/null || true
port_guard_pid=""
for _ in $(seq 1 100); do
  if ! python3 -c 'import socket; socket.create_connection(("127.0.0.1", 26379), 0.2)' 2>/dev/null; then
    break
  fi
  sleep 0.1
done

if ! "$orbit_bin" up --json >"$test_root/up.json" 2>"$test_root/up.stderr"; then
  echo "orbit up failed during the quickstart journey." >&2
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
if PATH="$browser_stub_dir:$PATH" "$orbit_bin" open redis --json >"$test_root/open-infra.json"; then
  echo "orbit open unexpectedly treated infrastructure as an application." >&2
  exit 1
fi
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
assert resources["redis"]["ports"]["redis"] == 26379
assert resources["demo-shop"]["ports"]["http"] == 28080
assert resources["shop-catalog-api"]["ports"]["http"] == 28101
assert resources["shop-inventory-api"]["ports"]["http"] == 28102
assert resources["shop-order-api"]["ports"]["http"] == 28103
assert all(resource["state"] == "healthy" for resource in resources.values())
print(resources["demo-shop"]["url"])
' "$test_root/status.json"
)"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "${demo_url%/}/health" >"$test_root/health.json"

python3 - \
  "$test_root/status.json" \
  "$ORBIT_HOME/sources/default/current/envs/seeds/mini-shop/smoke.py" \
  "$orbit_bin" \
  >"$test_root/mini-shop-smoke.txt" <<'PY'
import json
import os
import pathlib
import subprocess
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))["data"]
resources = {resource["name"]: resource for resource in payload["resources"]}
service_url_variables = {
    "demo-shop": "DEMO_SHOP_URL",
    "shop-catalog-api": "SHOP_CATALOG_API_URL",
    "shop-inventory-api": "SHOP_INVENTORY_API_URL",
    "shop-order-api": "SHOP_ORDER_API_URL",
}
smoke_environment = os.environ.copy()
for service, variable in service_url_variables.items():
    smoke_environment[variable] = resources[service]["url"]
smoke_environment["PATH"] = os.pathsep.join(
    filter(None, (str(pathlib.Path(sys.argv[3]).parent), smoke_environment.get("PATH")))
)
subprocess.run([sys.executable, sys.argv[2]], check=True, env=smoke_environment)
PY

expected_demo_ref="$(
  sed -n 's/.*EnvRepoRef: "\(v[0-9][^"]*\)".*/\1/p' "$repo_root/cmd/orbit/extensions.go"
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
infrastructure = read("open-infra.json")

assert up["ok"] is True
assert status["ok"] is True
assert env_list["ok"] is True
assert version["ok"] is True
assert version["schema_version"] == "orbit.cli.v1"
assert version["data"]["version"]
assert health["ok"] is True
status_sources = status["data"]["environment"]["sources"]
list_sources = env_list["data"]["environment"]["sources"]
status_source = status_sources[0]
list_source = list_sources[0]
assert status_source == list_source
assert status_source["location"] == "https://github.com/iml885203/orbit-demo.git"
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
assert infrastructure["ok"] is False
assert infrastructure["error"]["code"] == "not_configured"
assert infrastructure["error"]["message"] == "redis does not expose an application URL"
assert [action["command"] for action in infrastructure["recommended_actions"]] == [
    "orbit open --json"
]
PY

canonical_english_handoff="https://github.com/iml885203/orbit/blob/main/docs/local-first.md"
handoff_file="$ORBIT_HOME/sources/default/current/envs/seeds/mini-shop/index.html"
if ! grep -F "$canonical_english_handoff" "$handoff_file" >/dev/null; then
  echo "demo handoff in $handoff_file does not point to the canonical Orbit guide: $canonical_english_handoff" >&2
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
  "oversell was rejected with stock and orders unchanged; restock restored full stock" \
  "$test_root/mini-shop-smoke.txt"

"$orbit_bin" down shop-inventory-api --json >"$test_root/down-inventory.json"
python3 - "$test_root/down-inventory.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
resources = {resource["name"]: resource for resource in payload["data"]["resources"]}
assert payload["ok"] is True
assert set(resources) == {"shop-inventory-api", "shop-order-api", "demo-shop"}
assert resources["shop-inventory-api"]["state"] == "stopped"
assert resources["shop-order-api"]["state"] == "degraded"
assert resources["shop-order-api"]["blocked_by"] == "shop-inventory-api"
assert resources["demo-shop"]["state"] == "degraded"
assert set(payload["data"]["degraded_resources"]) == {"shop-order-api", "demo-shop"}
assert "now need a stopped dependency" in payload["data"]["message"]
assert payload["recommended_actions"] == [{
    "command": "orbit up shop-inventory-api --json",
    "reason": "Restore shop-inventory-api, which now blocks demo-shop, shop-order-api.",
    "destructive": False,
}]
PY
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
if "$orbit_bin" down demo-shp --json >"$test_root/offline-resource-typo.json"; then
  echo "offline down accepted an unknown resource." >&2
  exit 1
fi
if "$orbit_bin" down --group mini-shp --json >"$test_root/offline-group-typo.json"; then
  echo "offline down accepted an unknown group." >&2
  exit 1
fi
for command in up restart logs open; do
  if "$orbit_bin" "$command" demo-shp --json >"$test_root/offline-$command-typo.json"; then
    echo "offline $command accepted an unknown resource." >&2
    exit 1
  fi
done
python3 - \
  "$test_root/down.json" \
  "$test_root/down-again.json" \
  "$test_root/offline-resource-typo.json" \
  "$test_root/offline-group-typo.json" \
  "$test_root/offline-up-typo.json" \
  "$test_root/offline-restart-typo.json" \
  "$test_root/offline-logs-typo.json" \
  "$test_root/offline-open-typo.json" <<'PY'
import json
import sys

down = json.load(open(sys.argv[1], encoding="utf-8"))
again = json.load(open(sys.argv[2], encoding="utf-8"))
resource_typo = json.load(open(sys.argv[3], encoding="utf-8"))
group_typo = json.load(open(sys.argv[4], encoding="utf-8"))
up_typo = json.load(open(sys.argv[5], encoding="utf-8"))
restart_typo = json.load(open(sys.argv[6], encoding="utf-8"))
logs_typo = json.load(open(sys.argv[7], encoding="utf-8"))
open_typo = json.load(open(sys.argv[8], encoding="utf-8"))
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
assert resource_typo["error"]["code"] == "unknown_resource"
assert resource_typo["error"]["next_command"] == "orbit down demo-shop --json"
assert resource_typo["recommended_actions"][0]["command"] == "orbit down demo-shop --json"
assert group_typo["error"]["code"] == "invalid_argument"
assert group_typo["error"]["next_command"] == "orbit down --group mini-shop --json"
assert group_typo["recommended_actions"][0]["command"] == "orbit down --group mini-shop --json"
for command, result in [
    ("up", up_typo),
    ("restart", restart_typo),
    ("logs", logs_typo),
    ("open", open_typo),
]:
    expected = f"orbit {command} demo-shop --json"
    assert result["error"]["code"] == "unknown_resource"
    assert result["error"]["next_command"] == expected
    assert result["recommended_actions"][0]["command"] == expected
PY

echo "README quickstart journey completes a linked checkout and reports runtime dependency failures truthfully"
