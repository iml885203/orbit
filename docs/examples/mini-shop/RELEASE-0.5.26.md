# Orbit mini-shop Release 0.5.26

本次聚焦目標：再降一層心智模型，讓失敗情境也能有「清楚可預期」的行動。

## 主要變更

- 新增「命令列示範腳本」(情境 A/B/C) 入口：
  - 情境 A：成功下單
  - 情境 B：付款失敗
  - 情境 C：庫存不足
  - 位置：`docs/examples/mini-shop/apps/web/index.html` 的 `第一次 demo（30 秒）` 區塊
  - 也同步補到 `docs/examples/mini-shop/README.md` 的「命令列示範腳本」節點
- `README` 增補「三種示範腳本」的使用順序建議：A（基線）→ B/C（失敗）
- `DEMO-STRATEGY` 明確化 `dotnet/eshop` 與 mini-shop 的角色：
  - mini-shop 當主線，新手 30 秒可上手
  - 避免把複雜架構當作初始 entry
- `UX-SMOKE-CHECK` 與 `UX-ACCEPTANCE-CHECKLIST` 加入腳本化驗證對齊：
  - 不只 UI 一鍵，也可用可複製腳本做同樣核對

## 驗收結果（預期）

- 失敗前已知「可做什麼」：使用者不用猜下一步，能在第一畫面就找到對應場景。
- 命令列腳本與 UI 情境達到一致結果：
  - 成功有訂單+出貨
  - 付款失敗與庫存不足可直接看到錯誤訊息
- 1.0 前 UX 迴圈更完整：
  - 進入 → 成功 → 失敗 → 回歸，重複率高、學習成本低
