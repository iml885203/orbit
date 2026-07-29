# mini-shop 0.5.75

## 1.0 前 UX 交付（第一輪可見價值）

## 重點改動

- 明確補上 mini-shop 為 1.0 前主 demo 的價值敘述：從單純「可跑」轉為「可證明 Orbit 是否能管理 host/container 混合、多服務關聯」。
- 在 `README.md` 新增「為什麼這個 demo 有價值」章節，回答新手在第一分鐘最關心的三件事：
  - 前端與後端是否能完成一筆關聯交易
  - 故障時是否能快速定位
  - 成功與失敗是否都能復現
- 確認 `RELEASE-README` 記錄本輪 0.5.75 的可交付目的，方便每次發版回看「心理模型是否降低」。
- 延續既有 `compact-onboarding` 與 `release-check` 流程，讓每次打磨都能輸出可貼到 release 的交付摘要。

## 對使用者心智模型的直接作用

- 減少第一印象負擔：新手在打開專案後，先看到「為什麼值得做」與「第一輪要成功/失敗驗證」的目的。
- 提升可判斷性：用「成功 + 失敗」形成最短驗證閉環，降低「我到底要再測什麼」的不確定。
- 提升可決策性：release 細節與交付條件可追蹤，便於你直接判斷是否值得在 1.0 前再繼續推進。

## 本輪驗收（建議）

- `bash docs/examples/mini-shop/scripts/compact-onboarding.sh` 可完成 success/decline 並給出下一步。
- `bash docs/examples/mini-shop/scripts/release-check.sh quick|full|all` 會輸出可直接貼到 release 的 body。
- `docs/examples/mini-shop/README.md`「為什麼這個 demo 有價值」段落與 `DEMO-STRATEGY.md` 的方向一致。
