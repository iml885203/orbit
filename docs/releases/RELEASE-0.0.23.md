# Orbit v0.0.23 (release polish: session metric reliability)

日期：2026-07-29

## 目標

讓 "本次 demo 指標" 在使用者重置流程時不再保留舊資料，避免新手誤判。

## 這版重點（可直接驗證）

### 1) 指標卡可重設，避免視覺誤導

- [docs/examples/mini-shop/apps/web/index.html](../examples/mini-shop/apps/web/index.html)
  - 新增 `resetSessionMetrics()`。
  - 在頁面初始化與「重置流程」按鈕後重置以下欄位：
    - 服務就緒起始時間
    - 成功 checkout 起始時間
    - 關聯驗證時間
  - 讓每個 demo session 開始都回到可預期的空狀態。

### 2) 交付 checklist 納入 session metric

- [docs/releases/RELEASE-CHECKLIST-TEMPLATE.md](./RELEASE-CHECKLIST-TEMPLATE.md)
  - 新增驗收欄位：服務就緒、checkout、關聯耗時必填，讓 release review 時可比對可交付一致性。

## 變更匯總

- `docs/examples/mini-shop/apps/web/index.html`
  - 新增 session 指標重置機制
  - 重置流程將指標改為「本次 session 的空白起點」
- `docs/releases/RELEASE-CHECKLIST-TEMPLATE.md`
  - 補充 session 指標交付欄位

## 本次驗證

- `make preflight`：待提交後執行
- 重點驗證：
  - 點「重置流程」後，本次 demo 指標恢復為未完成狀態，且不再顯示舊 session 的已完成計時文字。

## 1.0 前遞進判斷

- 本輪完成：把看起來像「功能完成」但其實會誤導的狀態同步，提升新手可預測性。
- 下一步建議：持續補齊 release note 範本欄位與實際測量值，形成一套可追溯的 UX 里程碑。
