# Release 0.5.4

- 新增「流程依賴速覽」卡片（Flow map）
  - 在一頁內顯示 `checkout-api` 與其關聯服務狀態（`cart-api`、`order-api`、`payment-api`、`shipping-api`、`inventory-api`、`catalog-api`、`customer-api`）。
  - 以紅黃綠標記，讓新手先定位故障鏈路，降低不必要的猜測。
- 同步更新 README 驗收指引
  - 補上「流程依賴速覽」的實際使用場景與排障順序。

這個 release 目標：
- 降低「要先猜哪個 service 掛掉」的心智負擔
- 讓第一次上手能更快形成正確 mental model：先看依賴圖，再做下一步
