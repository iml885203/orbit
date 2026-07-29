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
  - 你可以先借它的「產品/訂單/付款/出貨」邏輯拆分做 UI 文字邏輯，不一定要複製專案結構。

### 2) [dotnet-architecture/eShopOnWeb](https://github.com/dotnet-architecture/eShopOnWeb)
- 優點
  - 單一應用為主、但邏輯分層完整，適合借「場景流程命名」與「報錯文字」。
- 可借鑒重點
  - 對使用者說明「在哪個流程卡住」比直接暴露 service 名稱更友善。
- 不做主入口的原因
  - 它不是以本地多服務環境為中心，會增加 1.0 前的啟動成本。

### 3) [GoogleCloudPlatform/microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo)
- 可借鏡
  - 對 trace/觀測、跨服務呼叫路徑視覺化的概念。
- 不做主入口的原因
  - 太接近「平台感示範」，對第一次上手者不夠快。

### 4) [dockersamples/example-voting-app](https://github.com/dockersamples/example-voting-app)
- 可借鏡
  - 前後端 + worker 的輕量混搭。
- 不做主入口的原因
  - 聚焦投票流程，不夠直接對應 Orbit 的「環境/服務就緒 -> 交易閉環」。

### 5) [microservices-demo by Google + OpenTelemetry 示例（可直接借思路）](https://github.com/GoogleCloudPlatform/microservices-demo)
- 可借鏡
  - 如何把「請求跨服務路徑」轉成可視覺化指標。
- 注意
  - 這類專案在 1.0 前可作觀察性章節素材，不宜當新手唯一主入口。

### 6) [weaveworks/awesome-microservices](https://github.com/weaveworks/awesome-microservices)
- 可借鏡
  - 可快速比對不同 demo 的 service 數量/啟動複雜度，幫你挑合適的 1.0 前入口。
- 注意
  - 主要是索引，不是直接可複製 repo。

## 目前採納策略（針對 mini-shop）

### 主入口（不變）
- `mini-shop` 保持 8 個主 service，目標是 3 分鐘內可完成成功流程：
  - 服務就緒 -> 加入購物車 -> Checkout -> 關聯驗證。

### 進階入口（可選）
- `mini-shop-advanced` 加 `observability-api` + `notification-api`（已完成）
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

## 為你這個問題的直接答案：mini-shop 太簡單嗎？

不是太簡單。  
它已經有：

- 多個後端服務（host）
- 服務依賴鏈（`checkout` 串接 catalog / cart / order / payment / shipping）
- 本地持久化（多個 SQLite）
- 前端 demo 流程和失敗回歸
- 關聯可驗證（訂單與出貨是否對上）

如果你要再往上提質感，建議不是再加更多服務，而是補上「同一筆交易」能被新手看懂的敘事段落，例如：

- 開始前：先說「這筆訂單要完成三件事」；
- 成功後：同時展示「Order ID、Shipment ID、時間軸」；
- 失敗後：直接對應到 1 個可修復卡片（例如 payment_declined、insufficient_stock）。

這樣比「更複雜」更能降低心理模型，因為它把複雜度轉成了可預測、可修復的步驟。

如果你願意，我下一步會直接幫你把這份矩陣轉成「PR 風險清單」
（每項參考專案對 mini-shop 的影響、是否進 1.0、需要多少維護成本）。
