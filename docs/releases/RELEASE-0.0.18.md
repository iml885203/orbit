# Orbit v0.0.18 (UX polish for 1.0 preparation)

日期：2026-07-29

## 目標

讓 Orbit 的 mini-shop demo 在 1.0 前達到「第一次打開就能知道下一步」的使用者體驗：

- 少一層「要先看哪裡」的心智負擔。
- 有明確可驗證條件（不是只靠成功/失敗訊息）。
- 故障時能給可執行建議，不是只停在報錯。

## 這版重點（可直接看得到）

### 1) 1.0 打磨用 Demo UX 改造（核心）

- [docs/examples/mini-shop/apps/web/app.html](../examples/mini-shop/apps/web/app.html)
  - 新增「現在只要做一件事」卡片：依目前狀態動態顯示下一步。
  - 新增「第一次 demo（30 秒）」：
    - 開始 demo 一輪
    - 核對可 demo 條件（服務/交易/關聯三格）
    - 快速跳到交付檢核
  - 新增新手/進階檢視切換。
  - 新增「快速判斷值」與「關聯完整性快檢查」。
  - 新增「1.0 交付前檢核」卡片，可點擊導向下一步。
  - 新增一鍵最短重現路徑與可重播流程。
  - 錯誤導向改為可操作建議（包含一鍵修復、焦點定位）。

### 2) 文檔打磨（讓可驗收不靠猜）

- [docs/examples/mini-shop/README.md](../examples/mini-shop/README.md)
  - 版本前可交付心智體驗流程重寫為 30 秒 path。
  - 增加對 1.0 之前每次 release 前的 UX 可見檢查。

- [docs/examples/mini-shop/UX-ACCEPTANCE-CHECKLIST.md](../examples/mini-shop/UX-ACCEPTANCE-CHECKLIST.md)
  - 定義可重複驗收清單（首次可用性、失敗回歸、重置恢復）。

- [docs/examples/mini-shop/UX-SMOKE-CHECK.md](../examples/mini-shop/UX-SMOKE-CHECK.md)
  - 15 分鐘實作導向 smoke check。

### 3) 1.0 計畫對齊

- [docs/1.0-plan.md](../1.0-plan.md)
  - first-run journey 增加「mini-shop demo 成功+失敗回歸」驗收步驟，讓 1.0 前的 UX 目標和主流程一致。

## 變更總覽（Commit）

- `4b98d33` feat: make mini-shop timeline explicit and progressive
- `e28fe53` feat: add attention cues to reduce flow mental model
- `a79d75d` feat: add error playbook focus actions
- `2072b19` feat: prioritize error-card recovery actions
- `d049e51` feat(mini-shop): add 1.0 onboarding UX polish and acceptance checklist
- `6677537` docs(mini-shop): add UX smoke check and tie 1.0 journey to demo outcome

## 本次 release 驗證

- `make preflight` 已通過（含 UI build/lint/test、Go test、go build、go vet、install/test、verify-types、check-neutral）。
- mini-shop 前端腳本 `new Function(...)` 語法 parse pass。
- 主要頁面 DOM 引用 ID 全部對應。

## 還沒完成（1.0 前待續）

- 將這些 UX 改造同步到其他 demo。
- 為 demo smoke check 增加輸出結果標準化格式（可自動歸檔）。
