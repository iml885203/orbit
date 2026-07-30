#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"
example_config="$repo_root/docs/examples/local-first/orbit.yaml"

for guide in docs/local-first.md docs/local-first.zh-TW.md; do
  guide_path="$repo_root/$guide"

  for command in \
    "orbit doctor" \
    "orbit up" \
    "orbit open app" \
    "orbit logs app" \
    "orbit down"; do
    if ! grep -Fx "$command" "$guide_path" >/dev/null; then
      echo "$guide is missing the local-first command: $command" >&2
      exit 1
    fi
  done

  for concept in "orbit init" "~/.orbit" 'path: ${WORKSPACE_ROOT}'; do
    if ! grep -F "$concept" "$guide_path" >/dev/null; then
      echo "$guide does not explain the local-to-shared boundary: $concept" >&2
      exit 1
    fi
  done

  if grep -F "ORBIT_AUTO_PORT_" "$guide_path" >/dev/null; then
    echo "$guide exposes the legacy movable-port expression." >&2
    exit 1
  fi
done

if [ ! -f "$example_config" ]; then
  echo "Local-first example config not found at $example_config." >&2
  exit 1
fi
if grep -F "ORBIT_AUTO_PORT_" "$example_config" >/dev/null; then
  echo "Local-first example exposes the legacy movable-port expression." >&2
  exit 1
fi
for redundant_field in "type: python" "path: ."; do
  if grep -F "$redundant_field" "$example_config" >/dev/null; then
    echo "Local-first example teaches redundant service config: $redundant_field" >&2
    exit 1
  fi
done

if [ "${ORBIT_DOCS_ONLY:-}" = "1" ]; then
  echo "Local-first guides preserve the five-command trial and promotion path"
  exit 0
fi

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
local_home="$test_root/local-home"
shared_home="$test_root/shared-home"
local_namespace="local-first-$$"
shared_namespace="shared-first-$$"
export ORBIT_HOME="$local_home"
export ORBIT_NAMESPACE="$local_namespace"
export ORBIT_DASHBOARD_PORT="$((24000 + ($$ % 1000)))"

cleanup() {
  ORBIT_HOME="$shared_home" ORBIT_NAMESPACE="$shared_namespace" \
    "$orbit_bin" down --json >/dev/null 2>&1 || true
  ORBIT_HOME="$shared_home" ORBIT_NAMESPACE="$shared_namespace" \
    "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  ORBIT_HOME="$local_home" ORBIT_NAMESPACE="$local_namespace" \
    "$orbit_bin" -c "$test_root/project/orbit.yaml" down --json >/dev/null 2>&1 || true
  ORBIT_HOME="$local_home" ORBIT_NAMESPACE="$local_namespace" \
    "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  if [ -n "${port_guard_pid:-}" ]; then
    kill "$port_guard_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$test_root"
}
trap 'status=$?; cleanup; exit "$status"' EXIT

mkdir -p "$test_root/project"
cp "$example_config" "$test_root/project/orbit.yaml"
printf '<h1>Orbit local-first works</h1>\n' >"$test_root/project/index.html"
mkdir -p "$test_root/project/apps/web"
cd "$test_root/project/apps/web"

printf '%s\n' \
  'version: "3"' \
  'services:' \
  '  override:' \
  '    type: shell' \
  '    path: .' \
  '    command: python3 -m http.server 0' \
  >"$test_root/override.yaml"
"$orbit_bin" -c "$test_root/override.yaml" status --json \
  >"$test_root/override-status.json"
python3 - "$test_root/override-status.json" <<'PY'
import json
import pathlib
import sys

status = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert status["ok"] is True
assert [resource["name"] for resource in status["data"]["resources"]] == ["override"]
PY

printf '%s\n' \
  'version: "3"' \
  'services:' \
  '  api:' \
  '    command: python3 -m http.server 0' \
  '    depend_on: [database]' \
  >"$test_root/typo.yaml"
if "$orbit_bin" -c "$test_root/typo.yaml" doctor --json \
  >"$test_root/typo-doctor.json"; then
  echo "orbit doctor unexpectedly accepted a typo'd schema field." >&2
  exit 1
fi
"$orbit_bin" -c "$test_root/typo.yaml" inspect --json \
  >"$test_root/typo-inspect.json"
printf '%s\n' \
  'version: "3"' \
  'services:' \
  '  database:' \
  '    command: python3 -m http.server 0' \
  '  api:' \
  '    command: python3 -m http.server 0' \
  '    depends_on: [databse]' \
  >"$test_root/unknown-reference.yaml"
if "$orbit_bin" -c "$test_root/unknown-reference.yaml" doctor --json \
  >"$test_root/unknown-reference-doctor.json"; then
  echo "orbit doctor unexpectedly accepted an unknown dependency." >&2
  exit 1
fi
python3 - \
  "$test_root/typo-doctor.json" \
  "$test_root/typo-inspect.json" \
  "$test_root/unknown-reference-doctor.json" <<'PY'
import json
import pathlib
import sys

doctor = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert doctor["error"]["code"] == "checks_failed"
config_check = next(
    check for check in doctor["data"]["checks"] if check["name"] == "Config"
)
assert 'did you mean "depends_on"?' in config_check["message"]

inspect = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
assert inspect["data"]["readiness"]["state"] == "config_invalid"
assert 'did you mean "depends_on"?' in inspect["data"]["risks"][0]["message"]

reference = json.loads(pathlib.Path(sys.argv[3]).read_text(encoding="utf-8"))
reference_check = next(
    check for check in reference["data"]["checks"] if check["name"] == "Config"
)
assert 'did you mean "database"?' in reference_check["message"]
PY

printf '%s\n' \
  'version: "3"' \
  'containers:' \
  '  database:' \
  '    image: postgres:18-alpine' \
  '    ports:' \
  '      primary: "25432:5432"' \
  '      admin: "25433:5433"' \
  'services:' \
  '  api:' \
  '    command: python3 -m http.server 0' \
  '    depends_on: [database]' \
  >"$test_root/ambiguous-readiness.yaml"
"$orbit_bin" -c "$test_root/ambiguous-readiness.yaml" doctor --json \
  >"$test_root/ambiguous-readiness.json"
"$orbit_bin" -c "$test_root/ambiguous-readiness.yaml" inspect --json \
  >"$test_root/ambiguous-readiness-inspect.json"
python3 - \
  "$test_root/ambiguous-readiness.json" \
  "$test_root/ambiguous-readiness-inspect.json" <<'PY'
import json
import pathlib
import sys

doctor = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
checks = {check["name"]: check for check in doctor["data"]["checks"]}
readiness = checks["Readiness (database)"]
assert readiness["status"] == "warn"
assert readiness["hint"] == (
    "Add containers.database.health_check so dependents wait for a real readiness signal"
)

inspect = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
risks = {risk["code"]: risk for risk in inspect["data"]["risks"]}
assert risks["dependency_readiness_ambiguous"]["severity"] == "medium"
assert "containers.database.health_check" in risks["dependency_readiness_ambiguous"]["message"]
PY

python3 -c '
import pathlib
import socket
import sys
import time

listeners = []
for port in (26379, 28080):
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        listener.bind(("127.0.0.1", port))
    except OSError:
        listener.close()
    else:
        listener.listen()
        listeners.append(listener)
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
  echo "Timed out waiting for occupied-port guard." >&2
  exit 1
fi

"$orbit_bin" doctor --json >"$test_root/doctor.json"
if ! "$orbit_bin" up --json \
  >"$test_root/up.json" 2>"$test_root/up.stderr"; then
  echo "orbit up failed during local-first adoption." >&2
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
PATH="$browser_stub_dir:$PATH" \
  "$orbit_bin" open app --json >"$test_root/open.json"

app_url="$(
  python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def read(name):
    return json.loads((root / name).read_text(encoding="utf-8"))

doctor = read("doctor.json")
up = read("up.json")
status = read("status.json")
opened = read("open.json")

assert doctor["ok"] is True
assert "Git" not in {check["name"] for check in doctor["data"]["checks"]}
assert up["ok"] is True
assert status["ok"] is True
resources = {resource["name"]: resource for resource in status["data"]["resources"]}
assert set(resources) == {"app", "redis"}
assert all(resource["state"] == "healthy" for resource in resources.values())
assert resources["app"]["ports"]["http"] != 28080
assert resources["redis"]["ports"]["redis"] != 26379
environment = status["data"]["environment"]
assert environment["source"] == "project"
assert pathlib.Path(environment["selected_path"]) == root / "project" / "orbit.yaml"
app_url = resources["app"]["url"]
assert opened["data"] == {
    "url": app_url,
    "target": "service",
    "service": "app",
    "opened": True,
}
print(app_url)
PY
)"

curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "$app_url" >"$test_root/index.html"
grep -F "Orbit local-first works" "$test_root/index.html" >/dev/null

"$orbit_bin" logs app --json >"$test_root/logs.json"
python3 - "$test_root/logs.json" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert payload["ok"] is True
assert any("GET /" in line for line in payload["data"]["lines"])
PY

"$orbit_bin" down --json >"$test_root/down.json"
"$orbit_bin" status --json >"$test_root/stopped.json"
python3 - "$test_root" "$ORBIT_HOME" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
orbit_home = pathlib.Path(sys.argv[2])
down = json.loads((root / "down.json").read_text(encoding="utf-8"))
stopped = json.loads((root / "stopped.json").read_text(encoding="utf-8"))

assert down["ok"] is True
assert stopped["ok"] is True
assert all(
    resource["state"] == "stopped"
    for resource in stopped["data"]["resources"]
)
assert not (orbit_home / "settings.json").exists()
assert not (orbit_home / "envs").exists()
PY

"$orbit_bin" daemon stop --json >/dev/null

git -C "$test_root/project" init --quiet
git -C "$test_root/project" config user.name "Orbit Acceptance"
git -C "$test_root/project" config user.email "acceptance@orbit.invalid"

mkdir -p "$test_root/team-env/envs"
awk '{
  print
  if ($0 == "  app:") {
    print "    path: ${WORKSPACE_ROOT}"
  }
}' \
  "$example_config" >"$test_root/team-env/envs/dev.yaml"
git -C "$test_root/team-env" init --quiet
git -C "$test_root/team-env" config user.name "Orbit Acceptance"
git -C "$test_root/team-env" config user.email "acceptance@orbit.invalid"
git -C "$test_root/team-env" add envs/dev.yaml
git -C "$test_root/team-env" commit --quiet -m "test: add shared environment"

rm "$test_root/project/orbit.yaml"
cd "$test_root/project"
export ORBIT_HOME="$shared_home"
export ORBIT_NAMESPACE="$shared_namespace"
printf '\n' | "$orbit_bin" init \
  --env-repo "file://$test_root/team-env" \
  --env dev >"$test_root/shared-init.txt"
grep -F \
  "Project checkout or workspace root (absolute path) [$test_root/project]" \
  "$test_root/shared-init.txt" >/dev/null
grep -F "Project workspace: $test_root/project" \
  "$test_root/shared-init.txt" >/dev/null

"$orbit_bin" up --json >"$test_root/shared-up.json"
"$orbit_bin" status --json >"$test_root/shared-status.json"
shared_url="$(
  python3 - "$test_root" "$ORBIT_HOME" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
orbit_home = pathlib.Path(sys.argv[2])
up = json.loads((root / "shared-up.json").read_text(encoding="utf-8"))
status = json.loads((root / "shared-status.json").read_text(encoding="utf-8"))

assert up["ok"] is True
assert status["ok"] is True
environment = status["data"]["environment"]
assert environment["state"] == "selected"
assert environment["selected_name"] == "dev"
assert pathlib.Path(environment["selected_path"]) == orbit_home / "envs" / "dev.yaml"
resources = {resource["name"]: resource for resource in status["data"]["resources"]}
assert set(resources) == {"app", "redis"}
assert all(resource["state"] == "healthy" for resource in resources.values())
assert (orbit_home / "settings.json").is_file()
print(resources["app"]["url"])
PY
)"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "$shared_url" >"$test_root/shared-index.html"
grep -F "Orbit local-first works" "$test_root/shared-index.html" >/dev/null
"$orbit_bin" down --json >"$test_root/shared-down.json"

echo "Local-first trial and shared promotion work with occupied preferred ports"
