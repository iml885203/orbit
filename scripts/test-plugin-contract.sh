#!/usr/bin/env bash
set -euo pipefail

skill="plugins/orbit/skills/orbit/SKILL.md"
workflows="plugins/orbit/skills/orbit/references/workflows.md"
aspire_migration="plugins/orbit/skills/orbit/references/aspire-migration.md"
metadata="plugins/orbit/skills/orbit/agents/openai.yaml"
manifest="plugins/orbit/.codex-plugin/plugin.json"
claude_manifest="plugins/orbit/.claude-plugin/plugin.json"
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
  "aspire do publish-manifest" \
  "Do not silently wrap" \
  'receives `<DEPENDENCY_NAME>_URL`' \
  "isolated local resource" \
  "Treat it as public repository data" \
  "share a central artifacts directory" \
  "preserve each project's working directory" \
  "protocol-correct probe" \
  "never invoke" \
  "do not infer a product gap from the manifest" \
  "not a reason to hand environment" \
  "concrete required behavior" \
  'Get approval before creating or changing `orbit.yaml`' \
  "without Aspire"
do
  if ! grep -F -- "$required" "$skill" "$aspire_migration" >/dev/null; then
    echo "plugin is missing the Aspire migration boundary: $required" >&2
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

node - "$manifest" "$claude_manifest" <<'NODE'
const fs = require('fs')
const [codexPath, claudePath] = process.argv.slice(2)
const codexVersion = JSON.parse(fs.readFileSync(codexPath, 'utf8')).version
const claudeVersion = JSON.parse(fs.readFileSync(claudePath, 'utf8')).version
if (codexVersion !== claudeVersion) throw new Error('Codex and Claude plugin versions must match')
if (!/^20\d{2}\.(?:[1-9]|1[0-2])\.[1-9]\d*$/.test(codexVersion)) {
  throw new Error(`plugin version must use YEAR.MONTH.RELEASE calendar versioning: ${codexVersion}`)
}
NODE

echo "plugin lifecycle guidance matches the public CLI"
