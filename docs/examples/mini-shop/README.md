# mini-shop demo

這個範例展示 Orbit 的核心心智模型，重點是降低新手門檻：

- host process + container 混合運行
- 多個服務間有 `depends_on`
- 服務健康檢查能直接反映依賴狀態（catalog / inventory / redis / cart / checkout 等）
- 透過清楚的「流程狀態」讓新手更容易理解關聯
- 強調一次只做一件事：先確認就緒、再加入、再 checkout
- 新增「流程依賴速覽」讓使用者不用看程式也能理解 checkout 鏈路
- 預設是新手導向（只保留關鍵進度與下一步），點「進階」可展開流程地圖、診斷命令與操作回應。

包含資源：

- 容器：
  - `redis`（快取容器，做為相依服務）
- Host services：
  - `catalog-api`（產品 catalog，使用 SQLite）
  - `inventory-api`（庫存）
  - `customer-api`（客戶資料）
  - `cart-api`（購物車，前端先操作）
  - `order-api`（下單服務，依賴 catalog / inventory / customer / redis）
  - `payment-api`（模擬付款）
  - `shipping-api`（模擬出貨，建立 tracking）
  - `checkout-api`（Checkout orchestration：cart->reserve->pay->order->shipment）
  - `web`（前端）

## 快速啟動

目標：一條命令看到完整可操作流程。

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
```

> 預設會啟動 9 個 resource：`catalog-api`、`inventory-api`、`customer-api`、`cart-api`、`order-api`、`payment-api`、`shipping-api`、`checkout-api`、`web`。

```bash
orbit -c docs/examples/mini-shop/dev.yaml status
orbit -c docs/examples/mini-shop/dev.yaml open
```

用 `--json` 做下一步驗證：

```bash
orbit -c docs/examples/mini-shop/dev.yaml status --json
orbit -c docs/examples/mini-shop/dev.yaml logs web -f
orbit -c docs/examples/mini-shop/dev.yaml logs checkout-api -f
orbit -c docs/examples/mini-shop/dev.yaml logs order-api -f
orbit -c docs/examples/mini-shop/dev.yaml logs inventory-api -f
orbit -c docs/examples/mini-shop/dev.yaml logs cart-api -f
```

預期健康檢查結果（`--json` 節錄）：

```json
{
  "service": "checkout-api",
  "status": "ok",
  "dependencies": {
    "catalog_api": { "ready": true, "url": "http://127.0.0.1:3001" },
    "inventory_api": { "ready": true, "url": "http://127.0.0.1:3003" },
    "customer_api": { "ready": true, "url": "http://127.0.0.1:3004" },
    "cart_api": { "ready": true, "url": "http://127.0.0.1:3005" },
    "payment_api": { "ready": true, "url": "http://127.0.0.1:3007" },
    "shipping_api": { "ready": true, "url": "http://127.0.0.1:3008" }
  }
}
```

## 你能看到什麼

- 第一次進來先看「快速判斷值（3 秒）」：只要「服務 / 交易 / 關聯」三格都變綠，代表 demo 已能被人看懂、可立即 demo。
- 確認前端服務卡上 8/8（含 `checkout-api`）為 `ok`。
- 先選一個客戶，加入 1 件商品到購物車。
- 點 `Checkout`，一次看到 `cart -> payment -> order -> shipping` 的結果鏈。
- 成功後在「訂單」「出貨」區看到同一筆交易的對應資料。
- 在「關聯流程時間軸」看到同一次 checkout 的關鍵節點，失敗時可直接看到失敗發生在哪一段（例如付款失敗）。
- 在「關聯完整性快檢查」可直接判斷最近 checkout 是否已建立關聯出貨，不用再手動比對。
- 在「流程依賴速覽」看見上游與下游服務即時狀態，先知道該先補哪個 service。

### 用戶心理模型設計（不需要背 service）

- 按鈕會在服務未就緒時停用，避免「為什麼按了沒反應」。
- 錯誤訊息會回報「下一步建議」，例如付款失敗直接提示改 `mock_card`。
- 「服務關係」區塊讓使用者不需讀原始碼就理解服務怎麼串起來。
- 新增「故障情境對照卡」後，看到錯誤碼就能直接對到對應修復動作，不必先猜原因。
- 加入「關聯完整性快檢查」：最近一筆 checkout 是否成功關聯到出貨，讓用戶不用理解內部資料結構也能驗證結果。

與 1.0 打磨對齊的友善體驗：

- 前端明確的服務就緒訊息，避免盲猜某個服務壞掉。
- 一個流程對應一個按鈕：從購物車到結帳，降低心智切換。
- 每個邊界可回報可行動錯誤（缺貨、付款失敗、庫存不足）。
- 出錯時不只說失敗，還給「下一步」建議。

### 典型驗收流程

1. 啟動：

   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml up
   orbit -c docs/examples/mini-shop/dev.yaml status --json
   ```

2. 打開頁面，確認：
   - 已顯示 9 個 resource
   - 服務狀態卡中有 8/8 ready，或至少 `checkout-api` 顯示 `status: ok`
   - 先看「下一步（照著做）」與「目前進度（你在第幾步）」卡片，或直接點「重置流程」回到起始狀點
   - 「一眼看懂進度」會即時顯示服務 / 客戶 / 購物車 / 成功訂單 4 個狀態

3. 加入購物車：
   - 選客戶
   - 選 product 1，quantity 1
   - 點「加入購物車」

4. 完成 checkout：
   - 選 payment method `mock_card`
   - 點「Checkout」
   - 看到成功訊息，訂單與出貨皆新增
   - 「快速判斷值」會一起變成綠色（服務、交易、關聯）
   - 在「最近關聯交易」與「關聯流程時間軸」確認同一筆交易是否順利完成

   - 同時可在「當前可驗證結果」確認：最新訂單、對應出貨、最後 checkout 結果是否已出現

5. 失敗測試：
   - payment method 切成 `decline` 再 checkout，看到明確「付款失敗」提示
   - 下超出庫存的數量，看到 `insufficient_stock`

6. 卡住時也不用猜：
   - 看頁面「診斷命令（可複製）」的建議指令
   - 時間軸每個節點也提供該 service 的 log 指令，點一下即可 copy，直接貼上 terminal 對應 service 做定點排錯
   - 「故障情境對照卡」同步高亮目前常見錯誤碼，並可直接 copy 對應 service log 指令
   - 直接複製貼上到終端執行 `orbit status --json` 與對應 `orbit logs`

7. 先看「流程依賴速覽」：
   - 如果 `checkout-api` 的箭頭節點顯示 ⚠️，表示上游依賴有異常，先從上游 service 開始處理
   - 從這裡能快速縮小排查範圍，不用每次都先猜錯服務順序

8. 查看「執行報告」：
   - 每次加購物車 / checkout / 情境執行都會留下可讀摘要
   - 你可以直接確認「這筆交易最後是成功還是失敗，以及下一步要做什麼」

9. 直接測試情境（建議第一次使用先做這個）：
   - 點「情境 A：成功下單」：會自動完成成功購物路徑
   - 點「情境 B：付款失敗」：會示範 `decline` 路徑與錯誤回饋
   - 點「情境 C：庫存不足」：會示範 `insufficient_stock` 的錯誤表現

   - 這些情境都有「預估時間」提示，若超過時間仍未返回結果，先看「診斷命令」。

### 失敗後最短回復（建議默認作法）

遇到卡住時，不要先切服務或重開整個環境，先用這條路徑：

1. `orbit -c docs/examples/mini-shop/dev.yaml status --json`  
   看紅色節點和 `recommended` 修復方向（通常先修上游）。
2. 對著對應節點看 log：`orbit -c docs/examples/mini-shop/dev.yaml logs <service> -f`  
3. 回到頁面跑一次「一鍵 demo 一輪」或「失敗情境」，看故障卡是否變成可修復狀態。

更完整的 demo 方向決策請看：[DEMO-STRATEGY.md](./DEMO-STRATEGY.md)。

### 最短重現路徑

- 「最短重現路徑」提供三個一鍵入口：
  1. 最短成功路徑
  2. 快速付款失敗
  3. 快速庫存不足
- 每個入口都會直接執行最少步驟並帶到可驗證結果（訂單 / 出貨 / 時間軸 / 報告），適合剛接觸者先驗證 demo 是否「可用」。

### 故障演練（服務 down → 恢復）

你可以用 4 步練一次「故障→定位→恢復」：

1. 先點「故障演練」中的「先做一輪成功流程」建立 baseline。
2. 在 terminal 貼上：

   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml down cart-api --json
   ```

   模擬 `cart-api` 異常。
3. 再貼上：

   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml status --json
   ```

   看 `cart-api` 是否變紅、頁面的錯誤建議是否清楚。
4. 貼上：

   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml restart cart-api --json
   ```

5. 回頁面點「故障演練（1/2 分鐘）」的「回到 3 秒可 demo 指標」，確認「服務 / 交易 / 關聯」再回到綠。

這樣你可以確認這個 demo 不只「能用」，而且「壞掉之後也可自己恢復」。

### 首次最小心智體驗（建議新手先做）

1. 開啟後先不用看所有卡片。
2. 直接按「第一次 demo（30 秒）」裡的「開始 demo 一輪」，最快可直接看到完整成功鏈路（訂單 + 出貨 + 時間軸）。
3. 接著按「核對可 demo 條件」，確認「快速判斷值（服務 / 交易 / 關聯）」都為綠色。
4. 需要更細可回到「一頁式目標導覽」按「下一步操作建議」逐步操作。
5. 核對「訂單 / 出貨 / 時間軸」出現同一筆關聯結果。
6. 若只想快速重播一次成功 path，按「一鍵完成成功流程」或「重做一次成功流程」。

### 1.0 前可交付快速檢核（建議每次 release 前）

- [ ] 服務全數就緒（`checkout-api` 及 `web` 相關依賴 8/8 ready）
- [ ] 至少完成一筆 `mock_card` 成功 checkout
- [ ] 訂單有對應出貨，且前端顯示「關聯完整性快檢查」為綠色
- [ ] 「故障情境對照卡」能在情境 B / C 下提供可執行建議（可複製命令或一鍵動作）
- [ ] 「1.0 交付前檢核」三項可直接點擊並導向下一步，使用者不必靠猜測判斷下一步。

> 需要可重複的 QA 節奏，可直接看 [UX 驗收檢查單](./UX-ACCEPTANCE-CHECKLIST.md)。

## 釋出前 UX smoke check

- 建議每次打磨完成後跑一次： [UX smoke check](./UX-SMOKE-CHECK.md)  
  先驗證成功路徑、再驗證兩個失敗回歸（付款失敗 / 庫存不足），確保文案與操作仍是「可猜測率低」的流程。

你可把「操作摘要」當作目前這一步的進度日誌，搭配「診斷命令」快速定位。
想重複 demo 時，直接按「重置流程」即可回到起始節點（清空購物車、回到預設付款方式）。

### 常見錯誤對照

| 錯誤代碼 | 可能原因 | 建議操作 |
| --- | --- | --- |
| `insufficient_stock` | 庫存不足 | 降低 quantity 或等待清庫存測試資料 |
| `cart_unreachable` | cart 服務未就緒 | 先確認 `orbit status --json` 中 `cart-api` ready |
| `catalog_unreachable` | catalog 服務未就緒 | 先確認 `catalog-api` ready |
| `inventory_unreachable` | inventory 服務未就緒 | 先確認 `inventory-api` ready |
| `payment_declined` | 模擬付款失敗 | 改用 `mock_card`，或關閉 `decline` |
| `checkout failed` | 任一服務回傳異常 | 查看 `logs checkout-api -f`，優先修復 upstream dependency |

### 依賴關係（簡化）

- `web` -> `catalog-api`, `cart-api`, `checkout-api`
- `checkout-api` -> `cart-api`, `customer-api`, `order-api`, `inventory-api`, `payment-api`, `shipping-api`
- `order-api` -> `catalog-api`, `inventory-api`, `customer-api`, `redis`
- `cart-api` -> `catalog-api`, `sqlite`
- `shipping-api` -> `sqlite`

這是「有關聯、有流程、可反覆操作」的 mini demo，適合當 Orbit 進入 release 的第一個可感知價值案例。
