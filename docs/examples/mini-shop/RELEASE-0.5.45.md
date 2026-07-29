# mini-shop v0.5.45（1.0 打磨：降低新手啟動心智負擔）

## 為什麼這次
在上一版把服務計數改成模式感知後，頁面初始化時還有一個硬編碼/未定義的提示字串，可能在某些瀏覽器環境下直接卡住第一輪操作。  
這版把「啟動初學者訊號」也補齊，讓新手一開場就只看到一致且可理解的目標數。

## 這版做了什麼
- 新增 `BASELINE_RESOURCE_COUNT / ADVANCED_RESOURCE_COUNT`，讓初始化文案走與目標模式一致的資源計數：
  - 新手模式：基線 9 個資源（8 後端服務 + web）
  - 進階模式：11 個資源（進階服務 + web）
- 補齊 `setRunResult` 初始提示，改為 `getTrackedResourceCount(uiMode)`，避免 `TOTAL_RESOURCE_COUNT` 未定義造成啟動中斷。
- 保持 `getTrackedServiceStatusPayload(...)` 作為唯一就緒判斷來源，確保：
  - 一致的「服務 / 交易 / 關聯」三格訊號
  - 新手進度提示（goal/notice/diagnostic）與實際可追蹤服務集合一致。

## 使用者體驗影響
- 首次打開頁面時不會因 `ReferenceError` 中斷腳本，頁面可直接進入新手流程。
- 初始「請先確認 X 個資源就緒」的訊息會隨模式（新手/進階）自動調整，不會再讓使用者困惑於不一致的數字。
- 減少首次啟動的心智負擔，讓人一眼知道「先把這些資源補齊」。

## 驗收
- `make preflight`
- 開啟 mini-shop：觀察頁面是否能正常顯示「準備中」提示、服務卡與 goal 導覽，且不會在 console 出現初始化異常。
