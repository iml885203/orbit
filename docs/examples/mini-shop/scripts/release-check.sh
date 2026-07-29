#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-quick}"
REPORT_DIR="/tmp/mini-shop-smoke-reports"
RUN_TS="$(date -u +%s)"
COMPACT_SUMMARY="$REPORT_DIR/compact-summary.json"
P1_SUMMARY="$REPORT_DIR/mini-summary.json"
P1_ALL_SUMMARY="$REPORT_DIR/all-summary.json"

usage() {
  cat <<'USAGE'
用法：
  bash docs/examples/mini-shop/scripts/release-check.sh [quick|full|all]

quick  - 僅跑 compact smoke（success + decline）
full   - 先跑 compact，接著跑 smoke-p1 mini（含 stock）
all    - 先跑 compact，接著跑 smoke-p1 all（mini + advanced）

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
p1_passed=false
p1_all_passed=false

if ! run_compact; then
  compact_passed=false
else
  compact_passed=true
fi

if [[ -f "$COMPACT_SUMMARY" ]]; then
  compact_passed=$(python3 - "$COMPACT_SUMMARY" <<'PY'
import json,sys
payload=json.load(open(sys.argv[1]))
print('true' if payload.get('suite_passed') else 'false')
PY
)
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
python3 - <<PY
import json
import os

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
        "ready_for_release": False,
    },
}

payload["ux_ready"]["ready_for_release"] = (
    payload["checks"]["compact"]
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

SUMMARY_PATH="/tmp/mini-shop-smoke-reports/release-check-summary.json"
log "release check complete"
log "summary: $SUMMARY_PATH"

cat "$SUMMARY_PATH"

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
