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

failure_hint() {
  local details="$1"

  case "$details" in
    "fail:清空購物車失敗")
      echo "先做 status 檢查 cart 是否 ready，再重跑：orbit -c docs/examples/mini-shop/dev.yaml status --json"
      ;;
    "fail:加入品項失敗")
      echo "確認 catalog 已 ready 並重試：orbit -c docs/examples/mini-shop/dev.yaml logs catalog-api -f"
      ;;
    "fail:checkout 呼叫失敗")
      echo "先看 checkout 啟動與依賴：orbit -c docs/examples/mini-shop/dev.yaml logs checkout-api -f"
      ;;
    "fail:回應缺少 code（API 可能改版）")
      echo "先看 checkout health 與輸出欄位：curl http://127.0.0.1:3006/health，確認 API 規約未變"
      ;;
    "fail:成功路徑 code 不符（預期 checkout_ok，實際 "*)
      echo "檢查 payment/order/shipping：orbit -c docs/examples/mini-shop/dev.yaml logs payment-api order-api shipping-api -f"
      ;;
    "fail:orders API 無法讀取")
      echo "重點修 order 與 DB：orbit -c docs/examples/mini-shop/dev.yaml logs order-api -f"
      ;;
    "fail:shipments API 無法讀取")
      echo "重點修 shipping 與 DB：orbit -c docs/examples/mini-shop/dev.yaml logs shipping-api -f"
      ;;
    "fail:checkout 回應與 shipping 關聯不一致")
      echo "確認 checkout payload 與 shipment 是否一致：orbit -c docs/examples/mini-shop/dev.yaml logs checkout-api shipping-api -f"
      ;;
    "fail:預期為 payment 失敗碼，實際為 "*)
      echo "確認 payment 失敗碼與 checkout 邏輯：orbit -c docs/examples/mini-shop/dev.yaml status --json；再跑 checkout 測試"
      ;;
    "not_run:"*)
      echo "此 mode 未執行該場景：請用 bash docs/examples/mini-shop/scripts/smoke-compact.sh all 取得完整覆蓋"
      ;;
    *)
      echo "先做系統性診斷：orbit -c docs/examples/mini-shop/dev.yaml status --json，必要時配合對應服務 logs"
      ;;
  esac
}

check_cart_checkout_success() {
  local method="$1"
  local resp
  local code
  local payload_path
  local correlation

  if ! curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST >/dev/null; then
    echo "fail:清空購物車失敗"
    return 1
  fi

  if ! curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
    -H 'Content-Type: application/json' \
    -d '{"product_id":'$PRODUCT_ID',"quantity":1}' >/dev/null; then
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
    if ! curl -fsS "http://$BASE:3008/shipments?customer_id=$CUSTOMER_ID" >/dev/null; then
      echo "fail:shipments API 無法讀取"
      return 1
    fi

    payload_path="$(mktemp)"
    printf '%s' "$resp" >"$payload_path"
    correlation="$(python3 - "$payload_path" <<'PY'
import json, sys

payload = json.loads(sys.argv[1])
orders = payload.get("orders", [])
shipments = payload.get("shipments", [])

if not isinstance(orders, list) or not orders:
    raise SystemExit("no_order_in_checkout")
if not isinstance(shipments, list) or not shipments:
    raise SystemExit("no_shipment_in_checkout")

order_ids = set()
for item in orders:
    item_id = item.get("id")
    if isinstance(item_id, int) and item_id > 0:
        order_ids.add(item_id)
    elif isinstance(item_id, str) and item_id.isdigit():
        order_ids.add(int(item_id))

ship_order_ids = set()
for item in shipments:
    order_id = item.get("order_id")
    if isinstance(order_id, int) and order_id > 0:
        ship_order_ids.add(order_id)
    elif isinstance(order_id, str) and order_id.isdigit():
        ship_order_ids.add(int(order_id))

if not order_ids.issubset(ship_order_ids):
    missing = sorted(order_ids - ship_order_ids)
    raise SystemExit("missing_shipments_for_orders:" + ",".join(str(x) for x in missing))

print(",".join(str(x) for x in sorted(order_ids)))
PY
)"
    status=$?
    if [[ $status -ne 0 ]]; then
      rm -f "$payload_path"
      echo "fail:checkout 回應與 shipping 關聯不一致"
      return 1
    fi
    rm -f "$payload_path"
    echo "pass:checkout_ok"
    if [[ -n "${correlation:-}" ]]; then
      echo "corr:${correlation}"
    fi
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
  local start_ts
  local response
  local first_line
  local corr_line
  local rc
  start_ts="$(date +%s%N)"
  set +e
  response="$(check_cart_checkout_success "mock_card")"
  rc=$?
  set -e
  first_line="$(printf '%s\n' "$response" | sed -n '1p')"
  corr_line="$(printf '%s\n' "$response" | awk -F: '/^corr:/{print $2}' | tr -d '\n')"
  echo "${first_line}|${start_ts}|${corr_line}"
  return "$rc"
}

run_decline_flow() {
  log "情境 decline：清空購物車→加 1 件→decline" >&2
  local start_ts
  local response
  local first_line
  local corr_line
  local rc
  start_ts="$(date +%s%N)"
  set +e
  response="$(check_cart_checkout_success "decline")"
  rc=$?
  set -e
  first_line="$(printf '%s\n' "$response" | sed -n '1p')"
  corr_line="$(printf '%s\n' "$response" | awk -F: '/^corr:/{print $2}' | tr -d '\n')"
  echo "${first_line}|${start_ts}|${corr_line}"
  return "$rc"
}

success_detail="not_run:mode does not include success"
decline_detail="not_run:mode does not include decline"
success_corr=""
decline_corr=""
success_passed=0
decline_passed=0
success_start_ts=""
decline_start_ts=""
success_ms=0
decline_ms=0

if [[ "$MODE" != "all" && "$MODE" != "success" && "$MODE" != "decline" ]]; then
  echo "Usage: $0 [all|success|decline]" >&2
  exit 1
fi

if [[ "$MODE" == "all" || "$MODE" == "success" ]]; then
  set +e
  success_result="$(run_success_flow)"
  success_rc=$?
  set -e
  success_detail="$(printf '%s\n' "$success_result" | awk -F'|' 'NR==1{print $1}')"
  success_start_ts="$(printf '%s\n' "$success_result" | awk -F'|' '{print $2}')"
  success_corr="$(printf '%s\n' "$success_result" | awk -F'|' '{print $3}')"
  if [[ -n "$success_start_ts" ]]; then
    success_ms=$(( $(date +%s%N) - success_start_ts ))
    success_ms=$(( success_ms / 1000000 ))
  fi
  if [[ "$success_rc" -eq 0 ]]; then
    success_passed=1
  else
    success_passed=0
  fi
fi

if [[ "$MODE" == "all" || "$MODE" == "decline" ]]; then
  set +e
  decline_result="$(run_decline_flow)"
  decline_rc=$?
  set -e
  decline_detail="$(printf '%s\n' "$decline_result" | awk -F'|' 'NR==1{print $1}')"
  decline_start_ts="$(printf '%s\n' "$decline_result" | awk -F'|' '{print $2}')"
  decline_corr="$(printf '%s\n' "$decline_result" | awk -F'|' '{print $3}')"
  if [[ -n "$decline_start_ts" ]]; then
    decline_ms=$(( $(date +%s%N) - decline_start_ts ))
    decline_ms=$(( decline_ms / 1000000 ))
  fi
  if [[ "$decline_rc" -eq 0 ]]; then
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

success_hint="$(failure_hint "$success_detail")"
decline_hint="$(failure_hint "$decline_detail")"

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
export SUCCESS_HINT="$success_hint"
export DECLINE_HINT="$decline_hint"
export SUCCESS_MS="$success_ms"
export DECLINE_MS="$decline_ms"
export SUCCESS_CORR="$success_corr"
export DECLINE_CORR="$decline_corr"
export MODE_NAME="$MODE"
export SUITE_PASSED="$suite_passed"
export DURATION_SEC="$DURATION"
export STARTED_AT="$START_TS"
python3 - <<'PY'
import json
import os

mode = os.environ["MODE_NAME"]
suite_passed = os.environ["SUITE_PASSED"] == "1"

report = {
    "mode": "compact",
    "run_mode": mode,
    "started_at": int(os.environ["STARTED_AT"]),
    "duration_seconds": int(os.environ["DURATION_SEC"]),
    "suite_passed": suite_passed,
    "release_ready": suite_passed,
    "scenarios": {
    "success": {
            "passed": mode in ("all", "success") and os.environ["SUCCESS_DETAIL"].startswith("pass:"),
            "details": os.environ["SUCCESS_DETAIL"],
            "correlation_order_ids": [int(item) for item in (os.environ.get("SUCCESS_CORR", "").split(",")) if item],
            "next_action": os.environ["SUCCESS_HINT"],
            "duration_ms": int(os.environ["SUCCESS_MS"]),
        },
        "decline": {
            "passed": mode in ("all", "decline") and os.environ["DECLINE_DETAIL"].startswith("pass:"),
            "details": os.environ["DECLINE_DETAIL"],
            "correlation_order_ids": [int(item) for item in (os.environ.get("DECLINE_CORR", "").split(",")) if item],
            "next_action": os.environ["DECLINE_HINT"],
            "duration_ms": int(os.environ["DECLINE_MS"]),
        },
    },
    "ux_readiness": {
        "first_run_success_ms": int(os.environ["SUCCESS_MS"] if os.environ["SUCCESS_DETAIL"].startswith("pass:") else 0),
        "first_run_target_ms": 60000,
        "first_run_within_target": True if (
            os.environ["SUCCESS_DETAIL"].startswith("pass:") and int(os.environ["SUCCESS_MS"]) <= 60000
        ) else False,
    },
    "evidence": {
        "endpoints_checked": [
            "http://127.0.0.1:3005/carts/{customer_id}/clear",
            "http://127.0.0.1:3005/carts/{customer_id}/items",
            "http://127.0.0.1:3006/checkout/{customer_id}",
            "http://127.0.0.1:3002/orders",
            "http://127.0.0.1:3008/shipments?customer_id=1",
        ],
        "target_customer_id": 1,
        "target_product_id": 1,
        "method": "sequential_http",
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
  log "success duration_ms=${success_ms}, decline duration_ms=${decline_ms}"
  log "success: ${success_detail}"
  log "decline: ${decline_detail}"
  exit 0
fi

log "compact smoke failed, report=$REPORT_PATH"
log "success: ${success_detail}"
log "decline: ${decline_detail}"
if [[ "$success_passed" -ne 1 ]]; then
  log "success 建議：$success_hint"
fi
if [[ "$decline_passed" -ne 1 ]]; then
  log "decline 建議：$decline_hint"
fi
log "建議後續動作：1) 先執行 orbit status --json 檢查；2) 重跑 bash docs/examples/mini-shop/scripts/smoke-compact.sh"
exit 1
