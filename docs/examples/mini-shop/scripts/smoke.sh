#!/usr/bin/env bash

set -euo pipefail

base_url="${MINI_SHOP_BASE_URL:-http://127.0.0.1}"
customer_id="${MINI_SHOP_CUSTOMER_ID:-1}"
product_id="${MINI_SHOP_PRODUCT_ID:-1}"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

request() {
  curl --fail-with-body --silent --show-error "$@"
}

for port in 3001 3002 3003 3004 3005 3006 3007 3008; do
  request "$base_url:$port/health" >/dev/null
done
echo "✓ all eight APIs are healthy"

request -X POST "$base_url:3005/carts/$customer_id/clear" >/dev/null
request \
  -H 'Content-Type: application/json' \
  -d "{\"product_id\":$product_id,\"quantity\":1}" \
  "$base_url:3005/carts/$customer_id/items" >/dev/null
request \
  -H 'Content-Type: application/json' \
  -d '{"method":"mock_card"}' \
  "$base_url:3006/checkout/$customer_id" >"$test_root/success.json"

python3 - "$test_root/success.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as response:
    payload = json.load(response)

orders = payload.get("orders", [])
shipments = payload.get("shipments", [])
assert payload.get("status") == "ok", payload
assert len(orders) == 1, payload
assert len(shipments) == 1, payload
assert shipments[0]["order_id"] == orders[0]["id"], payload
print(f"✓ order #{orders[0]['id']} links to shipment {shipments[0]['tracking_no']}")
PY

request -X POST "$base_url:3005/carts/$customer_id/clear" >/dev/null
request \
  -H 'Content-Type: application/json' \
  -d "{\"product_id\":$product_id,\"quantity\":1}" \
  "$base_url:3005/carts/$customer_id/items" >/dev/null
status="$(
  curl --silent --show-error \
    --output "$test_root/decline.json" \
    --write-out '%{http_code}' \
    -H 'Content-Type: application/json' \
    -d '{"method":"decline"}' \
    "$base_url:3006/checkout/$customer_id"
)"

python3 - "$status" "$test_root/decline.json" <<'PY'
import json
import sys

status = int(sys.argv[1])
with open(sys.argv[2], encoding="utf-8") as response:
    payload = json.load(response)

assert status == 402, (status, payload)
assert payload.get("code") in {
    "payment_declined",
    "forced_decline",
    "insufficient_funds",
    "payment_failed",
}, payload
print(f"✓ declined payment stops safely with {payload['code']}")
PY

echo "mini-shop smoke passed"
