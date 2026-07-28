# Release 0.5.12（故障情境一鍵修復）

本版重點在把「錯誤提示」變成「可立即行動」的操作：

- 故障情境卡新增「一鍵修復」類按鈕，不只顯示建議。
- 為支付失敗提供直接切 `mock_card` + 重試 Checkout。
- 為 `cart_empty`、`insufficient_stock` 提供一鍵調整數量建議。
- 為通用失敗導向加入可快速回到下一步、情境重跑、或一鍵完成成功流程。
- 讓故障卡同時可直接複製「診斷命令 / 服務 log 命令」，減少手工輸入成本。

目前變更可見於：
- `docs/examples/mini-shop/apps/web/index.html`：新增 `recoveryActionsForErrorCard` 與對應按鈕事件，
  讓失敗卡可直接幫使用者把流程導回可成功路徑。
- `docs/examples/mini-shop/RELEASE-0.5.12.md`：記錄本次 UX 進展。

- 再次優化故障卡互動體驗：為每種可恢復行為增加「primary」主動作視覺提示（黃底按鈕），降低新手在多個按鈕中猶豫成本。
