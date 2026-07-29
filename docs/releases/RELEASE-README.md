# Release 文件目錄

這裡集中放 pre-1.0 的 release notes 與 release 驗收模板。

- `RELEASE-CHECKLIST-TEMPLATE.md`
  - 每次發版前必填的「UX 交付」模板。
- `RELEASE-0.0.18.md`
  - 目前最新已發佈版本的 UX 打磨與驗收記錄。
- `RELEASE-0.0.19.md`
  - 預熱版可見性、mini-shop 恢復路徑與 release 模板落地的文件化記錄。
- `RELEASE-0.0.20.md`
  - 新增服務 down/restart 故障演練卡，將「故障修復」打造成可驗證 UX 路徑。
- `RELEASE-0.0.21.md`
  - 為故障演練加上時間量化回饋，讓 release 之間能對比 UX 變快變順。
- `RELEASE-0.0.22.md`
  - 新增「本次 demo 指標」panel，集中展示 1.0 核心交付條件（服務就緒、成功 checkout、關聯驗證）的即時進度。
- `RELEASE-0.0.23.md`
  - 修正本次 demo 指標的 session 重置行為，避免重置後仍殘留舊 session 的完成時間。
- `RELEASE-0.5.72.md`
  - first-run 60 秒門檻可量化，並加入可貼式 release 交付摘要方向。
- `RELEASE-0.5.73.md`
  - release-check 可直接輸出可貼到版本描述的 body（含 ready_for_release 與 60 秒門檻）。
- `RELEASE-0.5.74.md`
  - 新手首屏只保留最小 demo 路徑，降低心智模型負擔。
- `RELEASE-0.5.75.md`
  - 將 1.0 前打磨聚焦為「可交付價值可見」：新增明確的 demo 價值敘述、第一輪成功-失敗可復現、與 release-ready 交付摘要一致化。
- `RELEASE-0.5.76.md`
  - 把 `release-check` 變成新手交付儀表板：加入 decline、first-run 時間、onboarding_score 讓每輪 UX 進步可量化。
- `RELEASE-0.5.77.md`
  - 增加 PR 交付門檻輸出（success/decline/time/score 一起判斷），並在 README 新增可直接貼 PR 的標準段落。
- `RELEASE-0.5.78.md`
  - 新增 demo 族譜與參考專案採用策略，明確「不為了複雜化而複雜化」的 1.0 前 UX 方針，將重點轉為交易敘事與可回歸心智模型。

建議流程：

1. 發布前，先以 `RELEASE-CHECKLIST-TEMPLATE.md` 填寫一份「本版交付摘要」。
2. 形成正式 `RELEASE-x.y.z.md` 時，保留同樣欄位（可附上實測時間/結論）。
3. 發布成功後，把本版的 release note body 內嵌在 GitHub release。
