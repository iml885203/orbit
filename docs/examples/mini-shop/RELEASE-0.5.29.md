# Orbit mini-shop Release 0.5.29

## 本次目標

讓第一次 demo 的「terminal 驗收」也能做到同樣一鍵化，降低新手還要記一長串 curl 腳本的心智成本。

## 主要變更

- 新增 `docs/examples/mini-shop/scripts/smoke-demo.sh`
  - 支援四種模式：`all`（預設）、`success`、`decline`、`stock`
  - 將成功、付款失敗、庫存不足三條主流程封成一個穩定命令
  - 成功模式會列印 orders / shipments 快覽，便於快速對照結果
- `docs/examples/mini-shop/apps/web/index.html`
  - 在「第一次 demo（30 秒）」增加「複製 smoke demo 腳本命令」按鈕
  - 讓使用者可以直接貼上 `bash docs/examples/mini-shop/scripts/smoke-demo.sh all` 取得可重複驗收節奏
- `docs/examples/mini-shop/README.md`
  - 補上 smoke 腳本執行方式，提供完整、可重複執行的命令提示

## 產品打磨觀點

- 仍舊保留原有「一鍵成功→核對」與故障演練邏輯，新增了終端機路徑，讓沒有 GUI 的 reviewer 也能在 CI 前置驗收。
- 這版重點不是更多功能，而是「一次命令就知道是否可 demo」的可預測體驗，進一步降低第一次試用者的認知負擔。

## 下一步

- 導入對應的 `assert` 驗證輸出（可在腳本回傳非零值），讓這個 demo 能直接被自動化測試流程消化。
