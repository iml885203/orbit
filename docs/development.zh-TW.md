# 開發

[English](./development.md) · [繁體中文](./development.zh-TW.md)

從原始碼建置 Orbit、執行測試，或維護自訂發行版。

## 貢獻 Orbit

### 測試尚未發布的 main

需要 `go.mod` 指定的 Go 版本、Node.js 22+ 與 pnpm 10+：

```bash
git clone https://github.com/iml885203/orbit.git
cd orbit
pnpm --dir ui install
make install
orbit version
orbit init
```

`make install` 會 build 目前 source，並刻意讓它成為一般 PATH 上唯一使用的
`orbit`。被替換的 binary 會保留在 `~/.local/bin/orbit.prev`，安裝完成也會
提醒重啟 daemon，避免已發布的 release 與 source build 在不知情下互相競爭。
切換後執行 `orbit version`，確認目前測試的是哪個 build。

若只想做一次性 build、不替換已安裝的 release，請執行 `make build`，並明確
使用 `./bin/orbit`。回到已安裝的 `orbit` 前，先停止由該 binary 啟動的 daemon。

### 開發流程

```bash
make build         # Build frontend + Go binary
make test          # Go 與 dashboard 行為測試
make preflight     # 完整 source、靜態、installer、文件 gate；每個完整 commit 跑一次
make test-journeys # 真實 Git、process、Docker 邊界下驗收已 build 的 binary
make lint          # Run linter
make setup         # Install git hooks
```

修改期間先跑最貼近變更的 sociable test；完成一組連貫的產品改善後跑
`make test`，commit 前再跑一次 `make preflight`。不要每修一點就發布；
先把同一個使用者結果相關的修改集中起來，整體 review release candidate，
再發布一個版本。

Release 編號與相容性承諾請見[版本與相容性](versioning.zh-TW.md)。

#### Dashboard 開發（hot reload）

```bash
cd ui && pnpm dev          # Vite dev server on :5173
orbit daemon start         # API backend + dashboard on :19800
```

## 維護自訂的 Orbit 發行版

當純設定不夠用時，團隊可 fork 這個 repository，在 `cmd/orbit` 註冊自己的功能，把額外的 `extension.Extension` 傳給 `app.Main`。每個 extension 可提供 CLI commands、daemon setup 與 hooks、doctor 與 init 行為，以及 distribution defaults；請見 [internal/extension/extension.go](https://github.com/iml885203/orbit/blob/main/internal/extension/extension.go)。這些 package 都在 internal 底下，所以只能在 fork 內使用：Orbit 沒有公開的 Go API（見 [versioning](versioning.zh-TW.md)）。

UI build 在 [ui/vite.config.ts](https://github.com/iml885203/orbit/blob/main/ui/vite.config.ts) 保留三個編譯時 seam：

- `ORBIT_UI_EXT` 用團隊的 navigation、routes、panels、settings sections 與 lifecycle hooks 取代 `$ext` module。
- `ORBIT_UI_TYPES` 取代 generated-types barrel。
- `ORBIT_UI_OUTDIR` 選擇由團隊 binary embed 的 dashboard build output。

這條路線代表團隊必須自行維護 build 與 release line。只在 env 設定無法表達需求時使用，並將中性改進留在這個 repo，避免複製 core。
