# 安裝與開發

[English](./development.md) · [繁體中文](./development.zh-TW.md)

兩種讀者：**使用 Orbit**（安裝方式、upgrade / rollback / uninstall）與
**在 Orbit 本身上動手**（從原始碼 build、開發流程、dashboard hot reload）。
日常基本流程請參考 [README](../README.zh-TW.md#常用操作)。

## 使用 Orbit

### 平台支援

| 平台 | 支援程度 | 安裝方式 |
|---|---|---|
| macOS arm64 / amd64 | 正式支援 | `install.sh` 或手動下載 release |
| Linux arm64 / amd64 | 正式支援 | `install.sh` 或手動下載 release |
| Windows amd64 / arm64 | Beta | 手動下載 `.exe`，或從 Git Bash/MSYS2 執行 `install.sh` |

使用 container 的環境在 macOS 與 Windows 需要 Docker Desktop，在 Linux
需要 Docker Engine。每個環境也可能宣告額外的 host runtime；`orbit doctor`
會列出並提供安裝提示。同步 environment repository 需要 Git。

Windows build 會執行 release smoke test，但目前不承諾與 macOS/Linux
完全同等。尚未提供原生 PowerShell installer；Windows 使用者應手動下載
`.exe`，或從 Git Bash/MSYS2 執行 Bash installer，container workload 則使用
Docker Desktop。

### Upgrade

下載較新的 release artifact 並替換 binary,或者在 binary 的 distribution config 有提供 install URL 時執行：

```bash
orbit update
```

升級後，正在跑的 daemon 仍是舊的 build。`orbit status` 會用 `⚠ newer orbit … — orbit daemon restart` 提醒你。執行 `orbit daemon restart` 讓新的 binary 生效。

每次 upgrade 會把前一版 binary 保留在 `<path>.prev`（例如 `~/.local/bin/orbit.prev`）。
Installer 會先驗證 checksum 與下載 binary 回報的版本，確認成功後才碰目前
安裝；替換檔會放在 target 同一個 filesystem，再用 atomic rename 安裝。
除非明確允許 downgrade，否則不會用舊版覆蓋較新的版本。

### Rollback

```bash
orbit update --rollback
orbit daemon restart
```

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
./scripts/uninstall.sh
./scripts/uninstall.sh --yes
```

只有確定要永久移除 `~/.orbit/` 時才加上 `--purge`：

```bash
./scripts/uninstall.sh --yes --purge
```

兩種方式都會先停止 Orbit。Docker images 與 workspace 底下的 git checkout
不會被移除。

### 手動下載

從對應的 [GitHub Release](https://github.com/iml885203/orbit/releases)
下載 `orbit-<os>-<arch>`（Windows 為 `.exe`）與 `checksums.txt`。驗證
SHA-256 checksum 後，再把 binary 放到 `PATH`。
Private 演練期間請從已登入的 GitHub CLI 執行 `gh release download`；匿名下載
不可用時，installer 也會自動改用這個機制。

### Agent plugin

每個 source release 都包含 `plugins/orbit-agent`，內有 Codex 與 Claude Code
兩份 manifest。使用 agent 的 plugin 指令，把該目錄加入為 local plugin。
兩份 manifest 與 Orbit release 必須維持相同版本；不要讓新版 plugin 搭配舊版
binary。

## 貢獻 Orbit

### 從原始碼 build

需要 Go 1.25+、Node.js 22+ 與 pnpm 10+：

```bash
git clone https://github.com/iml885203/orbit.git
cd orbit
pnpm --dir ui install
make build        # compiles UI + binary into ./bin/orbit
./bin/orbit init
```

`make install` 會把 dev build 覆蓋到 `~/.local/bin/orbit`，讓它成為你日常用的 orbit。

### 開發流程

```bash
make build      # Build frontend + Go binary
make test       # Run tests
make preflight  # CI 會擋的全部檢查（build、tests、vet、verify-types）— push 前必跑
make lint       # Run linter
make setup      # Install git hooks
```

Release 編號與相容性承諾請見[版本與相容性](versioning.zh-TW.md)。

#### Dashboard 開發（hot reload）

```bash
cd ui && pnpm dev          # Vite dev server on :5173
./bin/orbit daemon start   # API backend + dashboard on :19800
```
