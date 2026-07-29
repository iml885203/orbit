#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-standard}"

case "$MODE" in
  -h|--help|help)
    cat <<'USAGE'
用法：bash docs/examples/mini-shop/scripts/start-mini-shop.sh [standard|advanced]

預設：standard

standard    使用 docs/examples/mini-shop/dev.yaml 並啟動 baseline mini-shop
advanced    使用 docs/examples/mini-shop/dev-advanced.yaml 並啟動 mini-shop-advanced（含觀測與通知）
USAGE
    exit 0
    ;;
  standard|advanced)
    ;;
  *)
    echo "不支援的模式：$MODE" >&2
    echo "請用 standard 或 advanced（預設 standard）" >&2
    exit 1
    ;;
esac

if [[ "$MODE" == "advanced" ]]; then
  CONFIG="docs/examples/mini-shop/dev-advanced.yaml"
  GROUP="--group mini-shop-advanced"
  PROFILE="進階"
else
  CONFIG="docs/examples/mini-shop/dev.yaml"
  GROUP=""
  PROFILE="標準"
fi

echo "啟動 mini-shop（${PROFILE} 模式）"
echo "等同命令：orbit -c ${CONFIG} up ${GROUP}"

if [[ -n "$GROUP" ]]; then
  orbit -c "$CONFIG" up "$GROUP"
else
  orbit -c "$CONFIG" up
fi

