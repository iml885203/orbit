# Orbit mini-shop Release 0.5.31

## 本次目標

排除新手在 demo 流程中的「看得到但不能用」體驗問題，讓報告與建議功能能穩定執行，降低操作阻力。

## 主要變更

- `docs/examples/mini-shop/apps/web/index.html`
  - 修正 `buildDemoReport()` 中未定義變數導致的執行錯誤：
    - `allServiceReady` 改用 `snapshot.allServiceReady`
    - `latestOrder` 改由 `snapshot.latestOrder` 提供
  - 影響的行為：
    - 「複製 demo 報告」「故障回歸報告」不再因前端 Runtime Reference Error 卡住
    - 報告內容回覆穩定，可正確顯示「服務/交易/關聯」建議

## 使用者價值

- 小而直接：修掉報告功能壞掉的情境，避免新手因 copy 按鈕失效而誤判工具不好用。
- 心智負擔更低：功能按鈕在失敗情境下仍能回傳可執行結果，流程更可預期、更容易追蹤。

## 驗收建議

- 進入 mini-shop（`orbit -c docs/examples/mini-shop/dev.yaml up`）
- 在頁面操作一次成功/失敗情境後，按下：
  - `複製 demo 報告`
  - `複製故障回歸報告`
- 預期：
  - 不會出現報告按鈕無回應
  - 可直接貼上到終端機做保存或分享
