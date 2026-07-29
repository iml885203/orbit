# mini-shop v0.5.41（1.0 打磨：advanced 關聯驗收可讀化）

## 本次打磨目標

把進階路徑的「可驗證性」再往前推一步：除了看到關聯事件，還要有可重複確認的指標，避免 review 要靠猜測。

## 這版變更

- `docs/examples/mini-shop/scripts/smoke-p1.sh`
  - `advanced` 套件新增 `check_notification_signal`，在 smoke 中直接驗證 `notification-api` 至少有一筆 `notifications`。
  - `check_observability` 不再只印出 ratio；當 `correlation` 欄位缺失會直接視為 suite 失敗，避免 false positive。
  - `advanced` 套件 readiness 保持包含 `notification-api`，並加入通知事件存在性檢查。

- `docs/examples/mini-shop/README.md`
  - 補充進階用戶可直接檢查 `/notifications` 的閉環驗證說明，降低「怎麼知道真的到通知服務了？」這個認知負擔。

## 打磨目的

這是 1.0 前的「可驗證型 UX」補強：

- `advanced` 不只展示結果，還可以定義「成功」的明確條件。
- 失敗時有更明確的可自查訊號（沒有通知事件會直接 fail）。
- review/release 時能快速分辨「展示有沒有真的把關聯閉環做出來」。
