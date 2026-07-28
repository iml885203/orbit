#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-all}"
ROOT_DIR="/Users/logan/dev/orbit"
BASE="127.0.0.1"
TIMEOUT_SECONDS=120
SLEEP_SEC=2

REPORT_DIR="/tmp/mini-shop-smoke-reports"
START_TS=0

usage() {
  cat <<'USAGE'
用法：
bash docs/examples/mini-shop/scripts/smoke-p1.sh [mini|advanced|all]

mini     - 主線 mini-shop
advanced - mini-shop-advanced（多 observability-api）
all      - 先 mini，再 advanced

行為：
- 啟動目標環境
- 等候服務就緒（health / web）
- 跑 success / decline / stock 腳本
- 回報是否能進行可驗證結果（orders / shipments / observations）

預設：all
USAGE
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

if [[ "$MODE" != "mini" && "$MODE" != "advanced" && "$MODE" != "all" ]]; then
  echo "不支援模式: $MODE"
  usage
  exit 1
fi

log() {
  echo "[mini-shop-p1-smoke] $*"
}

fail() {
  echo "[mini-shop-p1-smoke][FAIL] $*" >&2
  exit 1
}

wait_for_ready_http() {
  local name="$1"
  local url="$2"
  local ready_seconds=0

  while (( ready_seconds < TIMEOUT_SECONDS )); do
    if [[ "$name" == "web" ]]; then
      if curl -fsS "$url" >/dev/null 2>&1; then
        log "已就緒：$name"
        return 0
      fi
    elif [[ "$name" == "observability-api" ]]; then
      if curl -fsS "$url/health" >/dev/null 2>&1; then
        log "已就緒：$name"
        return 0
      fi
    else
      if curl -fsS "$url/health" | python3 - "$name" <<'PY'
import json,sys
name = sys.argv[1]
raw = sys.stdin.read()
try:
    data = json.loads(raw)
except Exception:
    raise SystemExit(1)
status = data.get("status", "")
if status not in ("ok","degraded"):
    raise SystemExit(1)
print(f"{name}:{status}")
PY
      then
        log "已就緒：$name"
        return 0
      fi
    fi

    sleep "$SLEEP_SEC"
    ready_seconds=$((ready_seconds + SLEEP_SEC))
  done

  fail "等待 $name 就緒超時（${TIMEOUT_SECONDS}s）: $url"
}

run_smoke_scenarios() {
  log "執行命令列 smoke 場景"
  local mode_label="$1"
  local checkout_success=0
  local checkout_decline=0
  local checkout_stock=0
  local fail_count=0
  local log_prefix="$REPORT_DIR/$mode_label"

  mkdir -p "$REPORT_DIR"
  : > "${log_prefix}-summary.json"

  if bash docs/examples/mini-shop/scripts/smoke-demo.sh success >"${log_prefix}-success.log" 2>&1; then
    checkout_success=1
  else
    fail_count=$((fail_count + 1))
  fi

  if bash docs/examples/mini-shop/scripts/smoke-demo.sh decline >"${log_prefix}-decline.log" 2>&1; then
    checkout_decline=1
  else
    fail_count=$((fail_count + 1))
  fi

  if bash docs/examples/mini-shop/scripts/smoke-demo.sh stock >"${log_prefix}-stock.log" 2>&1; then
    checkout_stock=1
  else
    fail_count=$((fail_count + 1))
  fi

  if curl -fsS "http://$BASE:3002/orders" >/dev/null && curl -fsS "http://$BASE:3008/shipments" >/dev/null; then
    log "成功場景可讀：orders / shipments endpoint 都有回應"
  else
    fail "成功路徑驗證失敗：orders 或 shipments 未回應"
  fi

  # 只用來驗證 API 邊界，期望仍有可讀回應 (包括 4xx)
  local api_probe_path="${log_prefix}-api-probe.json"
  if ! curl -fsS "http://$BASE:3006/checkout/1" -H 'Content-Type: application/json' -d '{"method":"mock_card","force_decline":true}' >"$api_probe_path" 2>&1; then
    # allow non-200 when service is validating, but must return response body
    if [[ ! -s "$api_probe_path" ]]; then
      fail "checkout API 異常：無法回傳可讀回應"
    fi
  fi

  local has_success
  local has_decline
  local has_stock
  has_success=$((checkout_success))
  has_decline=$((checkout_decline))
  has_stock=$((checkout_stock))

  local now_ts
  now_ts=$(date -u +%s)
  cat > "${log_prefix}-summary.json" <<JSON
{
  "mode": "$mode_label",
  "started_at": ${START_TS},
  "ended_at": ${now_ts},
  "duration_seconds": $((now_ts - START_TS)),
  "suite_passed": $([ $fail_count -eq 0 ] && echo true || echo false),
  "scenarios": {
    "success": $([ "$has_success" -eq 1 ] && echo true || echo false),
    "decline": $([ "$has_decline" -eq 1 ] && echo true || echo false),
    "stock": $([ "$has_stock" -eq 1 ] && echo true || echo false)
  },
  "api_probe": {
    "status": "ok",
    "response_path": "$api_probe_path"
  }
}
JSON

  if (( fail_count > 0 )); then
    fail "smoke 場景未完整通過，失敗步驟：$fail_count（詳見 ${log_prefix}-*.log）"
  fi

  log "smoke 場景已完成，報告：${log_prefix}-summary.json；日誌：${log_prefix}-*.log"
}

check_observability() {
  log "檢查觀測 API"
  local ratio
  ratio=$(curl -fsS "http://$BASE:3010/insights" | python3 - <<'PY'
import json,sys
payload = json.load(sys.stdin)
print(payload.get('correlation', {}).get('correlation_ratio', 'missing'))
PY
)
  log "observability correlation_ratio = $ratio"
}

run_suite() {
  local env_type="$1"
  log "=== $env_type 套件開始 ==="

  if [[ "$env_type" == "mini" ]]; then
    wait_for_ready_http "catalog-api" "http://$BASE:3001"
    wait_for_ready_http "inventory-api" "http://$BASE:3003"
    wait_for_ready_http "customer-api" "http://$BASE:3004"
    wait_for_ready_http "order-api" "http://$BASE:3002"
    wait_for_ready_http "cart-api" "http://$BASE:3005"
    wait_for_ready_http "payment-api" "http://$BASE:3007"
    wait_for_ready_http "shipping-api" "http://$BASE:3008"
    wait_for_ready_http "checkout-api" "http://$BASE:3006"
    wait_for_ready_http "web" "http://$BASE:3000"
  else
    wait_for_ready_http "catalog-api" "http://$BASE:3001"
    wait_for_ready_http "inventory-api" "http://$BASE:3003"
    wait_for_ready_http "customer-api" "http://$BASE:3004"
    wait_for_ready_http "order-api" "http://$BASE:3002"
    wait_for_ready_http "cart-api" "http://$BASE:3005"
    wait_for_ready_http "payment-api" "http://$BASE:3007"
    wait_for_ready_http "shipping-api" "http://$BASE:3008"
    wait_for_ready_http "checkout-api" "http://$BASE:3006"
    wait_for_ready_http "web" "http://$BASE:3000"
    wait_for_ready_http "observability-api" "http://$BASE:3010"
  fi

  run_smoke_scenarios "$env_type"

  if [[ "$env_type" == "advanced" ]]; then
    check_observability
  fi

  log "=== $env_type 套件完成 ==="
}

up_env() {
  local mode="$1"
  local config_file
  local group_name
  if [[ "$mode" == "mini" ]]; then
    log "啟動主線 mini-shop：$ROOT_DIR/docs/examples/mini-shop/dev.yaml"
    config_file="dev.yaml"
    group_name="mini-shop"
  else
    log "啟動進階 mini-shop-advanced：$ROOT_DIR/docs/examples/mini-shop/dev-advanced.yaml"
    config_file="dev-advanced.yaml"
    group_name="mini-shop-advanced"
  fi

  (cd "$ROOT_DIR" && orbit -c "docs/examples/mini-shop/$config_file" up -g "$group_name") >/tmp/mini-shop-p1-orbit-up.log 2>&1 || {
    fail "orbit up 失敗，請查看 /tmp/mini-shop-p1-orbit-up.log"
  }
}

down_env() {
  local config_file="$1"
  local group_name="$2"

  log "關閉 $config_file 群組：$group_name（避免與下一個套件端口衝突）"
  (cd "$ROOT_DIR" && orbit -c "docs/examples/mini-shop/$config_file" down -g "$group_name") >/tmp/mini-shop-p1-orbit-down.log 2>&1 || {
    log "orbit down 失敗（略過），請手動檢查 /tmp/mini-shop-p1-orbit-down.log"
  }
}

combine_all_summary() {
  local mini_path="$REPORT_DIR/mini-summary.json"
  local advanced_path="$REPORT_DIR/advanced-summary.json"
  local combined_path="$REPORT_DIR/all-summary.json"
  python3 - "$mini_path" "$advanced_path" "$combined_path" <<'PY'
import json
import sys

mini_path, advanced_path, combined_path = sys.argv[1], sys.argv[2], sys.argv[3]

with open(mini_path, "r", encoding="utf-8") as fh:
    mini = json.load(fh)
with open(advanced_path, "r", encoding="utf-8") as fh:
    advanced = json.load(fh)

combined = {
    "mode": "all",
    "started_at": mini.get("started_at"),
    "ended_at": advanced.get("ended_at"),
    "duration_seconds": advanced.get("ended_at", 0) - mini.get("started_at", 0),
    "suite_passed": bool(mini.get("suite_passed")) and bool(advanced.get("suite_passed")),
    "suites": {
        "mini": mini,
        "advanced": advanced,
    },
}

with open(combined_path, "w", encoding="utf-8") as out:
    json.dump(combined, out, ensure_ascii=False, indent=2)
    out.write("\n")
PY
}

print_pr_summary() {
  local summary_file="$1"
  local mode="$2"
  local suite_passed
  local duration
  suite_passed=$(python3 - "$summary_file" <<'PY'
import json,sys
payload=json.load(open(sys.argv[1]))
print("PASS" if payload.get("suite_passed") else "FAIL")
PY
)
  duration=$(python3 - "$summary_file" <<'PY'
import json,sys
payload=json.load(open(sys.argv[1]))
print(payload.get("duration_seconds", 0))
PY
)
  echo
  echo "=== mini-shop PR 快速摘要 ==="
  echo "套件：$mode"
  echo "結果：$suite_passed"
  echo "耗時：${duration}s"
  echo "報告：$summary_file"
}

run_suite_set() {
  local mode="$1"
  if [[ "$mode" == "mini" ]]; then
    START_TS=$(date -u +%s)
    up_env mini
    run_suite mini
    print_pr_summary "$REPORT_DIR/mini-summary.json" "mini"
  elif [[ "$mode" == "advanced" ]]; then
    START_TS=$(date -u +%s)
    up_env advanced
    run_suite advanced
    print_pr_summary "$REPORT_DIR/advanced-summary.json" "advanced"
  else
    START_TS=$(date -u +%s)
    up_env mini
    run_suite mini
    print_pr_summary "$REPORT_DIR/mini-summary.json" "mini"
    down_env "dev.yaml" "mini-shop"

    START_TS=$(date -u +%s)
    up_env advanced
    run_suite advanced
    print_pr_summary "$REPORT_DIR/advanced-summary.json" "advanced"
    combine_all_summary
    print_pr_summary "$REPORT_DIR/all-summary.json" "all"
  fi
}

run_suite_set "$MODE"
log "P1 smoke 完成"
