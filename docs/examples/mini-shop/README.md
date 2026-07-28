# mini-shop demo

這個範例展示 Orbit 的核心心智模型：

- host process + container 混合運行
- 多個服務間有 `depends_on`
- 服務健康檢查能直接反映依賴狀態（catalog health / redis health）
- 同一個環境同時能做 `status`、`logs`、`open`

包含資源：

- 容器：
  - `redis`（快取容器，做為相依服務）
- Host services：
  - `catalog-api`（產品 catalog，使用 SQLite 作為本地 DB）
  - `order-api`（下單服務，依賴 catalog API 與 redis）
  - `web`（前端）

## 快速啟動

目標：一條命令內看到「可運作的全域觀感」。

```bash
orbit -c docs/examples/mini-shop/dev.yaml up
```

> 預設會啟動 4 個 resource：`catalog-api`、`order-api`、`web`、`redis`。

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
```

預期的健康檢查輸出（`--json` 節錄）：

```json
{
  "service": "order-api",
  "status": "ok",
  "dependencies": {
    "catalog_api": { "ready": true, "url": "http://127.0.0.1:3001" },
    "redis": { "ready": true }
  }
}
```

## 你能看到什麼

- 先把 `redis` 拉起來。
- `catalog-api` 啟動後會回報 SQLite 可用性（資料會寫到專案內 `mini_shop_*.db`）。
- `order-api` 等 `catalog-api` 與 `redis` 都 ready 後才啟動。
- `web` 在兩個後端 ready 後啟動；打開頁面後可以直接下單，
  前端會顯示關聯服務的健康與資料。

## 設計重點（為何這個 demo 對新手友善）

- 後端服務啟動順序固定可預期：你可以先看 `status --json` 就知道哪個
  dependency 卡住，不需要閱讀額外日誌。
- 所有服務使用 `python3 main.py`，不需要額外套件，第一次能直接跑起來。
- `web` 會展示可點式流程（清單、下單、訂單列表），不是只有靜態教學頁。

## 依賴關係（簡化）

- `web` -> `order-api`
- `order-api` -> `catalog-api`, `redis`
- `catalog-api` -> `sqlite`

這就是常見「有資料層 + 快取層 + 多後端 + 前端」的 local 開發雛形。
