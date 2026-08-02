#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

for guide in docs/local-first.md docs/local-first.zh-TW.md; do
  if ! grep -F "orbit up" "$repo_root/$guide" |
    grep -Ei "switch|切換" >/dev/null; then
    echo "$guide must explain that orbit up switches projects." >&2
    exit 1
  fi
done

if [ "${ORBIT_DOCS_ONLY:-}" = "1" ]; then
  echo "Project-switch guides preserve the one-command mental model"
  exit 0
fi

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
export ORBIT_HOME="$test_root/home"
export ORBIT_NAMESPACE="project-switch-$$"
export ORBIT_DASHBOARD_PORT="$((25000 + ($$ % 1000)))"
service_port="$((29000 + ($$ % 1000)))"

cleanup() {
  for project in project-b project-a; do
    if [ -d "$test_root/$project" ]; then
      (
        cd "$test_root/$project"
        "$orbit_bin" down --json >/dev/null 2>&1 || true
      )
    fi
  done
  "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap 'status=$?; cleanup; exit "$status"' EXIT

make_project() {
  project="$1"
  resource="$2"
  content="$3"
  mkdir -p "$test_root/$project"
  printf '<h1>%s</h1>\n' "$content" >"$test_root/$project/index.html"
  printf '%s\n' \
    'version: "3"' \
    'groups:' \
    '  app:' \
    '    enabled: true' \
    '    services:' \
    "      - $resource" \
    'services:' \
    "  $resource:" \
    '    type: python' \
    '    path: .' \
    "    command: python3 -m http.server $service_port" \
    "    url: http://localhost:$service_port" \
    '    ports:' \
    "      http: \"$service_port\"" \
    '    health_check:' \
    '      type: http' \
    '      path: /' \
    "      port: $service_port" \
    >"$test_root/$project/orbit.yaml"
}

make_project "project-a" "app-a" "project-a"
make_project "project-b" "app-b" "project-b"
mkdir -p "$test_root/candidate"
printf '%s\n' \
  'version: "3"' \
  'services:' \
  '  broken-app:' \
  '    type: shell' \
  '    path: .' \
  '    command: orbit-command-that-does-not-exist' \
  >"$test_root/candidate/orbit.yaml"

cd "$test_root/project-a"
"$orbit_bin" up --json >"$test_root/up-a.json"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "http://localhost:$service_port" >"$test_root/page-a.html"
grep -F "project-a" "$test_root/page-a.html" >/dev/null

cd "$test_root/project-b"
"$orbit_bin" status --json >"$test_root/status-b-before.json"
"$orbit_bin" doctor --json >"$test_root/doctor-b.json"
"$orbit_bin" inspect --json >"$test_root/inspect-b.json"
if "$orbit_bin" --config "$test_root/candidate/orbit.yaml" doctor --json \
  >"$test_root/doctor-candidate.json"; then
  echo "orbit doctor unexpectedly accepted a missing candidate executable." >&2
  exit 1
fi
if "$orbit_bin" --config "$test_root/candidate/orbit.yaml" up --yes --json \
  >"$test_root/up-candidate.json"; then
  echo "orbit up unexpectedly accepted an invalid candidate before switching." >&2
  exit 1
fi
if "$orbit_bin" down --json >"$test_root/down-b.json"; then
  echo "orbit down unexpectedly controlled the other project." >&2
  exit 1
fi
curl --fail --silent --show-error \
  "http://localhost:$service_port" >"$test_root/page-a-after-guard.html"
grep -F "project-a" "$test_root/page-a-after-guard.html" >/dev/null

if "$orbit_bin" up --json >"$test_root/up-b-refused.json"; then
  echo "orbit up --json unexpectedly switched projects without --yes." >&2
  exit 1
fi
if "$orbit_bin" up >"$test_root/up-b-noninteractive.txt" 2>&1; then
  echo "non-interactive orbit up unexpectedly switched projects without --yes." >&2
  exit 1
fi
curl --fail --silent --show-error \
  "http://localhost:$service_port" >"$test_root/page-a-after-refusal.html"
grep -F "project-a" "$test_root/page-a-after-refusal.html" >/dev/null

"$orbit_bin" up --yes --json >"$test_root/up-b.json"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "http://localhost:$service_port" >"$test_root/page-b.html"
grep -F "project-b" "$test_root/page-b.html" >/dev/null
"$orbit_bin" status --json >"$test_root/status-b-after.json"
cd "$test_root"
"$orbit_bin" daemon restart --json >"$test_root/restart-daemon-b.json"
"$orbit_bin" status --json >"$test_root/status-b-after-restart.json"
curl --fail --silent --show-error \
  "http://localhost:$ORBIT_DASHBOARD_PORT/api/envs" >"$test_root/envs-api-b.json"
cd "$test_root/project-b"
"$orbit_bin" inspect --json >"$test_root/inspect-b-after.json"
"$orbit_bin" doctor --json >"$test_root/doctor-b-after.json"
"$orbit_bin" logs app-b --json >"$test_root/logs-b-after.json"
"$orbit_bin" logs app-b >"$test_root/logs-b-after.txt"
for command in up down restart logs open; do
  if "$orbit_bin" "$command" appb --json >"$test_root/typo-$command.json"; then
    echo "orbit $command unexpectedly accepted a misspelled resource." >&2
    exit 1
  fi
done
if "$orbit_bin" logs appb >"$test_root/typo-human.txt" 2>&1; then
  echo "orbit logs unexpectedly accepted a misspelled resource." >&2
  exit 1
fi
"$orbit_bin" restart app-b --json >"$test_root/restart-b-after.json"
"$orbit_bin" down app-b --json >"$test_root/stop-b-after.json"
if "$orbit_bin" open app-b --json >"$test_root/open-stopped-b.json"; then
  echo "orbit open unexpectedly opened a stopped resource." >&2
  exit 1
fi
"$orbit_bin" up app-b --json >"$test_root/restore-b-after.json"
"$orbit_bin" down --group app --json >"$test_root/stop-group-b.json"
"$orbit_bin" up --group app --json >"$test_root/restore-group-b.json"

python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def read(name):
    return json.loads((root / name).read_text(encoding="utf-8"))

before = read("status-b-before.json")
doctor = read("doctor-b.json")
inspect = read("inspect-b.json")
candidate = read("doctor-candidate.json")
candidate_up = read("up-candidate.json")
guard = read("down-b.json")
refused = read("up-b-refused.json")
up = read("up-b.json")
after = read("status-b-after.json")
after_restart = read("status-b-after-restart.json")
restart_daemon = read("restart-daemon-b.json")
envs_api = read("envs-api-b.json")
inspect_after = read("inspect-b-after.json")
doctor_after = read("doctor-b-after.json")
logs_after = read("logs-b-after.json")
restart_after = read("restart-b-after.json")
stop_after = read("stop-b-after.json")
open_stopped = read("open-stopped-b.json")
restore_after = read("restore-b-after.json")
stop_group = read("stop-group-b.json")
restore_group = read("restore-group-b.json")

assert before["ok"] is True
environment = before["data"]["environment"]
assert environment["source"] == "project"
assert environment["selected_name"] == "project-b"
assert environment["context_switch_required"] is True
assert environment["running_name"] == "project-a"
assert before["data"]["daemon"]["context_mismatch"] is True
assert [(item["name"], item["state"]) for item in before["data"]["resources"]] == [
    ("app-b", "stopped")
]
assert [action["command"] for action in before["recommended_actions"]] == [
    "orbit up --json"
]

assert doctor["ok"] is True
assert [action["command"] for action in doctor["recommended_actions"]] == [
    "orbit up --json"
]

assert inspect["ok"] is True
assert inspect["data"]["environment"]["context_switch_required"] is True
assert inspect["data"]["environment"]["running_name"] == "project-a"
assert inspect["data"]["resources"]["stopped"] == ["app-b"]
assert inspect["data"]["risks"][0]["code"] == "project_context_inactive"
assert [action["command"] for action in inspect["recommended_actions"]] == [
    "orbit up --json"
]

assert candidate["ok"] is False
assert candidate["error"]["code"] == "checks_failed"
assert any(
    check["name"] == "Command (broken-app)"
    and check["status"] == "fail"
    and "orbit-command-that-does-not-exist" in check["message"]
    for check in candidate["data"]["checks"]
)
assert "daemon restart" not in json.dumps(candidate)
assert candidate_up["ok"] is False
assert candidate_up["error"]["code"] == "checks_failed"

assert guard["ok"] is False
assert guard["error"]["code"] == "project_context_inactive"
assert [action["command"] for action in guard["recommended_actions"]] == [
    "orbit up --json"
]

assert refused["ok"] is False
assert refused["error"]["code"] == "confirmation_required"
assert [action["command"] for action in refused["recommended_actions"]] == [
    "orbit up --yes --json"
]
noninteractive = (root / "up-b-noninteractive.txt").read_text(encoding="utf-8").lower()
assert "rerun with --yes" in noninteractive

assert up["ok"] is True
switched = up["data"]["context_switch"]
assert switched["from_name"] == "project-a"
assert switched["to_name"] == "project-b"
assert switched["stopped_resources"] == ["app-a"]
assert up["data"]["context"]["kind"] == "project"
assert up["data"]["context"]["display_name"] == "project-b"
assert pathlib.Path(up["data"]["context"]["project_root"]).resolve() == (root / "project-b").resolve()

assert after["ok"] is True
assert after["data"]["environment"].get("context_switch_required") is not True
assert after["data"]["daemon"].get("context_mismatch") is not True
assert [(item["name"], item["state"]) for item in after["data"]["resources"]] == [
    ("app-b", "healthy")
]
assert after["data"]["daemon"]["context"]["kind"] == "project"

assert restart_daemon["ok"] is True
assert after_restart["data"]["daemon"]["context"]["kind"] == "project"
assert after_restart["data"]["daemon"]["context"]["display_name"] == "project-b"
assert envs_api["context"]["kind"] == "project"
assert envs_api["context"]["display_name"] == "project-b"
assert pathlib.Path(envs_api["context"]["config_path"]).resolve() == (root / "project-b" / "orbit.yaml").resolve()

assert inspect_after["ok"] is True
assert inspect_after["data"]["readiness"]["state"] == "ready"
assert inspect_after["data"]["environment"]["selected_name"] == "project-b"
assert inspect_after["data"]["environment"]["daemon_env"] == "project-b"
assert inspect_after["data"]["risks"] == [], inspect_after["data"]["risks"]
assert inspect_after.get("recommended_actions", []) == []

assert doctor_after["ok"] is True
assert doctor_after.get("recommended_actions", []) == []

assert logs_after["ok"] is True
assert logs_after.get("recommended_actions", []) == []
assert "Next:" not in (root / "logs-b-after.txt").read_text(encoding="utf-8")

for command in ("up", "down", "restart", "logs", "open"):
    typo = read(f"typo-{command}.json")
    corrected = f"orbit {command} app-b --json"
    assert typo["ok"] is False
    assert typo["error"]["code"] == "unknown_resource"
    assert "did you mean app-b?" in typo["error"]["message"]
    assert typo["error"]["next_command"] == corrected
    assert [action["command"] for action in typo["recommended_actions"]] == [
        corrected
    ]

human_typo = (root / "typo-human.txt").read_text(encoding="utf-8")
assert "did you mean app-b?" in human_typo
assert "Next: orbit logs app-b" in human_typo

assert restart_after["ok"] is True
assert restart_after["data"]["resources"][0]["name"] == "app-b"
assert restart_after["data"]["resources"][0]["state"] == "healthy"
assert restart_after.get("recommended_actions", []) == []

assert stop_after["ok"] is True
assert stop_after["data"]["resources"][0]["state"] == "stopped"
assert stop_after.get("recommended_actions", []) == []

assert open_stopped["ok"] is False
assert "app-b is stopped" in open_stopped["error"]["message"]
assert [action["command"] for action in open_stopped["recommended_actions"]] == [
    "orbit up app-b --json"
]

assert restore_after["ok"] is True
assert restore_after["data"]["resources"][0]["state"] == "healthy"

assert stop_group["ok"] is True
assert stop_group["data"]["message"] == "Stopping group app (1 resource)."
assert [(item["name"], item["state"]) for item in stop_group["data"]["resources"]] == [
    ("app-b", "stopped")
]
assert stop_group.get("recommended_actions", []) == []

assert restore_group["ok"] is True
assert [(item["name"], item["state"]) for item in restore_group["data"]["resources"]] == [
    ("app-b", "healthy")
]
PY

"$orbit_bin" down --json >/dev/null
mkdir -p "$ORBIT_HOME/envs"
cp "$test_root/project-a/orbit.yaml" "$ORBIT_HOME/envs/quickstart.yaml"
"$orbit_bin" env use "$ORBIT_HOME/envs/quickstart.yaml" --json >/dev/null
cd "$test_root"
"$orbit_bin" status --json >"$test_root/status-outside-project.json"
"$orbit_bin" status >"$test_root/status-outside-project.txt"
"$orbit_bin" doctor --json >"$test_root/doctor-outside-project.json"
"$orbit_bin" doctor >"$test_root/doctor-outside-project.txt"
"$orbit_bin" logs app-b --json >"$test_root/logs-outside-project.json" 2>"$test_root/logs-outside-project.err"
if [ ! -s "$test_root/logs-outside-project.json" ]; then
  cat "$test_root/logs-outside-project.err" >&2
  echo "detached project logs returned no JSON." >&2
  exit 1
fi
if "$orbit_bin" --config "$ORBIT_HOME/envs/quickstart.yaml" status --json \
  >"$test_root/explicit-managed-status.json"; then
  echo "explicit managed config unexpectedly controlled the active project." >&2
  exit 1
fi
python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
status = json.loads((root / "status-outside-project.json").read_text(encoding="utf-8"))
doctor = json.loads((root / "doctor-outside-project.json").read_text(encoding="utf-8"))
logs = json.loads((root / "logs-outside-project.json").read_text(encoding="utf-8"))
explicit = json.loads((root / "explicit-managed-status.json").read_text(encoding="utf-8"))
assert status["ok"] is True
assert status["data"]["environment"]["source"] == "project"
assert status["data"]["environment"]["selected_name"] == "project-b"
assert status["data"]["daemon"]["detached_project"] is True
assert status["data"]["daemon"].get("context_mismatch") is not True
assert [action["command"] for action in status["recommended_actions"]] == [
    f"cd {root / 'project-b'} && orbit up --json"
]

human = (root / "status-outside-project.txt").read_text(encoding="utf-8")
assert "last project context" in human
assert "daemon restart" not in human
assert "quickstart.yaml" not in human

assert doctor["ok"] is True
environment_check = next(
    check for check in doctor["data"]["checks"] if check["name"] == "Daemon"
)
assert "project-b is selected and stopped" in environment_check["message"]
assert str(root / "project-b") in environment_check["message"]
doctor_human = (root / "doctor-outside-project.txt").read_text(encoding="utf-8")
assert "project-b is selected and stopped" in doctor_human
assert "daemon restart" not in doctor_human
assert "quickstart.yaml" not in doctor_human

assert logs["ok"] is True
assert logs["data"]["resource"] == "app-b"
assert "quickstart.yaml" not in json.dumps(logs)

assert explicit["ok"] is False
assert explicit["error"]["code"] == "env_mismatch"
PY
"$orbit_bin" down --json >"$test_root/down-outside-project.json"
cd "$test_root/project-a"
"$orbit_bin" doctor --json >"$test_root/doctor-after-down.json"
python3 - "$test_root/down-outside-project.json" <<'PY'
import json
import pathlib
import sys

down = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert down["ok"] is True
assert all(resource["state"] == "stopped" for resource in down["data"]["resources"])
assert down["data"]["context"]["kind"] == "project"
assert down["data"]["context"]["managed_selection"]["name"] == "quickstart"
assert down["data"]["context"]["managed_selection"]["active"] is False
PY
python3 - "$test_root/doctor-after-down.json" <<'PY'
import json
import pathlib
import sys

doctor = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert doctor["ok"] is True
environment = next(
    check for check in doctor["data"]["checks"] if check["name"] == "Daemon"
)
assert "project-b is selected and stopped" in environment["message"]
assert "project-b is running" not in environment["message"]
assert [action["command"] for action in doctor["recommended_actions"]] == [
    "orbit up --json"
]
PY
"$orbit_bin" daemon stop --json >/dev/null

cd "$test_root/project-a"
"$orbit_bin" --config "$test_root/project-a/orbit.yaml" up --json >"$test_root/explicit-up.json"
curl --fail --silent --show-error \
  "http://localhost:$ORBIT_DASHBOARD_PORT/api/envs" >"$test_root/explicit-envs-api.json"
python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
up = json.loads((root / "explicit-up.json").read_text(encoding="utf-8"))
envs = json.loads((root / "explicit-envs-api.json").read_text(encoding="utf-8"))
assert up["data"]["context"]["kind"] == "explicit"
assert envs["context"]["kind"] == "explicit"
assert pathlib.Path(envs["context"]["config_path"]).resolve() == (root / "project-a" / "orbit.yaml").resolve()
PY
"$orbit_bin" down --json >/dev/null
"$orbit_bin" daemon stop --json >/dev/null

echo "Project switching and detached context recovery keep one successful next action"
