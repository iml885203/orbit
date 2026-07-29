# mini-shop v0.5.39（1.0 打磨：smoke 交付可讀性與自助修復回饋）

## 本次打磨目標

讓每次 release 的驗證結果「更像可交付品質報告」：
- 失敗時不只是失敗，還能直接知道卡在哪個 service、卡多久；
- 用一眼就懂的「建議命令」讓使用者不用猜下一步；
- 命令列 demo 場景輸出可核對的關鍵回應，便於 review 比較版本差異。

## 這版變更

- `docs/examples/mini-shop/scripts/smoke-p1.sh`
  - 服務就緒等待超時時，輸出更明確資訊：
    - 哪個服務卡住
    - 已等待多久（秒）
    - 直接可複製貼上的 `status/logs/restart/down` 修復指令
    - 失敗前的 `orbit status --json --group` 快照（寫在 `/tmp/mini-shop-smoke-reports/*-readiness-dump.json`）
  - 所有套件失敗都改為輸出可執行修復步驟，而不是只回 `FAIL`。
  - `all` 模式下，mini 與 advanced 任一階段失敗時也會保留清晰中斷點。

- `docs/examples/mini-shop/scripts/smoke-demo.sh`
  - 各情境加入時間戳與回應摘要行（清空/加入品項/checkout 結果），並強化 `curl` 錯誤輸出可讀性。
  - 成功流程若 checkout 回應為空，會直接回傳失敗，避免假陽性。

## 使用者體驗改善重點

- 使用者/Reviewer 會看到「我現在該做什麼」而不是「只是失敗」。
- 針對第一次 demo 的心智負擔，縮小到兩條操作：
  1) 跑完命令，看是哪個 service 掛住；
  2) 直接執行建議命令修正後再重跑。

## 驗收建議

```bash
bash docs/examples/mini-shop/scripts/smoke-p1.sh mini
```

若成功，預期看到：
- 每個 service 就緒訊息按順序輸出；
- 結尾顯示：
  - 套件結果（PASS）
  - `mini-shop PR 快速摘要`
  - `* -summary.json` 報告路徑

若失敗，預期看到：
- 失敗 service 與已等待時間；
- 一組可複製的修復命令；
- 當前環境狀態快照；
- 失敗 logs 位置。
