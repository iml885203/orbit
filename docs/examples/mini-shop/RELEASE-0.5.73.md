# mini-shop 0.5.73

## 1.0 前 UX 打磨：讓 release 可見化可貼用

- 新增 `docs/examples/mini-shop/scripts/release-check.sh` 的可貼摘要輸出：
  - 會寫入 `/tmp/mini-shop-smoke-reports/release-check-body.md`
  - 同時輸出可直接貼到 release note 的 markdown（`mini-shop Release <mode>`）
- 交付欄位維持 `compact suite / success scenario / first_run_success_ms / first_run_within_60s / ready_for_release`，
  並補強 `ready_for_release` 對 quick/full/all 的一致解讀。

### 對使用者體驗的直接影響

- 你不再需要等 reviewer 自行整理結果；每次 release 前都能直接抓 `release-check-body.md` 做審視。
- 每次打磨都可對外產生「可讀、可比對」的交付證據，讓 1.0 前的 UX 改進更容易被接受與回顧。
