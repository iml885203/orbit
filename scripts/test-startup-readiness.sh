#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
export ORBIT_HOME="$test_root/home"
export ORBIT_NAMESPACE="startup-readiness-$$"
export ORBIT_DASHBOARD_PORT="$((27000 + ($$ % 1000)))"
api_port="$((30000 + ($$ % 1000)))"
silent_port="$((31000 + ($$ % 1000)))"

cleanup() {
  "$orbit_bin" down --json >/dev/null 2>&1 || true
  "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap 'status=$?; cleanup; exit "$status"' EXIT

mkdir -p "$test_root/delayed"
cat >"$test_root/delayed/api.py" <<'PY'
import http.server
import os
import time

time.sleep(0.6)
http.server.ThreadingHTTPServer(
    ("127.0.0.1", int(os.environ["PORT"])),
    http.server.SimpleHTTPRequestHandler,
).serve_forever()
PY
cat >"$test_root/delayed/client.py" <<'PY'
import os
import time
import urllib.request

with urllib.request.urlopen(os.environ["API_URL"], timeout=2) as response:
    response.read()
print("dependency request succeeded", flush=True)
time.sleep(600)
PY
cat >"$test_root/delayed/orbit.yaml" <<YAML
version: "3"
settings:
  health_check_interval: 100ms
services:
  api:
    type: python
    kind: frontend
    path: .
    command: python3 api.py
    ports:
      http:
        preferred: $api_port
  client:
    type: python
    path: .
    command: python3 client.py
    depends_on: [api]
YAML

cd "$test_root/delayed"
if ! "$orbit_bin" up --json >"$test_root/delayed-up.json" 2>"$test_root/delayed-up.stderr"; then
  echo "orbit up failed during startup readiness." >&2
  if [ -s "$test_root/delayed-up.json" ]; then
    echo "orbit up JSON:" >&2
    sed 's/^/  /' "$test_root/delayed-up.json" >&2
  fi
  if [ -s "$test_root/delayed-up.stderr" ]; then
    echo "orbit up stderr:" >&2
    sed 's/^/  /' "$test_root/delayed-up.stderr" >&2
  fi
  echo "orbit status after the failure:" >&2
  "$orbit_bin" status --json >&2 || true
  if [ -s "$ORBIT_HOME/daemon.log" ]; then
    echo "daemon log tail:" >&2
    tail -n 80 "$ORBIT_HOME/daemon.log" | sed 's/^/  /' >&2
  fi
  exit 1
fi
"$orbit_bin" logs client --json >"$test_root/client-logs.json"
"$orbit_bin" status --json >"$test_root/delayed-status.json"
python3 - "$test_root" <<'PY'
import json
import pathlib
import sys
import urllib.request

root = pathlib.Path(sys.argv[1])
up = json.loads((root / "delayed-up.json").read_text(encoding="utf-8"))
status = json.loads((root / "delayed-status.json").read_text(encoding="utf-8"))
logs = json.loads((root / "client-logs.json").read_text(encoding="utf-8"))
assert up["ok"] is True
resources = {item["name"]: item for item in status["data"]["resources"]}
assert resources["api"]["state"] == "healthy"
assert resources["client"]["state"] == "healthy"
assert any("dependency request succeeded" in line for line in logs["data"]["lines"])
with urllib.request.urlopen(resources["api"]["url"], timeout=2) as response:
    assert response.status == 200
PY
"$orbit_bin" down --json >/dev/null

mkdir -p "$test_root/silent"
cat >"$test_root/silent/sleep.py" <<'PY'
import time

time.sleep(600)
PY
cat >"$test_root/silent/orbit.yaml" <<YAML
version: "3"
settings:
  health_check_interval: 100ms
services:
  app:
    type: python
    kind: frontend
    path: .
    command: python3 sleep.py
    ports:
      http:
        preferred: $silent_port
YAML

cd "$test_root/silent"
if "$orbit_bin" up --timeout 3s --json >"$test_root/silent-up.json"; then
  echo "orbit up declared a non-listening frontend healthy." >&2
  exit 1
fi
"$orbit_bin" status --json >"$test_root/silent-status.json"
"$orbit_bin" logs app --json >"$test_root/silent-logs.json"
python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
up = json.loads((root / "silent-up.json").read_text(encoding="utf-8"))
status = json.loads((root / "silent-status.json").read_text(encoding="utf-8"))
logs = json.loads((root / "silent-logs.json").read_text(encoding="utf-8"))
assert up["ok"] is False
assert all(not action["command"].startswith("orbit open") for action in up.get("recommended_actions", []))
app = next(item for item in status["data"]["resources"] if item["name"] == "app")
assert app["state"] == "degraded"
assert app["failure_kind"] == "health"
assert [action["command"] for action in logs["recommended_actions"]] == ["orbit status --json"]
PY
"$orbit_bin" status --json >"$test_root/emitted-status.json"
"$orbit_bin" down --json >/dev/null
"$orbit_bin" daemon stop --json >/dev/null

mkdir -p "$test_root/invalid"
cat >"$test_root/invalid/sleep.py" <<'PY'
import time

time.sleep(600)
PY
cat >"$test_root/invalid/orbit.yaml" <<'YAML'
version: "3"
services:
  missing-command:
    type: python
    path: .
    command: orbit-tool-that-does-not-exist serve
  missing-pre-start:
    type: python
    path: .
    command: python3 sleep.py
    pre_start:
      - ./scripts/prepare
YAML

cd "$test_root/invalid"
if "$orbit_bin" doctor --json >"$test_root/invalid-doctor.json"; then
  echo "orbit doctor accepted missing startup executables." >&2
  exit 1
fi
if "$orbit_bin" up --json >"$test_root/invalid-up.json"; then
  echo "orbit up started a deterministically invalid environment." >&2
  exit 1
fi
python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
doctor = json.loads((root / "invalid-doctor.json").read_text(encoding="utf-8"))
up = json.loads((root / "invalid-up.json").read_text(encoding="utf-8"))
checks = doctor["data"]["checks"]
failures = {check["name"]: check for check in checks if check["status"] == "fail"}
assert "executable not found: orbit-tool-that-does-not-exist" in failures["Command (missing-command)"]["message"]
assert "update services.missing-command.command" in failures["Command (missing-command)"]["hint"]
assert "scripts/prepare" in failures["Pre-start (missing-pre-start #1)"]["message"]
assert "update services.missing-pre-start.pre_start entry 1" in failures["Pre-start (missing-pre-start #1)"]["hint"]
serialized = json.dumps(up)
assert "orbit restart" not in serialized
assert "orbit logs" not in serialized
PY

echo "Startup success waits for real readiness and deterministic setup failures stop before launch"
