#!/usr/bin/env bash

set -euo pipefail

BASE="${BASE:-127.0.0.1}"
TIMEOUT_SEC="${TIMEOUT_SEC:-120}"
SLEEP_SECONDS="${SLEEP_SECONDS:-2}"
REPORT_PATH="${REPORT_PATH:-/tmp/mini-shop-smoke-reports/onboarding-summary.json}"

CATALOG_URL="http://$BASE:3001"
CART_URL="http://$BASE:3005"
CHECKOUT_URL="http://$BASE:3006"
ORDER_URL="http://$BASE:3002"
SHIPMENT_URL="http://$BASE:3008"
CUSTOMER_ID=1
PRODUCT_ID=1

log() {
  echo "[mini-shop-onboarding] $*"
}

extract_json_field() {
  local payload="$1"
  local field="$2"
  python3 - "$payload" "$field" <<'PY'
import json
import sys

payload = sys.argv[1]
field = sys.argv[2]
try:
    data = json.loads(payload)
    value = data.get(field, "")
except Exception:
    value = ""
print(value)
PY
}

wait_for_ready() {
  local url="$1"
  local name="$2"
  local elapsed=0

  log "等待 ${name} 就緒..."
  while true; do
    if response="$(curl -sS "$url/health" 2>/dev/null)"; then
      status="$(extract_json_field "$response" status)"
      if [[ "$status" == "ok" ]]; then
        echo "  ✅ ${name}: ok"
        return 0
      fi
    fi

    if [[ "$elapsed" -ge "$TIMEOUT_SEC" ]]; then
      echo "  ❌ ${name}: timeout（${TIMEOUT_SEC}s）"
      return 1
    fi

    log "  尚未 ready，等待 ${SLEEP_SECONDS}s..."
    sleep "$SLEEP_SECONDS"
    elapsed=$((elapsed + SLEEP_SECONDS))
  done
}

run_success_demo() {
  log "第一輪成功流程（2 分鐘內可完成）"

  if ! curl -sS "$CART_URL/carts/$CUSTOMER_ID/clear" -X POST >/dev/null; then
    log "❌ 無法清空購物車"
    return 1
  fi

  if ! curl -sS "$CART_URL/carts/$CUSTOMER_ID/items" \
    -H 'Content-Type: application/json' \
    -d '{"product_id":'"$PRODUCT_ID"',"quantity":1}' >/dev/null; then
    log "❌ 無法加入購物車項目"
    return 1
  fi

  log "已加入商品，執行 checkout..."
  if ! response="$(curl -sS "$CHECKOUT_URL/checkout/$CUSTOMER_ID" \
    -H 'Content-Type: application/json' \
    -d '{"method":"mock_card"}' 2>/dev/null)"; then
    log "❌ Checkout API 呼叫失敗"
    return 1
  fi

  code="$(extract_json_field "$response" code)"
  if [[ "$code" != "checkout_ok" ]]; then
    log "❌ Checkout 回傳 code 非預期：${code}"
    return 1
  fi

  if ! orders_payload="$(curl -sS "$ORDER_URL/orders" 2>/dev/null)"; then
    log "❌ 讀不到 orders"
    return 1
  fi

  if ! shipment_payload="$(curl -sS "$SHIPMENT_URL/shipments?customer_id=$CUSTOMER_ID" 2>/dev/null)"; then
    log "❌ 讀不到 shipments"
    return 1
  fi

  echo "  ✅ 成功：checkout_ok"
  echo "  🧾 Orders：$(python3 - "$orders_payload" <<'PY'
import json, sys
payload = json.loads(sys.argv[1])
print(len(payload.get("orders", [])))
PY
)"
  echo "  📦 Shipments：$(python3 - "$shipment_payload" <<'PY'
import json, sys
payload = json.loads(sys.argv[1])
print(len(payload.get("shipments", [])))
PY
)"

  mkdir -p "$(dirname "$REPORT_PATH")"
  START_TS="$(date +%s)"
  python3 - "$REPORT_PATH" "$START_TS" "$response" "$orders_payload" "$shipment_payload" <<'PY'
import json
import sys
import time

report_path, start_ts, response_payload, orders_payload, shipment_payload = sys.argv[1:6]

run_at = int(start_ts)
response = json.loads(response_payload)
orders = json.loads(orders_payload).get("orders", [])
shipments = json.loads(shipment_payload).get("shipments", [])

payload = {
    "mode": "onboarding",
    "started_at": run_at,
    "duration_ms": (int(time.time()) - run_at) * 1000,
    "customer_id": 1,
    "product_id": 1,
    "result": {
        "checkout_code": response.get("code"),
        "checkout_ok": response.get("code") == "checkout_ok",
        "orders_count": len(orders),
        "shipments_count": len(shipments),
    },
    "ready": {
        "catalog_api": True,
        "cart_api": True,
        "checkout_api": True,
        "order_api": True,
        "shipping_api": True,
    },
    "ready_for_demo": True,
}

with open(report_path, "w", encoding="utf-8") as file:
    json.dump(payload, file, ensure_ascii=False, indent=2)
    file.write("\n")
PY
  echo "  🧾 報告已輸出：$REPORT_PATH"
  return 0
}

main() {
  START_TS="$(date +%s)"
  log "1) 先啟動：orbit -c docs/examples/mini-shop/dev.yaml up"
  log "2) 等待下列服務就緒（按服務單位確認）"
  if ! wait_for_ready "$CATALOG_URL" "catalog-api"; then
    FAILURE_REASON="catalog-api_not_ready"
  elif ! wait_for_ready "$CART_URL" "cart-api"; then
    FAILURE_REASON="cart-api_not_ready"
  elif ! wait_for_ready "$CHECKOUT_URL" "checkout-api"; then
    FAILURE_REASON="checkout-api_not_ready"
  elif ! wait_for_ready "$ORDER_URL" "order-api"; then
    FAILURE_REASON="order-api_not_ready"
  elif ! wait_for_ready "$SHIPMENT_URL" "shipping-api"; then
    FAILURE_REASON="shipping-api_not_ready"
  else
    FAILURE_REASON=""
  fi

  if [[ -n "${FAILURE_REASON:-}" ]]; then
    echo
    log "⚠️  首輪未能一次完成，請照以下順序修復："
    log "  - orbit -c docs/examples/mini-shop/dev.yaml status --json"
    log "  - orbit -c docs/examples/mini-shop/dev.yaml logs checkout-api -f"
    log "  - orbit -c docs/examples/mini-shop/dev.yaml logs order-api -f"
    python3 - "$REPORT_PATH" "$START_TS" "$FAILURE_REASON" <<'PY'
import json
import sys
import time

path, start_ts, reason = sys.argv[1], int(sys.argv[2]), sys.argv[3]
payload = {
    "mode": "onboarding",
    "started_at": start_ts,
    "finished_at": int(time.time()),
    "run_status": "failed",
    "ready_for_demo": False,
    "failure_reason": reason,
    "result": {
        "checkout_code": None,
        "checkout_ok": False,
        "orders_count": 0,
        "shipments_count": 0,
    },
    "ready": {
        "catalog_api": None,
        "cart_api": None,
        "checkout_api": None,
        "order_api": None,
        "shipping_api": None,
    },
}
with open(path, "w", encoding="utf-8") as file:
    json.dump(payload, file, ensure_ascii=False, indent=2)
    file.write("\n")
PY
    log "⚠️  失敗報告已寫入：$REPORT_PATH"
    return 1
  fi

  log "3) 跑完 1 次成功流程，驗證關聯"
  if run_success_demo; then
    python3 - "$REPORT_PATH" "$START_TS" <<'PY'
import json
import sys
import time

path, start_ts = sys.argv[1], int(sys.argv[2])
with open(path, "r", encoding="utf-8") as file:
    report = json.load(file)
report["run_status"] = "success"
report["finished_at"] = int(time.time())
with open(path, "w", encoding="utf-8") as file:
    json.dump(report, file, ensure_ascii=False, indent=2)
    file.write("\n")
PY
    log "✅ 新手首輪 demo 可成功：服務就緒 + checkout + order/shipments 有建立"
    echo
    echo "接下來你可以直接複製以下你要的驗證："
    echo "  - 成功路徑（複製）: bash docs/examples/mini-shop/scripts/smoke-compact.sh success"
    echo "  - 失敗路徑（複製）: bash docs/examples/mini-shop/scripts/smoke-compact.sh decline"
    echo "  - 簡版 release check: bash docs/examples/mini-shop/scripts/release-check.sh quick"
    echo
    log "完成。"
    return 0
  fi

  log "⚠️  首輪未能一次完成，請照以下順序修復："
  python3 - "$REPORT_PATH" "$START_TS" <<'PY'
import json
import sys
import time

path, start_ts = sys.argv[1], int(sys.argv[2])
payload = {
    "mode": "onboarding",
    "started_at": start_ts,
    "finished_at": int(time.time()),
    "run_status": "failed",
    "ready_for_demo": False,
    "result": {},
    "ready": {
        "catalog_api": None,
        "cart_api": None,
        "checkout_api": None,
        "order_api": None,
        "shipping_api": None,
    },
}
with open(path, "w", encoding="utf-8") as file:
    json.dump(payload, file, ensure_ascii=False, indent=2)
    file.write("\n")
PY
  log "⚠️  失敗報告已寫入：$REPORT_PATH"
  log "  - orbit -c docs/examples/mini-shop/dev.yaml status --json"
  log "  - orbit -c docs/examples/mini-shop/dev.yaml logs checkout-api -f"
  log "  - orbit -c docs/examples/mini-shop/dev.yaml logs order-api -f"
  return 1
}

main "$@"
