# mini-shop 0.5.72

## 1.0 前 UX 打磨：讓 release 結果可直接貼

- 新增 `docs/examples/mini-shop/scripts/release-check.sh` 的可貼回覆摘要輸出（`mini-shop Release 交付摘要`）：
  - `compact suite`
  - `success scenario`
  - `first_run_success_ms`
  - `first_run_within_60s`
  - `ready_for_release`
- 在 `full` / `all` 下的 `release-check` 前置條件仍保留「成功情境 60 秒」門檻，讓檢核結果與「是否可對外 demo」一致。
- 將 1.0 前可量測門檻寫到主 README 導向中：執行一次打磨即能回填 `ux_readiness.first_run_within_target`。

### 對 1.0 目標的作用

- 減少版本差異溝通成本：從「看了很多檔案才知道改善」改為「一眼看見可發佈結論」。
- 導向性更清楚：每次失敗能快速看到是 `compact` 還是 `p1` 阻塞，降低定位時間，保留 `demo 一眼可懂` 的節奏。
