# mini-shop demo

這個範例展示 Orbit 的核心心智模型，重點是降低新手門檻：

- host process + container 混合運行
- 多個服務間有 `depends_on`
- 服務健康檢查能直接反映依賴狀態（catalog / inventory / redis / cart / checkout 等）
- 透過清楚的「流程狀態」讓新手更容易理解關聯
- 強調一次只做一件事：先確認就緒、再加入、再 checkout

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

- 確認前端服務卡上 8/8（含 `checkout-api`）為 `ok`。
- 先選一個客戶，加入 1 件商品到購物車。
- 點 `Checkout`，一次看到 `cart -> payment -> order -> shipping` 的結果鏈。
- 成功後在「訂單」「出貨」區看到同一筆交易的對應資料。

### 用戶心理模型設計（不需要背 service）

- 按鈕會在服務未就緒時停用，避免「為什麼按了沒反應」。
- 錯誤訊息會回報「下一步建議」，例如付款失敗直接提示改 `mock_card`。
- 「服務關係」區塊讓使用者不需讀原始碼就理解服務怎麼串起來。

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

3. 加入購物車：
   - 選客戶
   - 選 product 1，quantity 1
   - 點「加入購物車」

4. 完成 checkout：
   - 選 payment method `mock_card`
   - 點「Checkout」
   - 看到成功訊息，訂單與出貨皆新增
   - 在「最近關聯交易」看到同一筆交易如何對到出貨追蹤

5. 失敗測試：
   - payment method 切成 `decline` 再 checkout，看到明確「付款失敗」提示
   - 下超出庫存的數量，看到 `insufficient_stock`

6. 卡住時也不用猜：
   - 看頁面「診斷命令（可複製）」的建議指令
   - 直接複製貼上到終端執行 `orbit status --json` 與對應 `orbit logs`

7. 查看「執行報告」：
   - 每次加購物車 / checkout / 情境執行都會留下可讀摘要
   - 你可以直接確認「這筆交易最後是成功還是失敗，以及下一步要做什麼」

8. 直接測試情境（建議第一次使用先做這個）：
   - 點「情境 A：成功下單」：會自動完成成功購物路徑
   - 點「情境 B：付款失敗」：會示範 `decline` 路徑與錯誤回饋
   - 點「情境 C：庫存不足」：會示範 `insufficient_stock` 的錯誤表現

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
