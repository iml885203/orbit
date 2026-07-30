#!/usr/bin/env bash
set -euo pipefail

skill="plugins/orbit-agent/skills/orbit/SKILL.md"
workflows="plugins/orbit-agent/skills/orbit/references/workflows.md"
metadata="plugins/orbit-agent/skills/orbit/agents/openai.yaml"

for stale in \
  "orbit service start" \
  "orbit service stop" \
  "orbit service restart" \
  "orbit daemon start" \
  "orbit db publish --clean"
do
  if grep -F "$stale" "$skill" "$workflows" >/dev/null; then
    echo "plugin teaches unsupported or internal lifecycle command: $stale" >&2
    exit 1
  fi
done

for required in \
  "orbit inspect --json" \
  "orbit up <resource> --json" \
  "orbit restart <resource> --json" \
  "orbit down <resource> --json" \
  "orbit status --json"
do
  if ! grep -F "$required" "$skill" "$workflows" >/dev/null; then
    echo "plugin is missing the supported workflow command: $required" >&2
    exit 1
  fi
done

if ! grep -F 'default_prompt: "Use $orbit ' "$metadata" >/dev/null; then
  echo "plugin metadata default prompt must invoke the skill as \$orbit" >&2
  exit 1
fi

echo "plugin lifecycle guidance matches the public CLI"
