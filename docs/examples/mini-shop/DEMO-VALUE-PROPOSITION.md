# mini-shop 為 1.0 前主 Demo 的價值主張

你提到「會不會太簡單」很對，這裡先定義我們要回答的不是功能多，而是價值明確。

## 何時是「太簡單」

- 只是一堆服務隨便在 localhost 跑起來，但使用者不知道為什麼要點。
- 服務都綠了，但不知道這筆交易是不是真的有端到端關聯。
- 失敗時只能靠看 log 猜原因，沒有明確下一步。

## 我們目前要避免的方向（已刻意不做）

- 不直接以大型企業級專案（如 eShop 系列）作為主入口。
- 不先要求使用者理解 service、DB、compose 結構。
- 不要求第一次就做 env 追蹤、切換或深度調參。

## 對 mini-shop 這個主 Demo 的定位

- 它要先扛起 3 件事：
  1. 你能很快看到服務/交易/關聯是否可被信任。
  2. 你能在不看原始碼的情況下完成一次成功、一次失敗。
  3. 失敗後能先修回去，不是先拆整個系統。

- 外部參考（只是借靈感，不照抄）：
  - [dotnet/eshop](https://github.com/dotnet/eshop)
  - [GoogleCloudPlatform/microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo)
  - [dockersamples/example-voting-app](https://github.com/dockersamples/example-voting-app)

## 1.0 前階段落地順序

- P0：保留 mini-shop 為主，優化「第一次可完成」路徑（現在這次的重點）。
- P1（本階段已完成）：補 1 個「進階 env（觀測型）」展示，主流程不變。
- P2：把 release 目標改成「每次變更都有一個可讀 demo 檢核結果」，避免只靠 commit 訊息判斷。

