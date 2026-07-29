#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE_URL="127.0.0.1"
COMPACT_CFG="docs/examples/mini-shop/dev.yaml"
REPORT_DIR="/tmp/mini-shop-smoke-reports"
LOCK_FILE="/tmp/mini-shop-compact-onboarding.pid"
ORB_LOG="/tmp/mini-shop-compact-orbit.log"

MODE="${1:-run}"

if [[ "${MODE}" == "help" || "${MODE}" == "-h" || "${MODE}" == "--help" ]]; then
  cat <<'USAGE'
用途：為 1.0 前新手提供最短可重複流程。

用法：bash docs/examples/mini-shop/scripts/compact-onboarding.sh [run|smoke-only]

預設 run：
  1) 以背景啟動 mini-shop（compact）
  2) 等候關鍵服務就緒（需回應 ok）
  3) 跑 success / decline smoke（compact）
  4) 輸出可貼到 release note 的報告路徑

smoke-only：只跑 success / decline 檢核（不啟動 orbit）
USAGE
  exit 0
fi

if [[ "${MODE}" != "run" && "${MODE}" != "smoke-only" ]]; then
  echo "不支援的模式：${MODE}" >&2
  echo "請用 run 或 smoke-only" >&2
  exit 1
fi

check_http_ok() {
  local url="$1"
  local name="$2"
  local payload

  if ! payload="$(curl -fsS "$url" 2>/dev/null)"; then
    return 1
  fi

  if python3 - "$url" "$name" "$payload" <<'PY'
import json
import sys

name = sys.argv[2]
raw = sys.argv[3]

if name in {"cart-api", "inventory-api", "customer-api", "order-api", "catalog-api", "shipping-api", "checkout-api", "payment-api", "observability-api", "notification-api"}:
    try:
        data = json.loads(raw)
    except Exception:
        sys.exit(1)
    if data.get("status") != "ok":
        sys.exit(1)
print("ok")
PY
then
    return 0
  fi

  return 1
}

run_smoke_or_exit() {
  local report_path="${REPORT_DIR}/compact-summary.json"
  bash docs/examples/mini-shop/scripts/smoke-compact.sh all
  local smoke_exit=$?
  if [[ -f "$report_path" ]]; then
    python3 - "$report_path" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1], 'r', encoding='utf-8'))
print('smoke 未完全通過，以下是建議：')
success = payload.get('scenarios', {}).get('success', {})
decline = payload.get('scenarios', {}).get('decline', {})
if success.get('next_action'):
    print('  - success:', success['next_action'])
if decline.get('next_action'):
    print('  - decline:', decline['next_action'])
PY
  fi
  return "$smoke_exit"
}

echo "[mini-shop-compact-onboarding] mini-shop 1.0 前新手流程啟動"

echo "[mini-shop-compact-onboarding] 第 1 步：${MODE} 模式檢核"
echo "  - compact 命令：bash docs/examples/mini-shop/scripts/start-mini-shop.sh compact"
echo "  - compact smoke：bash docs/examples/mini-shop/scripts/smoke-compact.sh all"

echo "[mini-shop-compact-onboarding] 第 2 步：啟動環境"
if [[ "${MODE}" == "run" ]]; then
  cd "$ROOT_DIR"
  if [[ -f "$LOCK_FILE" ]]; then
    old_pid="$(cat "$LOCK_FILE")"
    if [[ -n "$old_pid" ]] && kill -0 "$old_pid" >/dev/null 2>&1; then
      echo "偵測到既有背景 orbit 進程：pid=${old_pid}，將沿用該進程。"
    else
      rm -f "$LOCK_FILE"
    fi
  fi

  if [[ ! -f "$LOCK_FILE" ]]; then
    nohup bash docs/examples/mini-shop/scripts/start-mini-shop.sh compact >"$ORB_LOG" 2>&1 &
    echo "$!" >"$LOCK_FILE"
    echo "[mini-shop-compact-onboarding] 背景啟動 mini-shop：pid=$(cat "$LOCK_FILE"), log=${ORB_LOG}"
    echo "  - 可用尾行觀測：tail -f ${ORB_LOG}"
  fi
else
  echo "[mini-shop-compact-onboarding] smoke-only 模式，不啟動 orbit。"
fi

echo "[mini-shop-compact-onboarding] 第 3 步：等待關鍵服務（最多 20 秒）"
echo "  - 若服務長時間未 ready，先執行：orbit -c ${COMPACT_CFG} status --json"

wait_seconds=0
ready=1
while (( wait_seconds < 20 )); do
  ready=1
  for svc in catalog-api cart-api checkout-api order-api shipping-api; do
    url="http://$BASE_URL:"
    case "$svc" in
      catalog-api) url+="3001/health" ;;
      cart-api) url+="3005/health" ;;
      checkout-api) url+="3006/health" ;;
      order-api) url+="3002/health" ;;
      shipping-api) url+="3008/health" ;;
    esac
    if ! check_http_ok "$url" "$svc"; then
      ready=0
      break
    fi
  done

  if [[ "$ready" == "1" ]]; then
    echo "[mini-shop-compact-onboarding] 關鍵服務已回應且 ready"
    break
  fi

  echo "[mini-shop-compact-onboarding] 還在等待，就緒度：$((wait_seconds+1))/20"
  sleep 1
  wait_seconds=$((wait_seconds + 1))
done

if [[ "$ready" != "1" ]]; then
  echo "[mini-shop-compact-onboarding] 20 秒後部分服務仍未 ready，建議先做：" >&2
  echo "  1) orbit -c ${COMPACT_CFG} status --json" >&2
  echo "  2) tail -f ${ORB_LOG}" >&2
  echo "  3) orbit -c ${COMPACT_CFG} up" >&2
fi

echo "[mini-shop-compact-onboarding] 第 4 步：跑 compact smoke（success + decline）"
if ! run_smoke_or_exit; then
  echo "[mini-shop-compact-onboarding] smoke 未全綠，建議先依上方建議修復再重跑。" >&2
else
  echo "[mini-shop-compact-onboarding] smoke 直接驗證通過。"
fi

report="${REPORT_DIR}/compact-summary.json"
if [[ -f "$report" ]]; then
  echo "[mini-shop-compact-onboarding] 報告：${report}"
  python3 - "$report" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1], 'r', encoding='utf-8'))
print('[mini-shop-compact-onboarding] 版本摘要：')
print('  suite_passed=', payload.get('suite_passed'))
print('  success=', payload.get('scenarios', {}).get('success', {}).get('passed'))
print('  decline=', payload.get('scenarios', {}).get('decline', {}).get('passed'))
print('  hints=', payload.get('scenarios', {}).get('decline', {}).get('next_action'))
print('  started_at=', payload.get('started_at'))
print('  duration_seconds=', payload.get('duration_seconds'))
PY
else
  echo "[mini-shop-compact-onboarding] warning: 沒有找到 ${report}，請再跑一次 smoke-compact。" >&2
fi

echo "[mini-shop-compact-onboarding] 完成。建議下一步："
echo "  - 在 web 點『一輪 demo 結論』檢查是否已綠。"
echo "  - 若有失敗，先點『一鍵修復最近錯誤』或『先看核對清單』。"
