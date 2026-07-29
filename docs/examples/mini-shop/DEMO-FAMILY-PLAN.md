# mini-shop demo 家族化方案（1.0 前）

目標：保持 `mini-shop` 當主入口不變，但新增「更快新手入門」版本，讓 1.0 前評估可以同時看見：

- `mini-shop`（完整值，偏功能完整）
- `mini-shop-compact`（入門最快，偏少心智模型）
- `mini-shop-advanced`（偏可觀測）

## 先定義「4-service MVP」可實作方向（未來實際上線前）

`4-service` 的精神不是硬砍現有 API，而是要可交付的「最小用戶路徑」。可做法：

1. 建立 `mini-shop-compact` backend profile：
   - `catalog-api`
   - `cart-api`
   - `checkout-api`
   - `order-api`
   - `payment-api`
   - `web`
2. 將 inventory/customer/shipping 透過 checkout/order 內部 mock adapter 降為「可關閉 dependency」的可選項（`COMPACT_MODE=1`）。
3. UI 在 compact 模式只做三格判斷：
   - `服務就緒`
   - `交易成功`
   - `訂單成立`（關聯簡化為「order 有無結果」）。
4. 故障演練只保留：`cart unreachable / payment declined` 兩步，先把新手心智壓到最低。

## 建議採納順序（建議這樣打磨）

1. **保留當前 1.x 入口**：不改 `mini-shop` 行為，先把文檔、命令和導引做穩（你現在已經做了大半）。
2. **加入 compact 腳本路徑**：只做 success + decline，做為「新手首次 2 分鐘路徑」。
3. **把 compact profile 當成明確的目標（未來版）**：在 `1.0.0` 前先做可用的 4-service 原型，不一定要同時把所有原始 API 重構完成。

## Demo 族譜補充：不用自己重寫整個新專案的做法

你問「是不是太簡單」很實際。建議採用「分層 demo」而不是「大 repo 突然上去」：

1. **主 demo（保留）**  
   - 直接用 `mini-shop`：最短路徑、第一眼可 demo。  
   - 目標是讓新手 1–2 分鐘內完成「一筆成功 + 一筆失敗」並說清楚卡在哪裡。

2. **進階引用（外部參考）**  
   - 把 [dotnet/eshop](https://github.com/dotnet/eshop) 或 [microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo) 的概念，只做可觀測與服務分層的演示素材，不做預設入口。  
   - 例如：把「請求路徑圖」、「trace」做成 demo chapter，讓你能用同一個 Orbit 介紹不同風格，不增加新手首輪負擔。

3. **不做的事（1.0 前明確卡關）**  
   - 不再拿企業級 monolith/microservices 套件直接當主 demo。  
   - 不把資料庫類型（SQL Server / Kafka）當作先決條件。先做跨 service 的可理解，後再談技術棧擴展。

## 你現在可以直接打包的 UX 交付（本輪）

- 對外 release 的主要訊息：
  - 「看得懂」(three-card + next action)
  - 「能修」(故障演練一鍵)
  - 「能驗」(可複製 demo 報告)
- 新增 `compact smoke`：只做最核心流程，先驗證使用者是不是 2 分鐘內可完成一次成功與一次處理失敗。
