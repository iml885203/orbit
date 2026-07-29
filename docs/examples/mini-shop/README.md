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
- `observability-api`（進階模式）：彙整訂單 / 出貨關聯，提供最近事件快照
- `notification-api`（進階模式）：訂單完成時記錄通知紀錄，展示「關聯」最後一圈
- `web`（前端）

## 快速啟動

目標：一條命令看到完整可操作流程。

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
```

> 預設會啟動 9 個 resource：`catalog-api`、`inventory-api`、`customer-api`、`cart-api`、`order-api`、`payment-api`、`shipping-api`、`checkout-api`、`web`。


進階版本（加入觀測服務）可改用：

```bash
orbit -c docs/examples/mini-shop/dev-advanced.yaml up --group mini-shop-advanced
```

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
- 第一次進來更快：頁面右上有「一次成功」超大按鈕，直接幫你完成一輪成功鏈路；若有最近錯誤，會顯示「一鍵修復最近錯誤」超大按鈕。
- 再看「一輪 demo 結論」卡片：直接看「可 demo / 不可 demo」的最終判斷，以及缺口是在哪一格。
- 結論卡還提供一鍵導向按鈕：缺哪一格會直接跳你需要補齊的區塊。
- 確認前端服務卡上基線服務已就緒（含 `checkout-api`）為 `ok`。
- 先選一個客戶，加入 1 件商品到購物車。
- 點 `Checkout`，一次看到 `cart -> payment -> order -> shipping` 的結果鏈。
- 成功後在「訂單」「出貨」區看到同一筆交易的對應資料。
- 在「關聯流程時間軸」看到同一次 checkout 的關鍵節點，失敗時可直接看到失敗發生在哪一段（例如付款失敗）。
- 在「關聯完整性快檢查」可直接判斷最近 checkout 是否已建立關聯出貨，不用再手動比對。
- 在進階模式可看「關聯觀測洞察」卡片：一次看到近 10 筆「訂單→出貨」事件是否關聯成功，也會看到「通知事件」是否同步完成。
- 在「流程依賴速覽」看見上游與下游服務即時狀態，先知道該先補哪個 service。
- 進階模式若想驗證「關聯閉環」完整，直接看 `notification-api` 有沒有收到對應事件（`/notifications`）。

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

### 典型驗收流程（1.0 前可重複檢查）

> 目標：第一次開啟只需 3 個目標即可判斷是否可 demo，不用先懂 service 細節。

1. 啟動：

   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml up
   orbit -c docs/examples/mini-shop/dev.yaml status --json
   ```

2. 打開頁面，確認：
   - 已顯示 9 個 resource
   - 服務狀態卡中基線服務都 ready（進階模式會包含觀察服務），或至少 `checkout-api` 顯示 `status: ok`
   - 先看「目前進度（你在第幾步）」卡片，或直接點「重置流程」回到起始狀點（「下一步（照著做）」可在進階模式開啟）
   - 先看「一輪 demo 結論（先看這裡）」卡片，若顯示可 demo，代表可以進到下一步
   - 「一眼看懂進度」會即時顯示服務 / 客戶 / 購物車 / 成功訂單 4 個狀態

   - 預期這一步看到：`快速判斷值` 服務欄位由紅/灰轉為綠

2.1. 直接 60 秒最短成功流程（建議新手）：
   - 點「一次成功」超大按鈕（一次完成清空→加入→checkout）。
   - 看「一輪 demo 結論」是否變成「可 demo」。
   - 看到紅綠燈（服務 / 交易 / 關聯）都為綠，即可進入下一輪驗證。

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
   - 「一輪 demo 結論」會顯示「可直接 demo」，可直接複製「demo 報告」貼給 reviewer

   - 如果未達標，點結論卡上的按鈕可直接跳到對應流程區塊，不用先猜
   - 同時可在「當前可驗證結果」確認：最新訂單、對應出貨、最後 checkout 結果是否已出現

5. 失敗測試：
   - payment method 切成 `decline` 再 checkout，看到明確「付款失敗」提示
   - 下超出庫存的數量，看到 `insufficient_stock`

6. 卡住時也不用猜：
   - 看頁面「診斷命令（可複製）」的建議指令
   - 時間軸每個節點也提供該 service 的 log 指令，點一下即可 copy，直接貼上 terminal 對應 service 做定點排錯
   - 「故障情境對照卡」同步高亮目前常見錯誤碼，並可直接 copy 對應 service log 指令
   - 直接複製貼上到終端執行 `orbit status --json` 與對應 `orbit logs`

   - 仍無法判斷時，按「故障演練」裡「複製故障演練腳本」，一次貼完可完成一次 down → status → restart → status

7. 先看「流程依賴速覽」：
   - 如果 `checkout-api` 的箭頭節點顯示 ⚠️，表示上游依賴有異常，先從上游 service 開始處理
   - 從這裡能快速縮小排查範圍，不用每次都先猜錯服務順序

8. 查看「執行報告」：
   - 每次加購物車 / checkout / 情境執行都會留下可讀摘要
   - 你可以直接確認「這筆交易最後是成功還是失敗，以及下一步要做什麼」

9. 快速驗收結論（按這 3 行，若都通過代表可對外 demo）：

   - [x] 服務就緒與交易流程可到「快速判斷值」三格全綠
   - [x] 最近一筆 `mock_card` checkout 有對應 `訂單 + 出貨` 並可關聯
   - [x] 故障回歸腳本能在 120 秒內把三格拉回綠

10. 直接測試情境（建議第一次使用先做這個）：
   - 點「情境 A：成功下單」：會自動完成成功購物路徑
   - 點「情境 B：付款失敗」：會示範 `decline` 路徑與錯誤回饋
   - 點「情境 C：庫存不足」：會示範 `insufficient_stock` 的錯誤表現

   - 這些情境都有「預估時間」提示，若超過時間仍未返回結果，先看「診斷命令」。

### 命令列示範腳本（可直接複製）

如果你要完全用 terminal 做打磨/驗收，下面三支腳本是「最小心智模型」版本：每次只要對應一個明確結果，不用猜下一步。

你也可以直接執行專案內建的一鍵腳本（預設跑 3 種情境）：

```bash
bash docs/examples/mini-shop/scripts/smoke-demo.sh
```

發佈前固定用 1.0 前 smoke，可同時驗收 readiness 與三種情境，並輸出可讀報告：

```bash
bash docs/examples/mini-shop/scripts/smoke-p1.sh all
```

只驗證成功流程可改 `success`：

```bash
bash docs/examples/mini-shop/scripts/smoke-demo.sh success
```

- 先執行一次成功（A）當基線，再做 B/C。

1) 成功（A）

```bash
#!/usr/bin/env bash
set -e

BASE="127.0.0.1"
CUSTOMER_ID=1
PRODUCT_ID=1

curl -s "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST
curl -s "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
  -H 'Content-Type: application/json' \
  -d '{"product_id":'$PRODUCT_ID',"quantity":1}'

curl -s "http://$BASE:3006/checkout/$CUSTOMER_ID" \
  -H 'Content-Type: application/json' \
  -d '{"method":"mock_card"}'

echo "訂單/出貨結果："
curl -s "http://$BASE:3002/orders"
curl -s "http://$BASE:3008/shipments"
```

2) 付款失敗（B）

```bash
#!/usr/bin/env bash
set -e

BASE="127.0.0.1"
CUSTOMER_ID=1
PRODUCT_ID=1

curl -s "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST
curl -s "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
  -H 'Content-Type: application/json' \
  -d '{"product_id":'$PRODUCT_ID',"quantity":1}'

curl -s "http://$BASE:3006/checkout/$CUSTOMER_ID" \
  -H 'Content-Type: application/json' \
  -d '{"method":"decline"}'
```

3) 庫存不足（C）

```bash
#!/usr/bin/env bash
set -e

BASE="127.0.0.1"
CUSTOMER_ID=1
PRODUCT_ID=1

curl -s "http://$BASE:3005/carts/$CUSTOMER_ID/clear" -X POST
curl -s "http://$BASE:3005/carts/$CUSTOMER_ID/items" \
  -H 'Content-Type: application/json' \
  -d '{"product_id":'$PRODUCT_ID',"quantity":999}'

curl -s "http://$BASE:3006/checkout/$CUSTOMER_ID" \
  -H 'Content-Type: application/json' \
  -d '{"method":"mock_card"}'
```

你可直接複製貼上執行；若有服務沒有預設 port，先用 `orbit status --json` 對應到 `catalog-api/inventory-api/...` 的對應網址。

### 失敗後最短回復（建議默認作法）

遇到卡住時，不要先切服務或重開整個環境，先用這條路徑：

1. `orbit -c docs/examples/mini-shop/dev.yaml status --json`  
   看紅色節點和 `recommended` 修復方向（通常先修上游）。
2. 對著對應節點看 log：`orbit -c docs/examples/mini-shop/dev.yaml logs <service> -f`  
3. 回到頁面跑一次「一鍵 demo 一輪」或「失敗情境」，看故障卡是否變成可修復狀態。

更完整的 demo 方向決策請看：[DEMO-STRATEGY.md](./DEMO-STRATEGY.md)。
如要對比外部案例（例如 `dotnet/eshop`、`microservices-demo`、`example-voting-app`）並快速決定
哪些值得 1.0 前採納，請參考：[DEMO-REFERENCE-MATRIX.md](./DEMO-REFERENCE-MATRIX.md)。
如果你想先對齊「為什麼要保留這種複雜度」的設計原則，請閱讀：[DEMO-VALUE-PROPOSITION.md](./DEMO-VALUE-PROPOSITION.md)。

### 最短重現路徑

- 「最短重現路徑」提供三個一鍵入口：
  1. 最短成功路徑
  2. 快速付款失敗
  3. 快速庫存不足
- 每個入口都會直接執行最少步驟並帶到可驗證結果（訂單 / 出貨 / 時間軸 / 報告），適合剛接觸者先驗證 demo 是否「可用」。
- 另外「營運快覽」會直接用三眼看板給出：  
  「本輪是否有成功交易」、「最近關聯是否完成」、「可否直接 demo」。

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

量化建議（每次 release 觀察）：

- 基線耗時（先做一輪成功流程）：`___` 秒（建議 < 15 秒）
- 回歸耗時（從基線完成到 `回到 3 秒可 demo 指標`）：`___` 秒（建議 < 120 秒）

這樣你可以確認這個 demo 不只「能用」，而且「壞掉之後也可自己恢復」。

### 首次最小心智體驗（建議新手先做）

1. 開啟後先不用看所有卡片。
2. 直接按「第一次 demo（30 秒）」裡的「開始 demo 一輪」，最快可直接看到完整成功鏈路（訂單 + 出貨 + 時間軸）。
3. 接著按「核對可 demo 條件」，確認「快速判斷值（服務 / 交易 / 關聯）」都為綠色。
4. 需要更細可回到「一頁式目標導覽」按「下一步操作建議」逐步操作。
5. 核對「訂單 / 出貨 / 時間軸」出現同一筆關聯結果。
6. 若只想快速重播一次成功 path，按「一鍵完成成功流程」或「重做一次成功流程」。
7. 需要回報時可直接點「複製 demo 報告」，可拿到簡明文字摘要。
8. 若要貼到 PR 交付說明，還可點「複製 demo 指標 JSON」，直接貼上就能做逐版本比對。
9. 做完故障演練後，可再按「複製故障回歸報告」，直接把基線耗時、回歸耗時與三格可 demo 狀態一起貼給團隊。
10. 遇到失敗時，先按「現在只要做一件事」裡的「一鍵修復最近錯誤」，系統會依最近錯誤碼套用建議動作。



### 進階版（觀察交易關聯）

如果你想驗證「進階觀測值」是否正常，可以在 mini-shop-advanced 下加一段：

```bash
orbit -c docs/examples/mini-shop/dev-advanced.yaml up --group mini-shop-advanced
orbit -c docs/examples/mini-shop/dev-advanced.yaml logs observability-api -f
```

再呼叫：

```bash
curl http://127.0.0.1:3010/health
curl http://127.0.0.1:3010/insights
curl http://127.0.0.1:3010/events
```

當完成一筆成功 checkout 後，`/insights` 的 `correlation_ratio` 會接近 `1.0`，可直接讓觀察者看到「訂單與出貨關聯」的可觀測結果。
### 1.0 前可交付快速檢核（建議每次 release 前）

- [ ] 服務全數就緒（`checkout-api` 及 `web` 相關依賴基線服務 ready；進階模式再含觀察服務）

### 近期釋出摘要（內部 review 用）

- 本次可交付差異請直接看：[`RELEASE-0.5.58.md`](./RELEASE-0.5.58.md)
- [ ] 至少完成一筆 `mock_card` 成功 checkout
- [ ] 訂單有對應出貨，且前端顯示「關聯完整性快檢查」為綠色
- [ ] 「故障情境對照卡」能在情境 B / C 下提供可執行建議（可複製命令或一鍵動作）
- [ ] 「1.0 交付前檢核」三項可直接點擊並導向下一步，使用者不必靠猜測判斷下一步。
- [ ] 「資料關係快覽」可一眼看出本輪資料關聯是否合理（客戶/購物車/訂單/出貨）。

### 本次 demo 指標卡（建議 release 前確認）

- 確認 `本次 demo 指標` 有顯示三個維度且有顯示出可執行結論：
  - 服務就緒（可就緒時間）
  - checkout 成功（可執行成果）
  - 關聯驗證（訂單與出貨是否對齊）
- 目標可交付欄位應在完成關聯後轉為「可 demo」。

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
