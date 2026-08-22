# 安裝與開發

[English](./development.md) · [繁體中文](./development.zh-TW.md)

兩種讀者：**使用 Orbit**（安裝方式、upgrade / rollback / uninstall）與
**在 Orbit 本身上動手**（從原始碼 build、開發流程、dashboard hot reload）。
日常基本流程請參考[網站概覽](https://orbit.dotw.me/zh-TW/#一份檔案描述整個環境)。

## 使用 Orbit

文件中的 installer 一律安裝已發布的 GitHub Release。Script 雖然放在
`main`，但不會安裝尚未發布的 `main` build。要測試目前 source，請依照
[測試尚未發布的 main](#測試尚未發布的-main)。

### 安裝 Orbit

macOS 或 Linux 使用 Homebrew：

```bash
brew install iml885203/tap/orbit
```

Windows 使用 Scoop（Beta）：

```powershell
scoop bucket add iml885203 https://github.com/iml885203/scoop-bucket
scoop install orbit
```

macOS 或 Linux 使用 verified installer：

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
```

Windows PowerShell（Beta）：

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
```

### 平台支援

| 平台 | 支援程度 | 安裝方式 |
|---|---|---|
| macOS arm64 / amd64 | 正式支援 | Homebrew、`install.sh` 或手動下載 release |
| Linux arm64 / amd64 | 正式支援 | Homebrew、`install.sh` 或手動下載 release |
| Windows amd64 / arm64 | Beta | Scoop、原生 `install.ps1` 或手動下載 `.exe` |

使用 container 的環境在 macOS 與 Windows 需要 Docker Desktop，在 Linux
需要 Docker Engine。每個環境也可能宣告額外的 host runtime；`orbit doctor`
會列出並提供安裝提示，包含 Go。Node service 也會區分缺少 runtime 與尚未
安裝 project packages，並提供精確的 npm、pnpm、Yarn 或 Bun install 指令。
即使 command 直接以 `node` 啟動也適用；Orbit 會讀取 `packageManager` 或
最近 workspace 的 project lockfile，不要求 package manager 一定出現在
command，並會確認該 package manager 已安裝。`orbit switch` 會在使用者執行
`orbit up` 前，回報新 environment 的這些 checks。Runtime checks 會讀取
Go 的 `go.mod`、`.nvmrc`、`.node-version`、
`.python-version`、`.bun-version`、`.tool-versions` 的相關項目，以及 .NET
`global.json`。Orbit 會回報版本不符或互相衝突的宣告，但不會安裝或切換
runtime。這些回報是 warning，不會阻擋 `orbit up`；只有缺少必要 runtime 才會
阻擋。同步 environment repository 需要 Git。

Windows build 會執行 release smoke test，但目前不承諾與 macOS/Linux
完全同等。原生 PowerShell installer 會驗證 release checksum 與版本、保留
上一版 binary、阻止意外降級，並把 Orbit 加入 user PATH。Container workload
使用 Docker Desktop。

### Upgrade

下載較新的 release artifact 並替換 binary,或者在 binary 的 distribution config 有提供 install URL 時執行：

```bash
orbit update
```

透過 package manager 安裝的 Orbit 仍由原本的 package manager 管理。
`orbit update` 不會修改其 binary，而會回報明確指令：
`brew upgrade orbit` 或 `scoop update orbit`。`orbit update --rollback`
也不會直接修改 package manager 管理的 binary。

`orbit update` 會替換目前真正執行的 binary，即使 `PATH` 前面還有另一套
Orbit。若環境正在執行，指令會用新 binary 重新連接，並恢復更新前正在跑的
resources；正常更新不需要再執行 daemon 指令或第二次 `orbit up`。

Windows Beta 請重新執行 `install.ps1` 來更新；Windows 無法可靠地原地替換
正在執行的 `.exe`，因此尚不支援 `orbit update`。

手動替換 binary 後，`orbit status` 仍可能顯示 `Orbit update ready`；
resource mutation 會先暫停，避免跨版本操作。請依照它顯示的精確 recovery
指令處理。`orbit doctor` 也會指出是否有另一套安裝遮蔽目前 binary。

每次 upgrade 會把前一版 binary 保留在 `<path>.prev`（例如 `~/.local/bin/orbit.prev`）。
Installer 會先驗證 checksum 與下載 binary 回報的版本，確認成功後才碰目前
安裝；替換檔會放在 target 同一個 filesystem，再用 atomic rename 安裝。
除非明確允許 downgrade，否則不會用舊版覆蓋較新的版本。若已安裝版本與
release 相同，installer 會成功結束，且不替換 binary 或改動 `.prev` backup。

### Rollback

```bash
orbit update --rollback
```

若環境正在執行，rollback 也會像升級一樣重新連接，並恢復先前正在跑的
resources。

若要回到指定 release，而不是上一版：

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh |
  ORBIT_VERSION=v0.9.0 ORBIT_ALLOW_DOWNGRADE=1 bash
orbit daemon restart
```

指定的 artifact 仍必須通過 release checksum 與版本檢查；被替換的 binary
會成為新的 `.prev`。

如果連 orbit 自己都壞了，等效的手動步驟是：

```bash
mv ~/.local/bin/orbit.prev ~/.local/bin/orbit
orbit daemon restart
```

### Uninstall

先預覽實際路徑，再移除 binary；environments、settings 與本機狀態會保留：

```bash
orbit uninstall
orbit uninstall --yes
```

只有確定要永久移除 `~/.orbit/` 時才加上 `--purge`：

```bash
orbit uninstall --yes --purge
```

命令會先停止 Orbit。Windows Beta 會在命令結束後立即移除仍被執行中程序
鎖定的 `.exe`。Docker images 與 workspace 底下的 git checkout 永遠不會被移除。

### 手動下載

從對應的 [GitHub Release](https://github.com/iml885203/orbit/releases)
下載 `orbit-<os>-<arch>`（Windows 為 `.exe`）與 `checksums.txt`。驗證
SHA-256 checksum 後，再把 binary 放到 `PATH`。

### Orbit plugin

如果希望 Agent 從第一次 setup 就開始引導，而不只是操作既有 environment，
請先安裝 Orbit plugin，再讓它協助安裝 CLI。

Claude Code：

```bash
claude plugin marketplace add iml885203/orbit
claude plugin install orbit@orbit
```

Codex CLI：

```bash
codex plugin marketplace add iml885203/orbit
codex plugin add orbit@orbit
```

安裝後請開啟新的 Agent session。Bundled skill 會偵測缺少的 Orbit CLI、說明
符合平台的 installer，並在執行前詢問。每個 source release 也包含
`plugins/orbit`，供 local plugin 開發使用。Codex 與 Claude manifest 共用一個
獨立發布的日曆版本，因此只更新 skill 時不需要發布新的 CLI。

## 貢獻 Orbit

### 測試尚未發布的 main

需要 Go 1.25+、Node.js 22+ 與 pnpm 10+：

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
