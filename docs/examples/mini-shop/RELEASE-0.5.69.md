# mini-shop 0.5.69

## 本次目標

讓 1.0 前新手能真正用「一個命令」完成第一輪成功/失敗檢核，降低從 terminal 進場的心智負擔。

## UX 打磨

- 新增 `docs/examples/mini-shop/scripts/compact-onboarding.sh`。
  - 支援 `run`：背景啟動 compact 路徑、等待關鍵服務、跑 `smoke-compact`（success + decline）。
  - 支援 `smoke-only`：只做 success/decline 檢核。
  - 提供明確下一步提示與報告路徑，讓第一次使用者知道「下一步要做什麼」。
- README 明確將新手入口改為 `compact-onboarding.sh`。
- mini-shop web 快速啟動面板新增「複製 compact 一鍵導覽命令」，把文檔與操作入口對齊。

## 為何對使用者有價值

- 減少新手在第一次設定時的決策：不需要先猜要先啟動哪一條命令、要先做什麼檢核。
- 第一輪失敗時有可直接行動的 next-step，減少卡住但不知道接著做什麼。
- 這些改動不改動既有 advanced 功能，只提升首次體驗和 release 可交付可讀性。
