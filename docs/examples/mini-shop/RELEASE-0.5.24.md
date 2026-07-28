# Orbit Mini-Shop (0.5.24) — 1.0 前最終 UX 驗收稿

## 目標

把「可以打磨了」變成「可交付了」：新增可複製且可驗證的 1.0 前驗收路徑，讓每個人只需兩件事就能判斷這個 demo 是否準備好。

## 1.0 前可公開驗收流程（新手友善）

1. 開啟 demo
   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml up
   ```
2. 快速狀態對帳
   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml status --json
   ```
   - 期待：`checkout-api` 與前段依賴可見為 ready（服務頁面看到 `8/8` ready）。
3. 開啟頁面並按：`開始 demo 一輪`
4. 按：`核對可 demo 條件`
   - 期待：`快速判斷值（3 秒）` 三格同時為綠。
5. 檢查結果：最近關聯交易有 `confirmed` 訂單且 `關聯完整性快檢查` 綠色。

## 最短回歸流程（故障演練）

直接複製下列腳本進 terminal：

```bash
orbit -c docs/examples/mini-shop/dev.yaml down cart-api --json
orbit -c docs/examples/mini-shop/dev.yaml status --json
orbit -c docs/examples/mini-shop/dev.yaml restart cart-api --json
orbit -c docs/examples/mini-shop/dev.yaml status --json
```

- 回到頁面點：`回到 3 秒可 demo 指標`
- 期待：可以在 120 秒內恢復 `快速判斷值` 全綠。

## 本版新增（與 0.5.23 對比）

- 新增「故障演練腳本複製」：減少新手在 `down`/`status`/`restart` 間切換命令的認知負擔。
- 文件化 1.0 前驗收路徑：把每次 release 的判定標準固定為可執行清單。
- 讓驗收結果從描述型變成可操作型（命令 + 時間 + 三格綠燈）。

## 驗證記錄（每次 release 前應填）

- 成功路徑三格綠：
  - 服務（8/8）
  - 交易（mock_card checkout）
  - 關聯（訂單與出貨對應）
- 故障回歸耗時（分鐘）：`____` 秒
- 是否仍可快速定位失敗服務：`是 / 否`
- 是否可以不切換到 CLI 深度命令就回到「一眼可判斷」狀態：`是 / 否`

## 與 1.0 對齊重點

- 可預測第一步（不猜）
- 可複製修復步驟（不需自己拼命令）
- 可視覺驗證可 demo（不是只看文案）
