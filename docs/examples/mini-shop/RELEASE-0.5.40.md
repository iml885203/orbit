# mini-shop v0.5.40（1.0 打磨：進階版加入「通知關聯」閉環）

## 本次打磨目標

把進階模式的價值從「看到訂單/出貨」進一步拉到「能看到事件全鏈路」：

- checkout 成功後可以產生通知事件
- 觀測服務可以一起彙整「訂單 + 出貨 + 通知」
- advanced 環境在啟動與驗證上可重複檢查這個新關聯點

## 這版變更

- `docs/examples/mini-shop/apps/checkout-api/main.py`
  - 新增可選 `NOTIFICATION_API_URL`。
  - checkout 成功後會嘗試寫入 `notification-api`（若有設定）。
  - `/health` 中新增 `notification_api` 依賴欄位（有設定才檢查）。
  - 回應中新增 `notifications` 小結，給前端/排障知道通知是「已啟用、已要求、已發送」。

- `docs/examples/mini-shop/apps/observability-api/main.py`
  - 加上 `NOTIFICATION_API_URL`，可選抓取通知資料。
  - `/health` 回傳 `notification_api` 依賴狀態（可開可關）。
  - `/insights` 增加通知快照：
    - `totals.notifications`
    - `notifications` 前 10 筆事件
    - `events` 新增 `notification_emitted` 類型，與關聯時間軸共用。

- `docs/examples/mini-shop/dev-advanced.yaml`
  - 將 `notification-api` 納入 `mini-shop-advanced` 組。
  - `checkout-api` 與 `observability-api`、`web` 設定連向 `notification-api`。
  - `mini-shop-advanced` 服務清單同步更新。

- `docs/examples/mini-shop/scripts/smoke-p1.sh`
  - `advanced` 套件新增 `notification-api` 就緒檢查。

- `docs/examples/mini-shop/apps/web/index.html`
  - 「關聯觀測洞察」摘要新增「通知」欄位。
  - 事件清單可顯示通知事件的標題與訊息，讓事件流更容易對齊用戶認知。

- 文件更新
  - `docs/examples/mini-shop/README.md`
    - 更新進階資源清單與觀測洞察說明，補上 notification 的關聯價值。
  - `docs/examples/mini-shop/DEMO-REFERENCE-MATRIX.md`
    - 進階入口策略同步為 `observability + notification`。
  - `docs/examples/mini-shop/UX-ACCEPTANCE-CHECKLIST.md`
    - 增加 notification 關聯的可視化驗收點（可選）。

## 使用者體驗提升重點

- 主流程仍只需「加入購物車 → checkout」即可完成 demo。
- 進階模式會額外看到「關聯事件」中有通知節點，對「這是一個有多個服務合作完成的交易」更有感。
- 不要求所有人開啟 notification；預設 advanced 時可見，降低新手啟動成本。
