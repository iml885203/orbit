#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-quick}"
REPORT_DIR="/tmp/mini-shop-smoke-reports"
RUN_TS="$(date -u +%s)"
COMPACT_SUMMARY="$REPORT_DIR/compact-summary.json"
P1_SUMMARY="$REPORT_DIR/mini-summary.json"
P1_ALL_SUMMARY="$REPORT_DIR/all-summary.json"
TARGET_SUCCESS_MS=60000
MIN_ONBOARDING_SCORE_FOR_PR=70

usage() {
  cat <<USAGE
用法：
  bash docs/examples/mini-shop/scripts/release-check.sh [quick|full|all]

quick  - 僅跑 compact smoke（success + decline）
full   - 先跑 compact，接著跑 smoke-p1 mini（含 stock）
all    - 先跑 compact，接著跑 smoke-p1 all（mini + advanced）

快取目標：
  success first-run <= ${TARGET_SUCCESS_MS}ms 視為「1 分鐘可 demo」。

輸出：
  /tmp/mini-shop-smoke-reports/release-check-summary.json
USAGE
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

if [[ "$MODE" != "quick" && "$MODE" != "full" && "$MODE" != "all" ]]; then
  echo "不支援模式: $MODE" >&2
  usage
  exit 1
fi

log() {
  echo "[mini-shop-release-check] $*"
}

print_release_blurb() {
  local compact_suite="$compact_passed"
  local quick_path="$compact_scenario_success"
  local decline_path="$compact_scenario_decline"
  local compact_ms="$compact_first_run_ms"
  local decline_ms="$compact_decline_ms"
  local within_target="$compact_within_target"
  local score_value="$compact_onboarding_score"
  local score_label="$compact_onboarding_score_label"
  local p1_mini_status="skip"
  local p1_all_status="skip"
  local ready_for_release=false

  if [[ "$MODE" == "full" ]]; then
    p1_mini_status="$p1_passed"
    p1_all_status="skip"
  elif [[ "$MODE" == "all" ]]; then
    p1_mini_status="$p1_passed"
    p1_all_status="$p1_all_passed"
  fi

  if [[ "${compact_suite,,}" == "true" && "${within_target,,}" == "true" ]]; then
    if [[ "$MODE" == "quick" ]]; then
      ready_for_release=true
    elif [[ "$MODE" == "full" && "${p1_passed,,}" == "true" ]]; then
      ready_for_release=true
    elif [[ "$MODE" == "all" && "${p1_passed,,}" == "true" && "${p1_all_passed,,}" == "true" ]]; then
      ready_for_release=true
    fi
  fi

  echo
  echo "### mini-shop Release 交付摘要（$MODE）"
  echo "- compact suite: ${compact_suite}"
  echo "- success scenario: ${quick_path}"
  echo "- decline scenario: ${decline_path}"
  echo "- first_run_success_ms: ${compact_ms}"
  echo "- decline_ms: ${decline_ms}"
  echo "- first_run_within_60s: ${within_target}"
  echo "- smoke-p1 mini: ${p1_mini_status}"
  echo "- smoke-p1 all: ${p1_all_status}"
  echo "- ready_for_release: ${ready_for_release}"
  echo "- onboarding_score: ${score_value}/100（${score_label}）"

  if [[ "${compact_suite,,}" != "true" ]]; then
    echo "- 下一步：先修復 compact suite（service ready + success/decline 任一未通過）"
  elif [[ "${within_target,,}" != "true" ]]; then
    echo "- 下一步：優先把首次成功時間壓低到 ${TARGET_SUCCESS_MS}ms 內（新手門檻）"
  elif [[ "${decline_path,,}" != "true" ]]; then
    echo "- 下一步：補齊付款失敗情境可復現與回報"
  else
    echo "- 下一步：執行模式 full 或 all，擴大覆蓋面後可做下一輪 release"
  fi

  local pr_verdict="可提 PR"
  local pr_reason="符合 1.0 前新手交付門檻"
  if [[ "${compact_scenario_success,,}" != "true" || "${compact_scenario_decline,,}" != "true" || "${compact_within_target,,}" != "true" || "${compact_onboarding_score}" -lt "$MIN_ONBOARDING_SCORE_FOR_PR" ]]; then
    pr_verdict="建議暫緩提 PR"
    pr_reason="未達到 1.0 前門檻（success、decline、time target 或 onboarding_score）"
  fi

  echo "- PR 建議：${pr_verdict}"
  echo "- PR 理由：${pr_reason}"
}

run_compact() {
  log "執行 compact smoke（success + decline）"
  bash docs/examples/mini-shop/scripts/smoke-compact.sh all
}

run_p1() {
  local profile="$1"
  log "執行 smoke-p1: $profile"
  bash docs/examples/mini-shop/scripts/smoke-p1.sh "$profile"
}

compact_passed=false
compact_scenario_success=false
compact_scenario_decline=false
compact_first_run_ms=0
compact_within_target=false
compact_decline_ms=0
p1_passed=false
p1_all_passed=false
compact_onboarding_score=0
compact_onboarding_score_label="待評估"

if ! run_compact; then
  compact_passed=false
else
  compact_passed=true
fi

if [[ -f "$COMPACT_SUMMARY" ]]; then
  compact_passed=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
print('true' if payload.get('suite_passed') else 'false')
PY
)
  compact_scenario_success=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
success = payload.get('scenarios', {}).get('success', {}).get('passed', False)
print('true' if success else 'false')
PY
)
  compact_first_run_ms=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
ux = payload.get('ux_readiness') or {}
print(int(ux.get('first_run_success_ms', 0) or 0))
PY
)
  compact_within_target=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
ux = payload.get('ux_readiness') or {}
target = int(ux.get('first_run_target_ms', 60000) or 60000)
value = int(ux.get('first_run_success_ms', 0) or 0)
print('true' if (value and value <= target) else 'false')
PY
)
  compact_scenario_decline=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
decline = payload.get('scenarios', {}).get('decline', {}).get('passed', False)
print('true' if decline else 'false')
PY
)
  compact_decline_ms=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1]))
print(int((payload.get('scenarios', {}).get('decline') or {}).get('duration_ms', 0) or 0))
PY
)

  compact_onboarding_score=$(
python3 - "$COMPACT_SUMMARY" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1]))
score = 0
label = "需改進"

if payload.get("suite_passed"):
    score += 30
if payload.get("scenarios", {}).get("success", {}).get("passed"):
    score += 30
if payload.get("scenarios", {}).get("decline", {}).get("passed"):
    score += 20
if (payload.get("ux_readiness") or {}).get("first_run_within_target"):
    score += 20

if score >= 90:
    label = "優秀"
elif score >= 70:
    label = "穩定"
elif score >= 50:
    label = "可用"

print(score)
print(label)
PY
  )
  compact_onboarding_score="$(printf '%s' "$compact_onboarding_score" | awk 'NR==1 {print $1}')"
  compact_onboarding_score_label="$(printf '%s' "$compact_onboarding_score" | awk 'NR==2 {print $1}')"
fi

if [[ "$MODE" == "full" || "$MODE" == "all" ]]; then
  if run_p1 mini; then
    p1_passed=true
  else
    p1_passed=false
  fi
fi

if [[ "$MODE" == "all" ]]; then
  if run_p1 all; then
    p1_all_passed=true
  else
    p1_all_passed=false
  fi
fi

mkdir -p "$REPORT_DIR"
END_TS="$(date -u +%s)"
SUMMARY_PATH="/tmp/mini-shop-smoke-reports/release-check-summary.json"
BODY_PATH="/tmp/mini-shop-smoke-reports/release-check-body.md"
python3 - <<PY
import json
import os
from datetime import datetime, timezone

payload = {
    "run_mode": "$MODE",
    "started_at": int("$RUN_TS"),
    "ended_at": int("$END_TS"),
    "duration_seconds": int("$END_TS") - int("$RUN_TS"),
    "checks": {
        "compact": bool(str("$compact_passed").lower() == "true"),
        "p1_mini": bool(str("$p1_passed").lower() == "true") if "$MODE" in ("full", "all") else None,
        "p1_all": bool(str("$p1_all_passed").lower() == "true") if "$MODE" == "all" else None,
    },
    "reports": {
        "compact_summary": "$COMPACT_SUMMARY" if os.path.exists("$COMPACT_SUMMARY") else None,
        "mini_p1_summary": "$P1_SUMMARY" if os.path.exists("$P1_SUMMARY") else None,
        "all_p1_summary": "$P1_ALL_SUMMARY" if os.path.exists("$P1_ALL_SUMMARY") else None,
    },
    "repo": {
        "git": "$(git rev-parse HEAD 2>/dev/null || echo unknown)",
        "branch": "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)",
    },
    "ux_ready": {
        "demo_green_path_passed": bool(str("$compact_passed").lower() == "true"),
        "success_scenario_passed": bool(str("$compact_scenario_success").lower() == "true"),
        "decline_scenario_passed": bool(str("$compact_scenario_decline").lower() == "true"),
        "first_run_success_ms": int("$compact_first_run_ms"),
        "decline_duration_ms": int("$compact_decline_ms"),
        "first_run_target_ms": int("$TARGET_SUCCESS_MS"),
        "first_run_within_target": bool(str("$compact_within_target").lower() == "true"),
        "score_for_pr_ready": int("$compact_onboarding_score"),
        "onboarding_score": {
            "value": int("$compact_onboarding_score"),
            "label": "$compact_onboarding_score_label",
        },
        "ready_for_release": False,
    },
}

payload["ux_ready"]["ready_for_release"] = (
    payload["checks"]["compact"]
    and payload["ux_ready"]["first_run_within_target"]
    and (payload["checks"].get("p1_mini") is not False)
    and (payload["checks"].get("p1_all") is not False)
)

if "$MODE" == "quick":
    payload["checks"]["p1_mini"] = None
    payload["checks"]["p1_all"] = None

path = "/tmp/mini-shop-smoke-reports/release-check-summary.json"
with open(path, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
print(path)
PY

python3 - <<PY
from datetime import datetime, timezone
import json
import os

summary_path = "/tmp/mini-shop-smoke-reports/release-check-summary.json"
body_path = "/tmp/mini-shop-smoke-reports/release-check-body.md"
with open(summary_path, "r", encoding="utf-8") as fh:
    payload = json.load(fh)

checks = payload["checks"]
ux = payload["ux_ready"]
mode = payload.get("run_mode", "quick")
ended_at = datetime.fromtimestamp(payload.get("ended_at", 0), tz=timezone.utc)
duration = payload.get("duration_seconds", 0)

lines = [
    f"## mini-shop Release {mode}",
    f"- 時間：{ended_at:%Y-%m-%d %H:%M:%S} UTC",
    f"- 檢核耗時：{duration} 秒",
    "",
    "### 本輪交付結果",
    f"- compact suite：`{checks.get('compact')}`",
    f"- success scenario：`{ux.get('success_scenario_passed')}`",
    f"- decline scenario：`{ux.get('decline_scenario_passed')}`",
    f"- first_run_success_ms：`{ux.get('first_run_success_ms')}`",
    f"- decline_ms：`{ux.get('decline_duration_ms')}`",
    f"- first_run_within_60s：`{ux.get('first_run_within_target')}`",
    f"- onboarding_score：`{ux.get('onboarding_score', {}).get('value', 0)}/100`（{ux.get('onboarding_score', {}).get('label', '待評估')}）",
    f"- smoke-p1 mini：`{checks.get('p1_mini')}`",
    f"- smoke-p1 all：`{checks.get('p1_all')}`",
    f"- ready_for_release：`{ux.get('ready_for_release')}`",
    "",
    "### 1.0 前重點",
    "- 如果 `ready_for_release=true`，代表本輪可對外可見為「可 demo」與最小可交付條件達標。",
    "- `first_run_within_60s=true` 代表新手 60 秒進場目標達成，優先顯示給 release review。",
]

score_for_pr_ready = int(ux.get("score_for_pr_ready", 0))
pr_ready = False
pr_reason = "尚未達標"
if (
    checks.get("compact")
    and ux.get("success_scenario_passed")
    and ux.get("decline_scenario_passed")
    and ux.get("first_run_within_target")
    and score_for_pr_ready >= 70
):
    pr_ready = True
    pr_reason = "success + decline + 首次成功時間 + onboarding_score 都達門檻，可直接提 PR"

lines.extend([
    "",
    "### PR 交付門檻（可直接貼在 PR / release note）",
    f"- onboarding_score：`{score_for_pr_ready}`",
    f"- PR 準備度：`{pr_ready}`",
    f"- PR 備註：{pr_reason}",
])

with open(body_path, "w", encoding="utf-8") as fh:
    fh.write("\n".join(lines))
    fh.write("\n")

print(f"body: {body_path}")
PY

log "release check complete"
log "summary: $SUMMARY_PATH"
cat "$SUMMARY_PATH"
if [[ -f "$BODY_PATH" ]]; then
  echo
  echo "### mini-shop Release 可貼摘要（$BODY_PATH）"
  cat "$BODY_PATH"
fi
print_release_blurb

if [[ "${compact_scenario_success,,}" != "true" ]]; then
  log "compact success 情境未通過：請先確保成功流程可完成。"
  exit 1
fi

if [[ "${compact_within_target,,}" != "true" ]]; then
  log "compact success 超過 ${TARGET_SUCCESS_MS}ms：未達到 1.0 前 60 秒心理模型目標。"
  exit 1
fi

if [[ "${compact_passed,,}" != "true" ]]; then
  log "compact smoke 未通過：請先修正 mini-shop 基線。"
  exit 1
fi

if [[ "$MODE" == "full" && "${p1_passed,,}" != "true" ]]; then
  log "smoke-p1 mini 未通過：請先修正後再重跑 release check（建議 full）。"
  exit 1
fi

if [[ "$MODE" == "all" && "${p1_all_passed,,}" != "true" ]]; then
  log "smoke-p1 all 未通過：請先修正進階套件後再重跑 release check（建議 all）。"
  exit 1
fi

log "本次檢核已通過。"
