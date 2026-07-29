# mini-shop v0.5.44（UX 打磨：服務就緒計數對齊 / 模式感知）

## 為什麼這次
使用者在新手流程中仍有「看似全綠/其實未全綠」的認知偏差，特別在 `advanced` 或不同 service 組合下，目標計數不一致。這版讓 demo 的可讀性回饋回到「只顯示正在追蹤的服務」，降低猜測。

## 這版做了什麼
- 以 `BASELINE_SERVICE_SET` / `ADVANCED_SERVICE_SET` 統一追蹤服務集合，避免把 non-primary 服務（例如 web / 其它可觀測元件）混進就緒判斷。
- 把前台所有服務就緒判斷改成模式感知（新手/進階）的一致邏輯：
  - `getTrackedServiceNames`
  - `getTrackedServiceCount`
  - `getReadyTrackedServiceCount`
  - `getTrackedServiceStatusPayload`
- 將 `index.html` 中剩餘固定的 `8/8`/硬編 service 計數文案，改為「依目前模式動態顯示」，避免 advanced 與 baseline 混淆。
- 讓 `runScenario / 一鍵流程 / 下一步建議` 等 flow 在執行前，都先用同一套就緒判斷，避免在未就緒時誤導按鈕可點。
- 文件同步更新 `docs/examples/mini-shop/README.md`，把「8/8」改為「基線服務/模式化」表述。

## 使用者體驗效果
- 新手可看到一致的「服務就緒」訊號，不再因環境切換看見跳動的不同目標數。
- 遇到錯誤時，建議行為會指向實際未就緒服務，而不是 list 中第一個非目標服務。
- 進階模式進入時，觀測服務不再干擾基線 demo 判斷。

## 驗收
- `make preflight`
- 1.0 前 UX smoke check（建議）
- 實際操作觀察：
  - 從新手模式 -> 進入 advanced 時，服務就緒文案可平順切換；
  - 進階啟動下故障修復流程不再把觀測服務誤計入 baseline 就緒。
