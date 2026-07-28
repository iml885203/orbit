# Orbit v0.0.20 (release polish: drillable failure recovery)

日期：2026-07-29

## 目標

把 mini-shop 的「故障回復」再往下再往前一步：不只說怎麼修，而是給使用者明確「down -> status -> restart」演練流程，降低故障時認知負擔。

## 這版重點（可直接驗證）

### 1) 前端新增「服務 down → 恢復」演練卡

- [docs/examples/mini-shop/apps/web/index.html](../examples/mini-shop/apps/web/index.html)
  - 新增「故障演練（1/2 分鐘）」卡片。
  - 提供三個常用命令的一鍵複製：
    - `down cart-api --json`
    - `status --json`
    - `restart cart-api --json`
  - 增加 `回到 3 秒可 demo 指標` 快速回歸按鈕，讓修完可直接回到成功判斷。

### 2) README 加入故障演練章節

- [docs/examples/mini-shop/README.md](../examples/mini-shop/README.md)
  - 補上故障演練步驟（建立 baseline、down、status、restart、再驗證）。
  - 讓沒有啟用前端 UI 的使用者也能直接知道要做的事。

### 3) Smoke check 反映故障演練

- [docs/examples/mini-shop/UX-SMOKE-CHECK.md](../examples/mini-shop/UX-SMOKE-CHECK.md)
  - 新增「service down 演練」條目，固定檢核：「命令 + 頁面回歸」。
  - 增加 pass/fail check 項：可由演練回到可 demo 狀態。

## 變更匯總

- `docs/examples/mini-shop/apps/web/index.html`
  - 新增故障演練 UI 卡與 copy command 按鈕。
  - 新增 `drill-prime-success`、`drill-verify` 互動行為，與現有 1-鍵流程整合。
- `docs/examples/mini-shop/README.md`
  - 新增「故障演練（服務 down → 恢復）」章節與命令。
- `docs/examples/mini-shop/UX-SMOKE-CHECK.md`
  - 新增 service down 演練與驗收條目。

## 本次驗證

- `make preflight`：待提交後執行
- 變更焦點：以 UX 打磨為主，檢核重點為「能否快速定位 + 快速回歸」而非功能複雜度擴張。

## 1.0 前遞進判斷

- 本輪完成：
  - 把故障修復從「建議」變成「可演練」。
  - 在文檔與 smoke check 中同步演練結果可讀性。
- 下一步建議：
  - 為「重啟服務後的 dashboard 追蹤」補一個截圖/錄影驗收模板（固定時間門檻），讓每輪 release 更容易比較體感差異。
