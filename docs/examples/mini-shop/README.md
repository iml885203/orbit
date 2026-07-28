# mini-shop demo

這個範例展示 Orbit 的核心心智模型：

- host process + container 混合運行
- 多個服務間有 `depends_on`
- 服務健康檢查能直接反映依賴狀態（catalog / inventory / redis）
- 同一個環境同時能做 `status`、`logs`、`open`

包含資源：

- 容器：
  - `redis`（快取容器，做為相依服務）
- Host services：
  - `catalog-api`（產品 catalog，使用 SQLite 作為本地 DB）
  - `inventory-api`（庫存服務，使用 SQLite 記錄可售數量）
  - `order-api`（下單服務，依賴 catalog API / inventory API / redis）
  - `web`（前端）

## 快速啟動

目標：一條命令內看到「可運作的全域觀感」。

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
```

> 預設會啟動 5 個 resource：`catalog-api`、`inventory-api`、`order-api`、`web`、`redis`。

```bash
orbit -c docs/examples/mini-shop/dev.yaml status
orbit -c docs/examples/mini-shop/dev.yaml open
```

用 `--json` 可以做下一步驗證：

```bash
orbit -c docs/examples/mini-shop/dev.yaml status --json
orbit -c docs/examples/mini-shop/dev.yaml logs web -f
orbit -c docs/examples/mini-shop/dev.yaml logs catalog-api -f
orbit -c docs/examples/mini-shop/dev.yaml logs order-api -f
orbit -c docs/examples/mini-shop/dev.yaml logs inventory-api -f
```

預期的健康檢查輸出（`--json` 節錄）：

```json
{
  "service": "order-api",
  "status": "ok",
  "dependencies": {
    "catalog_api": { "ready": true, "url": "http://127.0.0.1:3001" },
    "inventory_api": { "ready": true, "url": "http://127.0.0.1:3003" },
    "redis": { "ready": true }
  }
}
```

## 你能看到什麼

- 先把 `redis` 拉起來。
- `catalog-api` 啟動後會回報 SQLite 可用性（資料會寫到專案內 `mini_shop_*.db`）。
- `order-api` 會等待 `catalog-api`、`inventory-api` 與 `redis` 都 ready 後才會進入 ready。
- `web` 在 `order-api` ready 後啟動；打開頁面後可以直接下單，並看到商品、
  庫存、訂單資料。

與 1.0 打磨對齊的友善體驗：

- 頁面有即時服務狀態卡片（catalog / inventory / order）
- 常見錯誤用可行動訊息呈現（包含下一步建議）
- 服務未就緒時，下單按鈕會停用，避免亂按造成誤會
- 成功/失敗皆會回到明確可繼續操作的流程

訂單流程是：

- `web` -> `order-api`
- `order-api` -> `inventory-api`（先保留庫存）
- `order-api` -> `catalog-api`（再補齊商品名稱與價格）
- `order-api` -> `redis`（快取/背景處理可用性）

## 設計重點（為何這個 demo 對新手友善）

- 後端服務啟動順序固定可預期：你可以先看 `status --json` 就知道哪個
  dependency 卡住，不需要閱讀額外日誌。
- 所有服務使用 `python3 main.py`，不需要額外套件，第一次能直接跑起來。
- `web` 會展示可點式流程（清單、下單、訂單列表），不是只有靜態教學頁。

### 一次成功率高的驗收流程（新手建議）

1. 啟動與等就緒
   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml up
   orbit -c docs/examples/mini-shop/dev.yaml status --json
   ```
   先確認 `order-api`、`catalog-api`、`inventory-api` 都是 `status: ok`。

2. 開啟前端
   ```bash
   orbit -c docs/examples/mini-shop/dev.yaml open
   ```
   頁面會顯示「服務狀態」與「庫存面板」。

3. 先放一筆成功案例
   在前端輸入 `product-id: 1`、`quantity: 1` 點「建立訂單」。
   成功後你應看到：
   - 訂單清單新增 `#...`
   - 庫存面板中產品 1 的剩餘數字減少

4. 失敗案例（驗證你知道錯了什麼）
   下超過庫存數量，會得到 `insufficient_stock`。

5. 一分鐘健康檢查
   - `orbit -c docs/examples/mini-shop/dev.yaml logs order-api -f`
   - `orbit -c docs/examples/mini-shop/dev.yaml logs inventory-api -f`
   - `orbit -c docs/examples/mini-shop/dev.yaml logs catalog-api -f`

### 常見錯誤對照

| 錯誤代碼 | 可能原因 | 建議操作 |
| --- | --- | --- |
| `insufficient_stock` | 庫存不足 | 降低 `quantity` 再試 |
| `catalog_unreachable` | catalog 服務未就緒 | 先確認 `orbit status --json` 中 catalog ready |
| `inventory_unavailable` | inventory 服務未就緒 | 先確認 `orbit status --json` 中 inventory ready |
| `product_not_found` | 商品不存在 | 查看左側商品清單，使用有效的 product id |
| `order_failed_release_failed` | 下單後回補失敗 | 稍後重試，避免反覆點擊 |

## 依賴關係（簡化）

- `web` -> `order-api`
- `order-api` -> `catalog-api`, `inventory-api`, `redis`
- `inventory-api` -> `redis`
- `catalog-api` -> `sqlite`
- `inventory-api` -> `sqlite`

這就是常見「有資料層 + 快取層 + 多後端 + 前端」的 local 開發雛形。
