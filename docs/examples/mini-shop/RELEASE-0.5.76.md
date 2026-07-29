# mini-shop 0.5.76

## 1.0 前 UX 交付（新手心智模型儀表板）

## 重點改動

- 升級 `release-check.sh` 的輸出內容：除了 `compact suite / success / first_run`，加入
  - `decline scenario`（付款失敗情境）
  - `decline_ms`
  - `onboarding_score`（0-100）
- 將 `onboarding_score` 寫入 `release-check-summary.json` 與 `release-check-body.md`，讓每次 release 都能直接比較「這版是否真正降低新手心智負擔」。
- 擴充 `README.md` 的 release 檢核章節，明確標示可貼到 PR/release 的交付欄位。
- `print_release_blurb` 會同時輸出「本次缺哪一塊、接下來先修」的下一步建議，讓 review 不再靠猜。

## 對使用者心智模型的直接作用

- 用戶可以一眼看出：成功與失敗都可以重播，否則不算完成新手交付。
- 有了 `onboarding_score`，團隊能比較連續版本是否在「第一輪能否 demo」上持續進步，而不是只看單點綠燈。
- release check 結果更容易對外解釋，降低內部判斷摩擦。

## 本輪驗收（建議）

- `bash docs/examples/mini-shop/scripts/release-check.sh quick|full|all` 會輸出新增的交付儀表板欄位。
- 任何 `onboarding_score` 低於 70 或 `decline scenario=false` 的版本建議先補齊再推進。
