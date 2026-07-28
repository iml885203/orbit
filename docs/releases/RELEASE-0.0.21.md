# Orbit v0.0.21 (release polish: drill runtime feedback)

日期：2026-07-29

## 目標

讓打磨結果更容易比較：在故障演練中加入「耗時回饋」，不是只做功能，而是能知道本輪是否真的變快、變順。

## 這版重點（可直接驗證）

### 1) mini-shop UI 追加演練時間回饋

- [docs/examples/mini-shop/apps/web/index.html](../examples/mini-shop/apps/web/index.html)
  - 故障演練的「先做一輪成功流程」與「回歸驗證」現在會回報耗時。
  - 透過 copy-feedback/操作摘要可看到 `一鍵流程耗時` 與 `回歸時間`，讓每輪 release 有可觀察差異。

### 2) Smoke check 改成可比較格式

- [docs/examples/mini-shop/UX-SMOKE-CHECK.md](../examples/mini-shop/UX-SMOKE-CHECK.md)
  - 新增可記錄欄位：基線耗時、故障後回歸耗時。
  - 固定目標值：基線 < 15 秒、回歸 < 120 秒。

### 3) README 補上量化建議

- [docs/examples/mini-shop/README.md](../examples/mini-shop/README.md)
  - 在故障演練段落新增「基線 / 回歸耗時」欄位，讓 demo 團隊可持續衡量體驗。

## 變更匯總

- `docs/examples/mini-shop/apps/web/index.html`
  - `drillState` 狀態追蹤：基線完成時間、回歸核對時間。
  - `runOneShotSuccessFlow()` 回傳流程成功與否，並回報耗時。
  - `故障演練` 按鈕回報回歸耗時到摘要／copy feedback。
- `docs/examples/mini-shop/UX-SMOKE-CHECK.md`
  - 增加測量欄位與跨 release 比較門檻。
- `docs/examples/mini-shop/README.md`
  - 加入故障演練量化建議。

## 本次驗證

- `make preflight`：待提交後執行
- 重點驗證：
  - 操作按鈕行為不改變成功流程，但能補上「多久回到可 demo」可測指標。

## 1.0 前遞進判斷

- 本輪完成：把「可感知的可用性」變成「可衡量的用時」，繼續壓低新手認知負擔。
- 下一步建議：把這些欄位接到 release 的模板輸出，讓每版都會附上耗時數值（而不只是文字）。
