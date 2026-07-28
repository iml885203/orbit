# Mini-shop demo 策略（Orbit 1.0 前）

## 先回答你最初的問題：現在這個 demo 太簡單嗎？

**結論：不算太簡單。**

- 已有 `host + container` 混合環境（`redis` container + 多個 Python host services）
- 有 9 個 resource，有清楚 dependency chain（`depends_on`）
- 有跨服務關聯驗證（cart/order/shipping）
- 有明確失敗情境（付款失敗、缺貨）
- 有「下一步」與排錯指令導向，已比一般 demo 僅「可用」更接近「好上手」

但還可以再往「更符合 Orbit 價值」推一層：

- 不只「看起來有很多服務」，而是**每輪操作都能一眼判斷成敗與下一步**（mental model 極低）
- 不只 demo 成功，還要讓使用者能把「修復」也做成一條流程

## 為什麼用 mini-shop 當主 demo（優先）

相比直接借 `dotnet/eshop`、`eShopOnContainers` 這類企業級專案：

- 目前目標是本地新手 30–120 秒上手，而不是「學一套完整平台」
- mini-shop 可在 `orbit -c ... up` 後就能操作，能直接驗證 Orbit 的價值點：
  - 多服務協調
  - 可預測啟動順序與健康檢查
  - host/container 混部署
  - 失敗快速定位到某個 service

## 推薦的 1.0 前 demo 演進路徑（按價值排序）

### P0（立即保留）
- 保持 mini-shop 為主要可 demo 案例
- 「第一次只做一件事」流程不變：
  - 準備完成檢查（3 綠格）
  - 一鍵成功路徑
  - 一鍵失敗回歸（付款/缺貨）
- 每次 release 前都在 `UX-SMOKE-CHECK` + `UX-ACCEPTANCE-CHECKLIST` 有紀錄

### P1（本輪可直接加）
- 增加一個「最小故障演練」片段（例如：停掉單一 service，再重啟）
  - 目標：驗證新手不用猜，能直接跟著頁面建議修復
- README 與指令頁補上「失敗後最短回復流程」
  - 例如：`status -> 看到一個被阻塞節點 -> 去 logs -> 重啟該節點`

### P2（觀察期）
- 可再增加一個「更偏混合語言」的 profile（但不作為預設 demo）
  - 只在進階章節提供：展示 `orbit switch` / env 切換價值
  - 維持 mini-shop 作為主線，避免把第一印象變成環境維護負擔

### 避免的方向
- 不要直接上完整 `eShopOnContainers` 作為唯一主 demo
  - 會把第一體驗從「會用」推向「先熟悉專案結構」
  - 對於 1.0 前目標（低 mental model）不友善

## 可考慮的外部參考（只用於設計，不複製全部）

- [dotnet/eshop](https://github.com/dotnet/eshop): 可做功能結構與前後端邊界的靈感來源
- [GoogleCloudPlatform/microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo): 可借 flow / trace / 可觀測性呈現方式
- [dockersamples/example-voting-app](https://github.com/dockersamples/example-voting-app): 可借「輕量多語言」組合思路，但不作為預設入口

## 1.0 前你會看到的判斷依據（很重要）

- 如果使用者在第一次打開後，能在不看原始碼前，直接完成一次成功與一次失敗回放；
- 如果失敗時能照著頁面命令做 2 步內修正；
- 如果不必猜「我要先查什麼」，就表示 demo 已經在真正降低 mental model。
