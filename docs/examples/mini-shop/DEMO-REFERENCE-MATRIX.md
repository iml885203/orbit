# mini-shop 1.0 前 demo 參考矩陣與採納原則

目標是先回應你這個問題：
「是不是要直接把 `dotnet/eshop` 當主 demo？」

先給結論：
- 對 1.0 打磨前，不建議直接採用 `dotnet/eshop` 作為第一個對外入口。
- 先保留 `mini-shop` 為主 demo，並吸收參考專案中對「可觀察」與「故障定位」有價值的那 20%，其餘交給進階場景。

## 參考專案對照（做了哪些可直接借鑒的點）

### 1) [dotnet/eshop](https://github.com/dotnet/eshop)
- 優點
  - 服務拆分邏輯完整：catalog / basket / ordering / payment / shipping / user。
  - 對於「跨服務關聯」這件事，思路成熟。
- 不採用作主入口的原因
  - 初始化複雜度高，第一次新手要先建立大量背景知識。
  - 對 1.0 前「少心智模型」目標干擾較大。

### 2) [GoogleCloudPlatform/microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo)
- 可借鏡
  - 對 trace/觀測、跨服務呼叫路徑視覺化的概念。
- 不做主入口的原因
  - 太接近「平台感示範」，對第一次上手者不夠快。

### 3) [dockersamples/example-voting-app](https://github.com/dockersamples/example-voting-app)
- 可借鏡
  - 前後端 + worker 的輕量混搭。
- 不做主入口的原因
  - 聚焦投票流程，不夠直接對應 Orbit 的「環境/服務就緒 -> 交易閉環」。

## 目前採納策略（針對 mini-shop）

### 主入口（不變）
- `mini-shop` 保持 8 個主 service，目標是 3 分鐘內可完成成功流程：
  - 服務就緒 -> 加入購物車 -> Checkout -> 關聯驗證。

### 進階入口（可選）
- `mini-shop-advanced` 加 `observability-api`（已完成）
  - 只做觀測與關聯快照，不改變 baseline 流程。

### 未來候選（視 1.0 後優先度）
- 可做一個 `mini-shop-ecosystem` 版本，展示更多微服務（例如 notification/recommendation）
  - 當作「多 service 但不壓新手」的高階章節。

## 1.0 前 UX 打磨判斷

優先保留這個順序：
1. **最短可驗證成功**（先把「三色指標」做對）
2. **失敗可修復流程**（有「一鍵」「一頁式命令」）
3. **進階可觀測**（advanced mode）
4. **不一定更多 service，而是更少猜測**

如果你願意，我下一步會直接幫你把這份矩陣轉成「PR 風險清單」
（每項參考專案對 mini-shop 的影響、是否進 1.0、需要多少維護成本）。
