# Orbit v0.0.22 (release polish: session-level clarity signals)

日期：2026-07-29

## 目標

讓 1.0 前 demo 不只「有新功能」，而是有「用戶一眼能看懂我現在做了什麼」的可見狀態。

## 這版重點（可直接驗證）

### 1) mini-shop 追加「本次 demo 指標」卡

- [docs/examples/mini-shop/apps/web/index.html](../examples/mini-shop/apps/web/index.html)
  - 新增 `本次 demo 指標` panel，集中顯示三個 1.0 核心條件：
    - 服務就緒耗時
    - checkout 成功耗時（從服務就緒到第一筆成功）
    - 關聯驗證耗時（從成功到關聯完成）
  - 目標可交付與回復可見性字串一起展示，讓使用者知道下一步要補哪一步。

### 2) 介面文字對齊「少猜」原則

- [docs/examples/mini-shop/README.md](../examples/mini-shop/README.md)
  - 補上新指標段落入口描述，讓首次使用者知道要關注什麼，不必先猜。

## 變更匯總

- `docs/examples/mini-shop/apps/web/index.html`
  - 新增 session metrics 視覺模組（`#session-metrics-card`）與樣式。
  - 新增 `sessionMetricsState` 與 `setSessionMetrics(...)`，於 `setAcceptanceChecklist()` 重算後更新。
  - 將 1.0 可交付條件集中成一張 panel，減少頁面分散式判讀。
- `docs/releases/RELEASE-README.md`
  - 新增 0.0.22 release note 條目。

## 本次驗證

- `make preflight`：待提交後執行
- 重點驗證：
  - 首次啟動時，頁面可看到 `本次 demo 指標`，且隨著服務就緒、成功 checkout、關聯完成逐步變綠。
  - 進度條件變更時，指標 panel 內容即時更新。

## 1.0 前遞進判斷

- 本輪完成：把「能不能 demo」從隱藏在多卡狀態裡，改成集中、可閱讀、一眼可決定下一步。
- 下一步建議：把 `本次 demo 指標` 的「服務就緒 / checkout / 關聯」數據綁定到 release 驗收表單，自動生成對比。
