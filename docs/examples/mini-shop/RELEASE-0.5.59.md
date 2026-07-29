# mini-shop 0.5.59

## 主要改善

- 新增 1.0 前新手最短驗收路徑文件：`DEMO-FAMILY-PLAN.md`。
  - 明確定義完整版 / 簡化版 / 進階版的 demo 角色，不再把 4-service 與 6-service 擴充停在口頭層面。
- 新增 compact smoke 命令：`scripts/smoke-compact.sh`。
  - 固定檢查「成功」與「decline」兩個最重要路徑，降低「第一次驗收要做多少件事」的負擔。
- 更新 `README`：補上 compact smoke 命令與驗收入口，並連結 demo 家族化規劃。

## 對 1.0 UX 目標的直接作用

- 將打磨焦點明確到「新手最短路徑」：成功 + 失敗處理。
- 為你在 release 前提供可重複的最小報告（compact report），方便判斷是否有持續降低心智負擔。
