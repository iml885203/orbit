#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-standard}"

case "$MODE" in
  -h|--help|help)
    cat <<'USAGE'
用法：bash docs/examples/mini-shop/scripts/start-mini-shop.sh [standard|compact|advanced]

預設：standard

standard    使用 docs/examples/mini-shop/dev.yaml 並啟動完整 baseline mini-shop（含完整服務）
compact     使用 docs/examples/mini-shop/dev.yaml 啟動 1.0 前最快新手入門流程（同樣可重複 demo）
advanced    使用 docs/examples/mini-shop/dev-advanced.yaml 並啟動 mini-shop-advanced（含觀測與通知）
USAGE
    exit 0
    ;;
  standard|compact|advanced)
    ;;
  *)
    echo "不支援的模式：$MODE" >&2
    echo "請用 standard / compact / advanced（預設 standard）" >&2
    exit 1
    ;;
esac

if [[ "$MODE" == "advanced" ]]; then
  CONFIG="docs/examples/mini-shop/dev-advanced.yaml"
  GROUP="--group mini-shop-advanced"
  PROFILE="進階"
elif [[ "$MODE" == "compact" ]]; then
  CONFIG="docs/examples/mini-shop/dev.yaml"
  GROUP=""
  PROFILE="一鍵入門"
else
  CONFIG="docs/examples/mini-shop/dev.yaml"
  GROUP=""
  PROFILE="標準"
fi

echo "啟動 mini-shop（${PROFILE} 模式）"
if [[ "$MODE" == "compact" ]]; then
  echo "小心智模型啟動路徑：先跑 success/decline 情境，再看可 demo 檢核"
fi
echo "等同命令：orbit -c ${CONFIG} up ${GROUP}"

if [[ -n "$GROUP" ]]; then
  orbit -c "$CONFIG" up "$GROUP"
else
  orbit -c "$CONFIG" up
fi

