#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-all}"
BASE="127.0.0.1"
CUSTOMER_ID=1
PRODUCT_ID=1

ts_echo() {
  echo "[$(date -u +%H:%M:%S)] $*"
}

usage() {
  cat <<'EOF'
用法：
  bash docs/examples/mini-shop/scripts/smoke-demo.sh [all|success|decline|stock]

all      先成功一次，接著失敗路徑、再庫存不足路徑（最完整）
success  成功路徑
decline  付款失敗（mock declined）
stock    庫存不足

預設模式：all
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

run_success() {
  ts_echo "[mini-shop-smoke] 成功下單"
  local resp
  resp=$(curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST)
  echo "[mini-shop-smoke] 清空購物車回應: ${resp:0:160}"
  resp=$(curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
    -H 'Content-Type: application/json' \
    -d '{"product_id":'"$PRODUCT_ID"',"quantity":1}')
  echo "[mini-shop-smoke] 加入品項回應: ${resp:0:160}"
  resp=$(curl -sS "http://$BASE:3006/checkout/$CUSTOMER_ID" \
    -H 'Content-Type: application/json' \
    -d '{"method":"mock_card"}')
  echo "[mini-shop-smoke] 付款成功回應: ${resp:0:240}"
  if [[ -z "$resp" ]]; then
    echo "[mini-shop-smoke][FAIL] checkout 回應為空"
    return 1
  fi
  echo
  echo "[mini-shop-smoke] 訂單/出貨快覽"
  echo "orders:"
  curl -sS "http://$BASE:3002/orders"
  echo
  echo "shipments:"
  curl -sS "http://$BASE:3008/shipments"
  echo
}

run_decline() {
  ts_echo "[mini-shop-smoke] 付款失敗（decline）"
  local resp
  resp=$(curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST)
  echo "[mini-shop-smoke] 清空購物車回應: ${resp:0:160}"
  resp=$(curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
    -H 'Content-Type: application/json' \
    -d '{"product_id":'"$PRODUCT_ID"',"quantity":1}')
  echo "[mini-shop-smoke] 加入品項回應: ${resp:0:160}"
  resp=$(curl -sS "http://$BASE:3006/checkout/$CUSTOMER_ID" \
    -H 'Content-Type: application/json' \
    -d '{"method":"decline"}')
  echo "[mini-shop-smoke] 付款失敗回應: ${resp:0:240}"
  echo
}

run_stock() {
  ts_echo "[mini-shop-smoke] 庫存不足（quantity=999）"
  local resp
  resp=$(curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST)
  echo "[mini-shop-smoke] 清空購物車回應: ${resp:0:160}"
  resp=$(curl -sS "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
    -H 'Content-Type: application/json' \
    -d '{"product_id":'"$PRODUCT_ID"',"quantity":999}')
  echo "[mini-shop-smoke] 加入品項回應: ${resp:0:160}"
  resp=$(curl -sS "http://$BASE:3006/checkout/$CUSTOMER_ID" \
    -H 'Content-Type: application/json' \
    -d '{"method":"mock_card"}')
  echo "[mini-shop-smoke] 庫存不足回應: ${resp:0:240}"
  echo
}

case "$MODE" in
  all)
    run_success
    run_decline
    run_stock
    ;;
  success)
    run_success
    ;;
  decline)
    run_decline
    ;;
  stock)
    run_stock
    ;;
  *)
    echo "不支援模式: $MODE"
    usage
    exit 1
    ;;
esac

echo "[mini-shop-smoke] done"
