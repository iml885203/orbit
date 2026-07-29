#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-all}"
ROOT_DIR="/Users/logan/dev/orbit"
BASE="127.0.0.1"
TIMEOUT_SECONDS=120
SLEEP_SEC=2
READY_FAIL_SERVICE=""
READY_FAIL_URL=""
READY_FAIL_ELAPSED=0
LATEST_MODE=""

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

suggest_recovery() {
  local config_file="$1"
  local group_name="$2"
  local phase="$3"
  log "建議立即修復動作（可直接複製貼上）："
  echo "  1) orbit -c docs/examples/mini-shop/$config_file status --json --group $group_name"
  echo "  2) orbit -c docs/examples/mini-shop/$config_file logs <service> -f"
  echo "  3) orbit -c docs/examples/mini-shop/$config_file restart $phase --json"
  echo "  4) orbit -c docs/examples/mini-shop/$config_file down --group $group_name"
}

print_ready_dump() {
  local config_file="$1"
  local group_name="$2"
  local dump_file="$REPORT_DIR/$LATEST_MODE-readiness-dump.json"
  log "取得 ${group_name} 狀態快照：$dump_file"

  if (cd "$ROOT_DIR" && orbit -c "docs/examples/mini-shop/$config_file" status --json --group "$group_name") >"$dump_file" 2>&1; then
    python3 - "$dump_file" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1]))
services = payload.get("services", payload.get("resources", {}))
if isinstance(services, dict):
    bad = [k for k,v in services.items() if str(v.get("status","")).lower() not in ("ok","ready")]
    if bad:
        print("  當前非就緒：", ", ".join(sorted(bad)))
    else:
        print("  狀態快照：全數為 ok / ready")
else:
    print("  狀態格式：", type(services).__name__)
PY
  else
    log "status --json 目前無法取得，請先確認 Orbit daemon 與目標環境是否仍在啟動中。"
  fi
}

mark_readiness_fail() {
  READY_FAIL_SERVICE="$1"
  READY_FAIL_URL="$2"
  READY_FAIL_ELAPSED="$3"
}

reset_readiness_fail() {
  READY_FAIL_SERVICE=""
  READY_FAIL_URL=""
  READY_FAIL_ELAPSED=0
}

wait_for_ready_http() {
  local name="$1"
  local url="$2"
  local ready_seconds=0
  reset_readiness_fail

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

  mark_readiness_fail "$name" "$url" "$ready_seconds"
  return 1
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
    cat <<EOF
[mini-shop-p1-smoke][FAIL] smoke 場景未完整通過，失敗步驟：$fail_count
   - 報告：${log_prefix}-summary.json
   - 日誌：${log_prefix}-success.log / ${log_prefix}-decline.log / ${log_prefix}-stock.log
   - 建議：
     1) 檢查前一輪 service 日誌
     2) 重新執行一個場景：bash docs/examples/mini-shop/scripts/smoke-demo.sh success|decline|stock
EOF
    return 1
  fi

  log "smoke 場景已完成，報告：${log_prefix}-summary.json；日誌：${log_prefix}-*.log"
  return 0
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
  if [[ "$ratio" == "missing" ]]; then
    return 1
  fi
  return 0
}

check_notification_signal() {
  if [[ "$BASE" != "127.0.0.1" ]]; then
    return 0
  fi

  local payload
  if ! payload=$(curl -fsS "http://$BASE:3009/notifications"); then
    log "advanced 通知 API 無法取得（可能服務未就緒）"
    return 1
  fi

  local total
  total=$(python3 - "$payload" <<'PY'
import json,sys
payload = json.loads(sys.argv[1])
notifications = payload.get("notifications", [])
print(len(notifications))
PY
)
  if [[ -z "$total" || "$total" == "0" ]]; then
    log "advanced 通知 API 尚未收到事件：需要至少 1 筆通知作為完整閉環證據"
    return 1
  fi

  log "advanced 通知事件數量: $total"
  return 0
}

run_suite() {
  local env_type="$1"
  log "=== $env_type 套件開始 ==="
  local -a readiness_targets=()
  readiness_targets=("catalog-api|http://$BASE:3001/health" "inventory-api|http://$BASE:3003/health" "customer-api|http://$BASE:3004/health" "order-api|http://$BASE:3002/health" "cart-api|http://$BASE:3005/health" "payment-api|http://$BASE:3007/health" "shipping-api|http://$BASE:3008/health" "checkout-api|http://$BASE:3006/health")

  if [[ "$env_type" == "mini" ]]; then
    readiness_targets+=("web|http://$BASE:3000")
  else
    readiness_targets+=("web|http://$BASE:3000" "observability-api|http://$BASE:3010" "notification-api|http://$BASE:3009/health")
  fi

  local service_name
  local service_url
  for pair in "${readiness_targets[@]}"; do
    service_name="${pair%%|*}"
    service_url="${pair##*|}"
    if ! wait_for_ready_http "$service_name" "$service_url"; then
      log "等待 $service_name 就緒失敗，已用時 ${READY_FAIL_ELAPSED}s（上限 ${TIMEOUT_SECONDS}s）"
      suggest_recovery "$( [ "$env_type" = mini ] && echo dev.yaml || echo dev-advanced.yaml )" "$( [ "$env_type" = mini ] && echo mini-shop || echo mini-shop-advanced )" "$service_name"
      print_ready_dump "$( [ "$env_type" = mini ] && echo dev.yaml || echo dev-advanced.yaml )" "$( [ "$env_type" = mini ] && echo mini-shop || echo mini-shop-advanced )"
      fail "等待 $service_name 就緒超時（${READY_FAIL_ELAPSED}s，超過 ${TIMEOUT_SECONDS}s）：$READY_FAIL_URL"
    fi
  done

  run_smoke_scenarios "$env_type" || return 1
  if [[ "$env_type" == "advanced" ]]; then
    check_observability
    check_notification_signal
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

  local up_cmd=(orbit -c "docs/examples/mini-shop/$config_file" up --group "$group_name")
  (cd "$ROOT_DIR" && "${up_cmd[@]}") >/tmp/mini-shop-p1-orbit-up.log 2>&1 || {
    echo "[mini-shop-p1-smoke][FAIL] orbit up 失敗，command:"
    printf '  %q ' "${up_cmd[@]}"
    printf '\n'
    echo "[mini-shop-p1-smoke][FAIL] 詳細錯誤輸出："
    cat /tmp/mini-shop-p1-orbit-up.log
    echo "[mini-shop-p1-smoke] 建議下一步："
    echo "  1) 檢查衝突服務：orbit -c docs/examples/mini-shop/$config_file status --json --group $group_name"
    echo "  2) 需要清空環境時：orbit -c docs/examples/mini-shop/$config_file down --group $group_name"
    print_ready_dump "$config_file" "$group_name"
    fail "請先用上述命令排查，修正後再重跑。"
  }
}

down_env() {
  local config_file="$1"
  local group_name="$2"

  log "關閉 $config_file 群組：$group_name（避免與下一個套件端口衝突）"
  (cd "$ROOT_DIR" && orbit -c "docs/examples/mini-shop/$config_file" down --group "$group_name") >/tmp/mini-shop-p1-orbit-down.log 2>&1 || {
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
  LATEST_MODE="$mode"
  if [[ "$mode" == "mini" ]]; then
    START_TS=$(date -u +%s)
    up_env mini
    if ! run_suite mini; then
      fail "mini 套件未通過（詳見上方訊息與 /tmp/mini-shop-smoke-reports/mini-summary.json）"
    fi
    print_pr_summary "$REPORT_DIR/mini-summary.json" "mini"
  elif [[ "$mode" == "advanced" ]]; then
    START_TS=$(date -u +%s)
    up_env advanced
    if ! run_suite advanced; then
      fail "advanced 套件未通過（詳見上方訊息與 /tmp/mini-shop-smoke-reports/advanced-summary.json）"
    fi
    print_pr_summary "$REPORT_DIR/advanced-summary.json" "advanced"
  else
    START_TS=$(date -u +%s)
    up_env mini
    if ! run_suite mini; then
      fail "mini 套件未通過（詳見上方訊息與 /tmp/mini-shop-smoke-reports/mini-summary.json）"
    fi
    print_pr_summary "$REPORT_DIR/mini-summary.json" "mini"
    down_env "dev.yaml" "mini-shop"

    START_TS=$(date -u +%s)
    up_env advanced
    if ! run_suite advanced; then
      fail "advanced 套件未通過（詳見上方訊息與 /tmp/mini-shop-smoke-reports/advanced-summary.json）"
    fi
    print_pr_summary "$REPORT_DIR/advanced-summary.json" "advanced"
    combine_all_summary
    print_pr_summary "$REPORT_DIR/all-summary.json" "all"
  fi
}

run_suite_set "$MODE"
log "P1 smoke 完成"
