# mini-shop 0.5.77

## 1.0 前 UX 交付（PR 可複製檢核模板）

## 重點改動

- `release-check.sh` 在輸出中加入 PR 交付判斷（`PR 準備度` 與 `PR 備註`）：
  - 直接整合 `success / decline / first_run_within_60s / onboarding_score`。
  - 當 `onboarding_score >= 70` 且三個核心條件達標時，輸出可直接提 PR。
- 在 `release-check.sh` 的可貼摘要加入「PR 交付門檻（可直接貼 PR / release note）」段落。
- 在 `README` 補上標準貼文模板，讓每次 release 前可直接複製到 PR 描述。

## 對使用者心智模型的直接作用

- 降低「這版到底好不好」的人為判斷成本：每輪 release 有明確可貼、可比較的文字輸出。
- 將 `success + decline + 時間目標 + score` 綁成同一判斷條件，避免只看單點綠燈就提前放行。
- 有助於 1.0 前每個版本都在「新手第一輪能否順利 demo」這件事上持續進步。

## 本輪驗收（建議）

- `bash docs/examples/mini-shop/scripts/release-check.sh quick|full|all` 會輸出 `PR 準備度`。
- 建議 Quick 版本至少 `onboarding_score >= 70` 且 `compact suite / success / decline / first_run_within_60s` 全為 true 後再提 PR。
