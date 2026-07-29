# mini-shop 0.5.60

## 主要改善

- 提升 `smoke-compact.sh` 的 release/打磨可讀性：
  - 明確支援 `all|success|decline` 三種模式（保留既有行為）。
  - 任何失敗場景都會回報 `fail:<原因>`，並在失敗時輸出可直接採取的下一步建議。
  - 結果輸出為機器可讀+人工可讀的 `compact-summary.json`，同時保留 success / decline 各自狀態與細節。
- 補充 README 使用說明：
  - 將 `smoke-compact.sh` 的驗收用途和報告位置（`/tmp/mini-shop-smoke-reports/compact-summary.json`）明確寫入，讓每次 release 都能快速對帳「1.0 前新手路徑」。

## 對 1.0 UX 目標的直接作用

- 加快「我是不是做到 demo 打磨」的判斷速度：打磨者可先看 compact report，不用靠猜測判斷失敗在哪。
- 讓失敗時第一反應更清楚：不只告訴你紅，還告訴你下一步該做什麼，降低新手對系統修復心智負擔。
