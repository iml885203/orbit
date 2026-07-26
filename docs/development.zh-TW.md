# 安裝與開發

[English](./development.md) · [繁體中文](./development.zh-TW.md)

兩種讀者：**使用 Orbit**（安裝方式、upgrade / rollback / uninstall —— 到 _手動下載_ 為止的章節）與**在 Orbit 本身上動手**（從原始碼 build、開發流程、dashboard hot reload —— 從 _從原始碼 build_ 開始）。日常基本流程請參考 [README](../README.zh-TW.md#五分鐘上手)。

## 使用 Orbit

### Upgrade

下載較新的 release artifact 並替換 binary,或者在 binary 的 distribution config 有提供 install URL 時執行：

```bash
orbit update
```

升級後，正在跑的 daemon 仍是舊的 build。`orbit status` 會用 `⚠ newer orbit … — orbit daemon restart` 提醒你。執行 `orbit daemon restart` 讓新的 binary 生效。

每次 upgrade 會把前一版 binary 保留在 `<path>.prev`（例如 `~/.local/bin/orbit.prev`）。

### Rollback

```bash
orbit update --rollback
orbit daemon restart
```

如果連 orbit 自己都壞了，等效的手動步驟是：

```bash
mv ~/.local/bin/orbit.prev ~/.local/bin/orbit
orbit daemon restart
```

### Uninstall

先執行 `orbit down` 停掉 services 與 containers,接著從 `PATH` 移除 `orbit` binary。如果也要刪除本機設定與狀態,再刪除 `~/.orbit/`。Docker images 與 workspace 底下的 git checkout 不會被移除。

### 手動下載

從對應的 [GitHub Release](https://github.com/iml885203/orbit/releases)
下載 `orbit-<os>-<arch>`（Windows 為 `.exe`）與 `checksums.txt`。驗證
SHA-256 checksum 後，再把 binary 放到 `PATH`。

### Agent plugin

每個 source release 都包含 `plugins/orbit-agent`，內有 Codex 與 Claude Code
兩份 manifest。使用 agent 的 plugin 指令，把該目錄加入為 local plugin。
兩份 manifest 與 Orbit release 必須維持相同版本；不要讓新版 plugin 搭配舊版
binary。

## 貢獻 Orbit

### 從原始碼 build

需要 Go 1.25+ 與 Node.js 22+：

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
