# ![Orbit](ui/public/orbit-logo-badge.svg) Orbit

**專心打造產品，讓 Agent 把專案跑起來。**

Orbit 讓 coding agents 能可靠地跑起專案需要的本機環境、理解失敗原因，
並確認應用程式真的可以使用。

[官方網站](https://orbit.dotw.me/) · [文件](#文件) ·
[English](https://orbit.dotw.me/)

## 開始使用

### 把一個需求交給 Agent

在你想跑起來的專案中，把這段貼給 coding agent：

```text
閱讀 https://orbit.dotw.me，幫我用 Orbit 把這個專案跑起來。
你可以安裝官方 Orbit CLI 與 plugin；安裝其他軟體或進行破壞性變更前先問我。
```

Agent 會理解現有 setup、啟動預期的本機環境、驗證一段實際應用流程，並回報
仍需要你處理的事項。

## 安裝

macOS 或 Linux 使用 Homebrew：

```bash
brew install iml885203/tap/orbit
```

Windows 使用 Scoop（Beta）：

```powershell
scoop bucket add iml885203 https://github.com/iml885203/scoop-bucket
scoop install orbit
```

macOS 與 Linux 也可以使用直接安裝並驗證過的 release：

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
```

Windows PowerShell（Beta）：

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
```

這會安裝最新發布的 preview release。Orbit 只安裝自己的 CLI，不會安裝專案的
runtimes 或 dependencies；`orbit doctor` 會回報所選 environment 需要什麼與
明確的修復方式。升級、rollback、移除與平台細節請見
[安裝與開發](docs/development.zh-TW.md)。

## Dashboard

`orbit open` 會開啟本機 dashboard：dependency graph 與 service controls、
environment 預覽與切換、logs、health diagnostics、traces 與 request
playback。Dashboard 只綁定 loopback。

## 文件

核心使用者文件：

- [在你的專案使用 Orbit](docs/local-first.zh-TW.md)
- [為什麼是 Orbit](docs/why-orbit.zh-TW.md) —— 設計取捨與工具比較
- [設定](docs/configuration.zh-TW.md)
- [Environment sources](docs/environment-sources.zh-TW.md)
- [隔離的 runtime instances](docs/instances.zh-TW.md)
- [在 E2E 測試底下使用 Orbit](docs/e2e-testing.zh-TW.md)
- [Tracing](docs/tracing.zh-TW.md)
- [疑難排解](docs/troubleshooting.zh-TW.md)
- [版本與相容性](docs/versioning.zh-TW.md)

選用 workflows（environment 啟用後才生效）：

- [SQL Server Database Projects](docs/sql-workflow.zh-TW.md)
- [Tunnel claims](docs/tunnel-claim.zh-TW.md)

導入者與 contributors：

- [團隊導入](docs/team-adoption.zh-TW.md)
- [架構](docs/architecture.zh-TW.md)
- [開發](docs/development.zh-TW.md)
- [Agent CLI contract](docs/agent-cli.zh-TW.md)
- [Agent instructions](https://orbit.dotw.me/agent/SKILL.md)
- [Contributing](https://orbit.dotw.me/CONTRIBUTING)

## License

[MIT](https://github.com/iml885203/orbit/blob/main/LICENSE)。Binary 內嵌第三方
dependencies，licenses 與 attributions 列於
[NOTICE](https://github.com/iml885203/orbit/blob/main/NOTICE)。
