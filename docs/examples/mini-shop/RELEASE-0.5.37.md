# mini-shop v0.5.37（1.0 打磨：可被 review 的體感差異）

## 本次打磨目標

- 讓第一次打開者不用猜該先做什麼（降低 mental model）
- 保留高階除錯能力，但只在需要時才展開
- 讓 1.0 前每次迭代都有一個可以被驗收的 release 摘要

## 變更明細

### 1) 新手導覽和下一步建議更可執行

- 新增 `docs/examples/mini-shop/apps/web/index.html` 的「第一次 60 秒導覽」卡片
  - 顯示 3 步完成路徑：服務就緒 / 加入購物車 / 完成關聯
  - 即時標示步驟狀態（待做/進行中/完成）
  - 直接提供「前往目前下一步」按鈕，避免新手在頁面中迷路
- 新增「一鍵修復最近錯誤」按鈕
  - 當最近有可識別失敗碼時，優先帶出對應最小修復行為
- `now only 一步` 的導向文本改為「可複製命令 + 可直接對焦卡片」的組合動作

### 2) 進階值可視化與參考決策沉澱

- 新增 `docs/examples/mini-shop/apps/web/index.html` 的「關聯觀測洞察」卡片（進階模式）
  - 一頁顯示訂單 / 出貨關聯指標與事件預覽
  - 讓使用者能在不拆解程式前提下理解跨 service 的 value
- 新增 `docs/examples/mini-shop/dev-advanced.yaml`
  - 加入 `observability-api`，保留 mini-shop 主流程不變
  - 提供 mini-shop-advanced 讓你在演示時可切到更高階的關聯觀測
- 補上 `docs/examples/mini-shop/DEMO-REFERENCE-MATRIX.md`
  - 明確回答「為何不直接用 dotnet/eshop 當主 demo」
  - 鎖定主入口與進階入口的邊界，避免方向漂移

### 3) 1.0 前可重現驗收路徑

- `docs/examples/mini-shop/README.md` 增補 `dev-advanced.yaml` 與觀測 API 的快速驗證
- `docs/examples/mini-shop/scripts/smoke-p1.sh` 與 `UX-SMOKE-CHECK.md` 統一作為發佈前固定 smoke 流程
- 提供 JSON 報告位置，讓每次 release 的通過條件可機械比對

## 對使用者有直接感受的改善（1.0 前）

1. 第一次開啟可先用新手模式，先把最重要三格變綠即可完成 demo；
2. 失敗時先拿到下一步（而不是先看 log）再決定是否展開進階；
3. 要展示「關聯」價值時，可直接切進階看到 order/shipments 關聯指標；
4. 文字、流程、指令都在一個節點可追溯，節奏不會在多頁文件裡散掉；
5. 每次打磨都能留下一則 release note，而不是只靠 commit message「看起來有改」。

## 本版驗收建議

在主線 demo 前先跑：

```bash
bash docs/examples/mini-shop/scripts/smoke-p1.sh mini
```

進階 demo（含觀測）再跑：

```bash
bash docs/examples/mini-shop/scripts/smoke-p1.sh all
```

通過條件至少包含：

- `smoke-summary` 的 `suite_passed` 為 `true`
- 介面上顯示「服務 / 交易 / 關聯」可進入可 demo 狀態
- 遇到 `decline / insufficient_stock` 仍有可直接操作的下一步建議
