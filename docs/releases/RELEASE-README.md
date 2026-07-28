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

建議流程：

1. 發布前，先以 `RELEASE-CHECKLIST-TEMPLATE.md` 填寫一份「本版交付摘要」。
2. 形成正式 `RELEASE-x.y.z.md` 時，保留同樣欄位（可附上實測時間/結論）。
3. 發布成功後，把本版的 release note body 內嵌在 GitHub release。
