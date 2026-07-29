# mini-shop Release 0.5.62

日期：2026-07-29

## 這版重點

- 為 1.0 前新手體驗新增「2 分鐘導覽腳本」：
  - `docs/examples/mini-shop/scripts/onboarding-walkthrough.sh`
  - 內容包含：起步指令、核心服務 ready 檢核、一次成功 checkout 驗證、下一步驗證建議。

## 1.0 UX 對齊

本次目標是縮短第一次理解成本，讓使用者：
- 不用記憶多服務關係，也能在 2 分鐘內看到關聯結果；
- 先靠機器化流程確認 baseline 成功，避免直接面對前端操作而卡住；
- 失敗時直接給固定修復順序，降低試錯認知成本。

## 影響檔案

- `docs/examples/mini-shop/scripts/onboarding-walkthrough.sh`（新增）
- `docs/examples/mini-shop/README.md`（新增 2 分鐘新手導覽）

