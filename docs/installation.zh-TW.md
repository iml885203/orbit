# 安裝與更新

[English](./installation.md) · [繁體中文](./installation.zh-TW.md)

安裝、更新、回退或移除 Orbit 的發行版本。

## 安裝 Orbit

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

## 平台支援

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

## Upgrade

下載較新的 release artifact 並替換 binary,或者在 binary 的 distribution config 有提供 install URL 時執行：

```bash
orbit update
```

透過 package manager 安裝的 Orbit 仍由原本的 package manager 管理。
`orbit update` 不會修改其 binary，而會回報明確指令：
`brew upgrade iml885203/tap/orbit` 或 `scoop update orbit`。`orbit update --rollback`
也不會直接修改 package manager 管理的 binary。

`orbit update` 會替換目前真正執行的 binary，即使 `PATH` 前面還有另一套
Orbit。若環境正在執行，指令會用新 binary 重新連接，並恢復更新前正在跑的
resources；正常更新不需要再執行 daemon 指令或第二次 `orbit up`。

Windows Beta 會把 updater 複製到 installation 外，等待目前 invocation 與已註冊
daemon 結束後才替換 `.exe`、驗證新版並恢復 runtime intent，避免覆蓋 Windows
仍保持開啟的 executable。

手動替換 binary 後，`orbit status` 仍可能顯示 `Orbit update ready`；
resource mutation 會先暫停，避免跨版本操作。請依照它顯示的精確 recovery
指令處理。`orbit doctor` 也會指出是否有另一套安裝遮蔽目前 binary。

每次 upgrade 會把前一版 binary 保留在 `<path>.prev`（例如 `~/.local/bin/orbit.prev`）。
Installer 會先驗證 checksum 與下載 binary 回報的版本，確認成功後才碰目前
安裝；替換檔會放在 target 同一個 filesystem，再用 atomic rename 安裝。
除非明確允許 downgrade，否則不會用舊版覆蓋較新的版本。若已安裝版本與
release 相同，installer 會成功結束，且不替換 binary 或改動 `.prev` backup。

### 自動更新

官方 release build 最多每 24 小時檢查一次新版。Foreground command 不會執行
release network request；detached checker 會下載符合平台的 artifact 與 checksum，
驗證 checksum、候選 binary 回報的版本，以及綁定 tag、commit、artifact 與
checksum file 的 immutable-release attestation，再把 bytes 與 bounded evidence
暫存在 OS user-global Orbit update registry。Source、dirty 與 unbranded build
沒有隱含的官方 update channel。

自動更新預設開啟。只有 command 已結束、所有已註冊 product environment 都沒有
running／restoring resource，而且 daemon convergence 已 idle，Orbit 才會套用已驗證
的更新。Idle daemon 會一起切到 target build；若 product environment 仍在執行，
則延後更新並只顯示一個 `orbit update`／**Update now** 動作，用來恢復原本的
running intent。Mutation 等待 replacement 或 rollback 時，read-only
`status --json` 與 `inspect --json` 仍可讀取 durable transaction。
延後套用只重新驗證本機 staged bytes，不會要求 GitHub request，因此已驗證的
更新可在離線狀態套用。

Verifier 以 Orbit build 內嵌的 root 啟動 GitHub trust repository，TUF metadata
cache 超過一天後會 refresh；無法取得目前可信材料時，本次檢查會 fail closed。
Root rotation 只能透過 TUF 接受。Candidate 完成 staging 後，apply 使用已記錄的
verification result，不會再 refresh trust metadata。

可從 default 或任何 named runtime 設定同一份 installation-wide preference：

```bash
orbit settings set automatic-updates off
```

Off 會停用自動檢查、下載與套用，也不會產生 update-related background network
traffic；明確執行 `orbit update` 仍可進行 bounded foreground check。Homebrew 與
Scoop installation 只檢查與通知，仍由 package manager 管理。Agent plugin 維持
獨立版本並由 host marketplace 更新，不與此機制綁定。

## Rollback

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

## Uninstall

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

## 手動下載

從對應的 [GitHub Release](https://github.com/iml885203/orbit/releases)
下載 `orbit-<os>-<arch>`（Windows 為 `.exe`）與 `checksums.txt`。驗證
SHA-256 checksum 後，再把 binary 放到 `PATH`。

## Orbit plugin

已安裝 CLI 時，一個指令就能把 Orbit skill 放到這台機器上的 coding agent
會載入的位置；不加 `--yes` 只預覽計畫，`orbit skill uninstall --yes` 會還原，
`orbit doctor` 會回報它的狀態。它安裝的內容由
[公開的 skill](https://orbit.ilogan.app/agent/SKILL.md) 說明。

```bash
orbit skill install --yes
```

沒有 CLI、或 CLI 還沒有 `orbit skill` 時，直接為你的 agent 安裝 plugin。
如果希望 Agent 從第一次 setup 就開始引導，而不只是操作既有 environment，
請先這樣安裝，再讓它協助安裝 CLI：

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

安裝後請開啟新的 Agent session。Bundled skill 在需求已授權時會直接安裝缺少的
Orbit CLI，否則先詢問。每個 source release 也包含 `plugins/orbit`，供 local
plugin 開發使用。Codex 與 Claude manifest 共用一個獨立發布的日曆版本，因此只
更新 skill 時不需要發布新的 CLI。
