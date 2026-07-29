# mini-shop 0.5.78

## 1.0 前 UX 交付（打磨節奏可見化）

## 重點改動

- 補強 `DEMO-REFERENCE-MATRIX.md`，把外部參考專案（dotnet/eshop、eShopOnWeb、microservices-demo、example-voting-app、weaveworks/awesome-microservices）整理成「可借取的 20% / 不直接採用」決策，明確支援「參考，不重寫」方向。
- 補強 `DEMO-FAMILY-PLAN.md`：回應「mini-shop 太簡單嗎」的判斷框架，明確定義 3 層 demo 族譜（主 demo / 進階引用 / 不做的事），避免把 1.0 前壓到過早複雜。
- 連動 1.0 心智模型目標：把重點放在「一筆交易的可理解敘事」而非服務數量。
  - 一筆成功流程：先定義三件事（服務就緒、成功、關聯）
  - 一筆失敗流程：直接對到修復卡片
  - 失敗修復回歸：在 2 步內可回到可 demo 狀態。

## 對使用者心智模型的直接作用

- 不是把專案做得更大，而是讓新手在第一次接觸時知道「我現在要做什麼、如果壞了下一步做哪個」。
- 把「參考大型專案」與「主 demo」切開，避免新手被過度上下文淹沒。
- 對 1.0 前最重要的是一致的可交付門檻：第一輪可成功可失敗、可修復可回歸。

## 本輪驗收

- `DEMO-REFERENCE-MATRIX.md`：新增了 `eShopOnWeb`、`awesome-microservices` 等參考連結與取用建議。
- `DEMO-FAMILY-PLAN.md`：新增「為何不直接重寫更重」的 3 層策略。
- 建議搭配 `bash docs/examples/mini-shop/scripts/release-check.sh quick` 把 `onboarding_score` + `first_run_within_60s` 保持穩定。
