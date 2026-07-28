# mini-shop v0.4.0 (pre-1.0 polish milestone)

## 目標
在 1.0 前驗證一版可感知價值的 demo，核心是讓使用者用很少心智模型就能理解 Orbit 的價值：

- Host process / container 混合
- 多服務依賴可視覺化
- 一條完整「購物與結帳」流程可直接操作

## 本次完成項目

- 新增服務（4）
  - `customer-api`：客戶資料（可選客戶）
  - `cart-api`：購物車（前置流程）
  - `payment-api`：模擬付款（可成功/失敗）
  - `shipping-api`：模擬出貨（tracking）
  - `checkout-api`：checkout 編排（cart -> inventory -> payment -> order -> shipping）

- 新增前端流程
  - 客戶選擇
  - 加入購物車
  - Checkout 按鈕（含付款方式切換）
  - 訂單 + 出貨結果回傳
  - 各服務健康狀態卡（可直接看 ready 狀態）
  - 失敗路徑可行動提示

- 環境/文件更新
  - `docs/examples/mini-shop/dev.yaml` 更新為 9 個 resource
  - `docs/examples/mini-shop/README.md` 重寫為新流程 onboarding 與驗收步驟
  - `docs/examples/mini-shop/apps/web/main.py` 擴充所有新 endpoint 的 env 注入

## 對 1.0 之前 UX 打磨的價值

- 降低心智模型：使用者只需關心「客戶→購物車→Checkout」，不用理解每個 service 的內部呼叫。
- 明確進度：健康卡 + 明確按鈕啟用狀態，避免盲目點按。
- 可見回饋：付款失敗/庫存不足/服務未就緒都有對應文案，不只是 500。
- 可驗證性：從 `orbit status --json`、前端訊息、service logs 可快速對齊問題點。

## 版本建議

- 這是可發布為 0.x 的里程碑版本（例如 `0.4.0`）
- 目標是先讓使用者在 demo 層面「快速知道 Orbit 有用」；到 1.0 再合併 release 機制與 CLI/UX 收斂。
