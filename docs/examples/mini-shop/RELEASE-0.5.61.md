# mini-shop 0.5.61

## 主要改善

- 新增 release 打磨打包腳本：
  - `docs/examples/mini-shop/scripts/release-check.sh`
  - 支援 `quick`、`full`、`all` 三種檢核等級
  - 會自動彙總 compact smoke 與 p1 smoke（依模式）並輸出 `/tmp/mini-shop-smoke-reports/release-check-summary.json`
- 讓 1.0 前打磨流程更可追溯：
  - 報告同時保留 `compact` 檢核結果、可選 `p1` 檢核結果、repo git/branch、`ready_for_release`
  - 失敗時輸出對應下一步修復建議，並以非 0 status 阻擋錯誤放行
- 補齊 mini-shop README 操作指引：
  - 明確新增 `release-check.sh quick/full/all` 的使用方式
  - 對齊「每次 release 前要留下怎麼驗證」的實際流程

## 對 1.0 UX 目標的直接作用

- 幫打磨者/ reviewer 提供一致、可比對、可貼報告的判斷口徑，降低「看起來有改但不知道是否有用」的心智負擔。
- 將 demo readiness 的判斷集中為可機器讀、可人工看（release-note 可貼）的一份證據，對齊「先給可行動結果，再談討論」的產品思維。
