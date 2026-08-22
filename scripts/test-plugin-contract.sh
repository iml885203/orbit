#!/usr/bin/env bash
set -euo pipefail

skill="plugins/orbit-agent/skills/orbit/SKILL.md"
workflows="plugins/orbit-agent/skills/orbit/references/workflows.md"
metadata="plugins/orbit-agent/skills/orbit/agents/openai.yaml"
manifest="plugins/orbit-agent/.codex-plugin/plugin.json"
marketplace=".agents/plugins/marketplace.json"

for stale in \
  "orbit service start" \
  "orbit service stop" \
  "orbit service restart" \
  "orbit daemon start" \
  "orbit db " \
  "orbit sqlserver publish --clean"
do
  if grep -F -- "$stale" "$skill" "$workflows" >/dev/null; then
    echo "plugin teaches unsupported or internal lifecycle command: $stale" >&2
    exit 1
  fi
done

for required in \
  "orbit inspect --json" \
  "orbit up <resource> --json" \
  "orbit restart <resource> --json" \
  "orbit down <resource> --json" \
  "orbit status --json" \
  "orbit instance list --json" \
  "orbit instance clean <name>" \
  "--instance <name>" \
  "orbit sqlserver diff" \
  "orbit sqlserver publish <database|project> --json" \
  "destructive: true"
do
  if ! grep -F -- "$required" "$skill" "$workflows" >/dev/null; then
    echo "plugin is missing the supported workflow command: $required" >&2
    exit 1
  fi
done

if ! grep -F 'default_prompt: "Use $orbit ' "$metadata" >/dev/null; then
  echo "plugin metadata default prompt must invoke the skill as \$orbit" >&2
  exit 1
fi

for installer in \
  "scripts/install.sh" \
  "scripts/install.ps1" \
  "orbit version --json"
do
  if ! grep -F -- "$installer" "$skill" >/dev/null; then
    echo "plugin is missing first-run installer guidance: $installer" >&2
    exit 1
  fi
done

node - "$manifest" "$marketplace" <<'NODE'
const fs = require('fs')
const [manifestPath, marketplacePath] = process.argv.slice(2)
const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
const marketplace = JSON.parse(fs.readFileSync(marketplacePath, 'utf8'))
const entry = marketplace.plugins?.find((plugin) => plugin.name === manifest.name)
if (!entry) throw new Error(`marketplace does not expose ${manifest.name}`)
if (entry.source?.path !== `./plugins/${manifest.name}`) throw new Error('marketplace plugin path does not match manifest name')
if (entry.policy?.installation !== 'AVAILABLE') throw new Error('marketplace plugin must be available')
if (!entry.policy?.authentication) throw new Error('marketplace plugin must declare authentication policy')
NODE

echo "plugin lifecycle guidance matches the public CLI"
