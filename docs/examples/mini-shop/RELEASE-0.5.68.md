# mini-shop 0.5.68

## 本次目標

讓第一次新手能更快決定「要走哪條路」，把 `compact` 路徑明確變成可選啟動入口，降低第一次看到指令就卡住的機率。

## UX 打磨

- 更新 `start-mini-shop.sh`：
  - 新增 `compact` 模式參數。
  - `compact` 與 `standard` 都使用既有 `dev.yaml`，但 `compact` 在回報文案上提示「先做 success/decline 再做可 demo 檢核」，降低首次行動決策成本。
- README 同步文案：
  - 起步建議改為先 `compact`。
  - 明確指出「先跑 success/decline」再進階。

## 影響

- 新手不會再在啟動階段迷失：第一步是明確可執行的路徑。
- 這些修改不影響既有進階啟動方式與功能。

## 兼容性

- `standard` / `advanced` 仍保持原有行為。
- 所有現有 smoke、release-check 文件鏈路仍保留。
