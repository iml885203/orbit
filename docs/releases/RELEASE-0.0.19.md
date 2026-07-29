# Orbit v0.0.19 (release polish: failure recovery path & release visibility)

日期：2026-07-29

## 目標

加快 1.0 之前的交付可見性，讓每次 release 對使用者體驗的改善不是「看起來有改」，而是「可觀測到更順手」。

## 這版重點（可直接驗證）

### 1) 將 demo 打磨決策固定成可追溯文件

- [docs/examples/mini-shop/DEMO-STRATEGY.md](../examples/mini-shop/DEMO-STRATEGY.md)
  - 明確回答「mini-shop 是否太簡單」與為何保留為主 demo。
  - 定義 P0 / P1 / P2 路徑：先保留主流程（低心智）再逐步加進階場景。
  - 明確列出避免方向（避免把首次體驗變成配置負擔）。

### 2) 打磨故障回復的使用者心智模型

- [docs/examples/mini-shop/README.md](../examples/mini-shop/README.md)
  - 新增「失敗後最短回復」段落：
    1. `status --json` 定位紅色節點
    2. 對應 service `logs -f`
    3. 回到一鍵 demo flow 重試
  - 把故障排查從「猜」變成「固定路徑」，降低新手第一反應負擔。

### 3) 建立 release 交付模板（pre-1.0）

- [docs/releases/RELEASE-CHECKLIST-TEMPLATE.md](./RELEASE-CHECKLIST-TEMPLATE.md)
  - 每次 release 前必填的 UX 交付欄位：30 秒可行動性、失敗修復、可驗證結果。
- [docs/releases/RELEASE-README.md](./RELEASE-README.md)
  - release 文件存放與流程統一入口。
- [docs/1.0-plan.md](../1.0-plan.md)
  - 將「demo 戰鬥力策略」納入 v1.0 release gate，防止 1.0 前回歸時把方向錯掉。

### 4) 文件一致性與可執行性

- [docs/examples/mini-shop/UX-SMOKE-CHECK.md](../examples/mini-shop/UX-SMOKE-CHECK.md)
  - 修正文案一致性（`decline` 情境），保持失敗回歸流程清晰。

## 變更匯總

- `131386e` docs: add demo strategy and release polish checklist for 1.0 UX
- 本次 release 補做：`docs/examples/mini-shop/UX-SMOKE-CHECK.md` typo 修正

## 本次驗證

- `make preflight` 已通過（含 UI build/lint/test、Go test、go build、go vet、install flow、verify-types、check-neutral）。
- 目標是讓 `release note`、UX checklist、demo strategy 在同一入口可追溯，不再只靠 commit message。

## 1.0 前遞進判斷

本次不是終版，為下一步提供明確「繼續做什麼」：

- 本輪完成：
  - 讓使用者在卡住時有固定排障路徑。
  - 讓每次 release 有可見 UX 交付摘要。
- 未完成（建議下一輪優先）：
  - 在 mini-shop 加入一個「一個 service down」最小故障演練（可手動/按鈕重啟）。
  - 讓 smoke 檢查的結果可附「通過/未通過 + 耗時」直接貼到 release note。
