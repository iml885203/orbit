# mini-shop 0.5.71

## 本次目標

讓「可 demo 判斷」不會被舊資料誤導，減少新手在失敗後的認知偏差；把每次變更都能在報告與目視卡片上看得見。

## UX 打磨

- 修正 checkout 成功/失敗判讀來源
  - 新增 `checkout attempt` 追蹤狀態，記錄每一次 checkout 的實際 attempt（含結果 code、時間、target order）。
  - 不再直接以 `latestOrdersPayload.orders[0]` 當「是否可 demo」的唯一依據，避免「最近一次 checkout 失敗但舊成功訂單還在」的假綠燈。
- 重定義可判斷範圍：服務 / 交易 / 關聯只看「最後一次 checkout 嘗試結果」
  - `setAcceptanceChecklist`、`setDemoVerdict`、`setSessionMetrics`、`buildSignalSnapshot`、`buildDemoReport`、`buildRecoveryReport`、流程進度與流程地圖都改用最後 checkout attempt 對齊。
  - `flow timeline` 和 `result` 面板會優先聚焦最後一次 attempt 對應訂單，避免新手誤判。
- 增加可追溯輸出
  - Recovery / Demo 報告新增「最後 checkout 嘗試時間」資訊，便於事後回放與交付時說明。

## 使用者價值

- 新手在失敗後不會因為 DB 裡殘留舊成功訂單，還看到 demo 綠燈。
- 「下一步建議」與「可 demo 牌告」更一致：同一個 checkout 對應一組結論，降低猜測成本。
- 針對每輪改進有固定 release 摘要，便於你看到版本間 UX 進步。
