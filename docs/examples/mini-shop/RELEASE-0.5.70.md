# mini-shop 0.5.70

## 本次目標

讓 1.0 前「第一輪可重放」更穩定：只要腳本看起來完成，就是「知道環境是否可 demo」+「知道下一步要做什麼」。

## UX 打磨

- 強化 `docs/examples/mini-shop/scripts/compact-onboarding.sh`
  - 啟動後不只等待服務回應，而是會確認關鍵服務的 `/health` 回傳 `status: ok`。
  - smoke 失敗時會保留 `compact-summary.json`，並直接印出每一個場景的下一步修正建議。
  - 在關鍵環節增加明確 fallback 命令：
    - `orbit -c docs/examples/mini-shop/dev.yaml status --json`
    - `tail -f /tmp/mini-shop-compact-orbit.log`
    - `orbit -c docs/examples/mini-shop/dev.yaml up`
- 同步 README 文檔
  - 修正腳本完成輸出檔名為 `compact-summary.json`。
  - 明確註明腳本包含 `status: ok` 檢核 + `success/decline` 兩段結果。
- 保持現有 web 一鍵流程與故障建議邏輯不變，先把入口體驗做穩（結果可讀、下一步可行）

## 使用者價值

- 使用者第一次做完「run」後，不再只看到一堆命令行文字，而是能直接看到：
  - 哪裡綠、哪裡沒綠
  - 成功/失敗場景是否通過
  - 下一步怎麼修（可複製建議）
- 更符合 1.0 前目標：降低心智模型（少一個「我該先做什麼」的猜測成本）。
