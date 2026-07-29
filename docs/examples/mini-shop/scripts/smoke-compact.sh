#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-all}"
BASE="127.0.0.1"
CUSTOMER_ID=1
PRODUCT_ID=1
REPORT_DIR="/tmp/mini-shop-smoke-reports"
START_TS=$(date +%s)

log() {
  echo "[mini-shop-compact-smoke] $*"
}

extract_code() {
  local response="$1"
  python3 - "$response" <<'PY'
import json, sys

payload = sys.argv[1]
try:
    data = json.loads(payload)
    print(data.get("code", ""))
except Exception:
    print("")
PY
}

check_cart_checkout_success() {
  local method="$1"
  local resp
  local code

  if ! curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST >/dev/null; then
    echo "fail:清空購物車失敗"
    return 1
  fi

  if ! curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
    -H 'Content-Type: application/json' \
    -d '{"product_id":'"$PRODUCT_ID"',"quantity":1}' >/dev/null; then
    echo "fail:加入品項失敗"
    return 1
  fi

  if ! resp=$(curl -sS "http://$BASE:3006/checkout/$CUSTOMER_ID" \
    -H 'Content-Type: application/json' \
    -d "{\"method\":\"$method\"}"); then
    echo "fail:checkout 呼叫失敗"
    return 1
  fi

  code="$(extract_code "$resp")"
  if [[ -z "$code" ]]; then
    echo "fail:回應缺少 code（API 可能改版）"
    return 1
  fi

  if [[ "$method" == "mock_card" ]]; then
    if [[ "$code" != "checkout_ok" ]]; then
      echo "fail:成功路徑 code 不符（預期 checkout_ok，實際 $code）"
      return 1
    fi
    if ! curl -fsS "http://$BASE:3002/orders" >/dev/null; then
      echo "fail:orders API 無法讀取"
      return 1
    fi
    echo "pass:checkout_ok"
    return 0
  fi

  if [[ "$code" != "forced_decline" && "$code" != "payment_declined" && "$code" != "payment_failed" ]]; then
    echo "fail:預期為 payment 失敗碼，實際為 $code"
    return 1
  fi
  echo "pass:$code"
  return 0
}

run_success_flow() {
  log "情境 success：清空購物車→加 1 件→mock_card" >&2
  check_cart_checkout_success "mock_card"
}

run_decline_flow() {
  log "情境 decline：清空購物車→加 1 件→decline" >&2
  check_cart_checkout_success "decline"
}

success_detail="not_run:mode does not include success"
decline_detail="not_run:mode does not include decline"
success_passed=0
decline_passed=0

if [[ "$MODE" != "all" && "$MODE" != "success" && "$MODE" != "decline" ]]; then
  echo "Usage: $0 [all|success|decline]" >&2
  exit 1
fi

if [[ "$MODE" == "all" || "$MODE" == "success" ]]; then
  if success_detail="$(run_success_flow)"; then
    success_passed=1
  else
    success_passed=0
  fi
fi

if [[ "$MODE" == "all" || "$MODE" == "decline" ]]; then
  if decline_detail="$(run_decline_flow)"; then
    decline_passed=1
  else
    decline_passed=0
  fi
fi

if [[ "$MODE" == "success" ]]; then
  decline_detail="not_run:mode only success"
elif [[ "$MODE" == "decline" ]]; then
  success_detail="not_run:mode only decline"
fi

if [[ "$MODE" == "all" ]]; then
  suite_passed=$((success_passed & decline_passed))
elif [[ "$MODE" == "success" ]]; then
  suite_passed=$success_passed
else
  suite_passed=$decline_passed
fi

mkdir -p "$REPORT_DIR"
DURATION=$(( $(date +%s) - START_TS ))

export SUCCESS_DETAIL="$success_detail"
export DECLINE_DETAIL="$decline_detail"
export MODE_NAME="$MODE"
export SUITE_PASSED="$suite_passed"
export DURATION_SEC="$DURATION"
export STARTED_AT="$START_TS"
python3 - <<'PY'
import json
import os

report = {
    "mode": "compact",
    "run_mode": os.environ["MODE_NAME"],
    "started_at": int(os.environ["STARTED_AT"]),
    "duration_seconds": int(os.environ["DURATION_SEC"]),
    "suite_passed": os.environ["SUITE_PASSED"] == "1",
    "scenarios": {
        "success": {
            "passed": os.environ["MODE_NAME"] in ("all", "success") and os.environ["SUCCESS_DETAIL"].startswith("pass:"),
            "details": os.environ["SUCCESS_DETAIL"],
        },
        "decline": {
            "passed": os.environ["MODE_NAME"] in ("all", "decline") and os.environ["DECLINE_DETAIL"].startswith("pass:"),
            "details": os.environ["DECLINE_DETAIL"],
        },
    },
    "release_signal": {
        "first_time_path": True,
        "meaningful_for_demo": "compact 路徑只驗證成功與 payment 失敗，不包含庫存不足；適合 1.0 前快速心理模型檢核。",
    },
}

path = "/tmp/mini-shop-smoke-reports/compact-summary.json"
with open(path, "w", encoding="utf-8") as f:
    json.dump(report, f, ensure_ascii=False, indent=2)
print(path)
PY

REPORT_PATH="$REPORT_DIR/compact-summary.json"
if [[ "$suite_passed" -eq 1 ]]; then
  log "compact smoke passed in ${DURATION}s, report=$REPORT_PATH"
  log "success: ${success_detail}"
  log "decline: ${decline_detail}"
  exit 0
fi

log "compact smoke failed, report=$REPORT_PATH"
log "success: ${success_detail}"
log "decline: ${decline_detail}"
log "建議後續動作：1) 先執行 orbit status --json 檢查；2) 重跑 bash docs/examples/mini-shop/scripts/smoke-compact.sh"
exit 1
