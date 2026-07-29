# mini-shop v0.5.38（1.0 打磨：加速失敗可讀與自救）

## 本次打磨目標

- 讓「發佈前 smoke」在第一步失敗時，不再只回 `fail`，而是直接告訴使用者下一步怎麼自救。
- 修正 CLI 選項使用不一致，避免指令層先踩雷。

## 這版變更

### 1) `smoke-p1.sh` 失敗可讀性

- 新增 `orbit up` 失敗時的直接輸出：
  - 實際失敗命令行
  - `/tmp/mini-shop-p1-orbit-up.log` 完整內容
  - 線上可直接執行的下一步命令建議（status / down）
- 去除不必要/容易誤導的輸出，改為可立即判斷的行動項目。

### 2) 參數一致性修正

- `smoke-p1.sh` 內將 `-g` 改為 `--group`，和現行 orbit CLI 對齊。
- `README.md` mini-shop 進階啟動範例同步改成 `--group`。

## 對 UX 的直接影響

- 使用者遇到 smoke 失敗不再「只知道失敗」，能看到下一步指令，降低排障的心理負擔。
- 減少第一次操作時「指令不可用」的挫折，維持「第一天就能走得動」的 1.0 打磨要求。

## 驗證建議

- 運行 `bash docs/examples/mini-shop/scripts/smoke-p1.sh mini`，若 `orbit up` 失敗，應看到：
  - 明確提示的完整 `orbit ... up --group ...` command
  - 日誌摘要
  - 下一步 status / down 建議

