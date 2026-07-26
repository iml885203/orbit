# <img src="ui/public/orbit-logo-badge.svg" width="32" height="32" alt=""> Orbit

用一份宣告式環境設定，同時管理本機開發 process 與 containers。

[安裝](docs/development.zh-TW.md) · [設定](docs/configuration.zh-TW.md) ·
[架構](docs/architecture.zh-TW.md) · [Agent CLI](docs/agent-cli.zh-TW.md) ·
[English](README.md)

Orbit 會依 dependency 順序啟動服務、檢查健康狀態、串流 logs，並透過常駐
daemon 維持環境。相同操作也能從本機 dashboard 與穩定的 JSON CLI contract
執行。

## 為什麼使用 Orbit？

真實的本機開發環境通常不只是一份 container 設定。應用程式會在 host 上執行
以便快速修改，database、queue、cache 則跑在 containers 裡。Orbit 為整個環境
提供單一 control plane：

- **混合 runtimes：** 在同一張 dependency graph 管理 host processes 與
  containers。
- **共享 environments：** 從任何 Git repository 同步有版本的 YAML。
- **Agent automation：** 使用 `orbit.cli.v1` JSON envelope 可靠控制環境。
- **本機診斷：** CLI 與 dashboard 都能查看 health、logs、history、traces 與設定。
- **選用 workflows：** 不重建 image 就發布 SQL schema，或將授權的 callback
  path 導到本機服務。

設計取捨與比較請見[為什麼是 Orbit](docs/why-orbit.zh-TW.md)。

## 安裝

Repo 還是 private 時，請先登入 [GitHub CLI](https://cli.github.com/)：

```bash
gh auth setup-git
gh api -H "Accept: application/vnd.github.raw+json" \
  repos/iml885203/orbit/contents/scripts/install.sh | bash
orbit init
```

Repo 公開後可改用不需登入的指令：

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
orbit init
```

預設設定會使用 [Orbit demo 環境](https://github.com/iml885203/orbit-demo)：
在本機執行的 Python 標準函式庫 service，搭配 container 內的 Redis。Orbit
不會安裝 Python 或其他專案 runtime；`orbit doctor` 會回報所選環境需要的工具。

升級、rollback、移除、手動下載與從 source build
請見[安裝與開發](docs/development.zh-TW.md)。

## 常用操作

```bash
orbit up --infra             # 只啟動 containers
orbit up                     # 啟動 services 與 dependencies
orbit status --json          # 取得穩定的 machine-readable 狀態
orbit logs api -f            # 追蹤單一 service
orbit env sync --json        # 更新共享 environment files
orbit switch development     # 選擇 environment
orbit doctor --json          # 診斷本機設定
orbit down                   # 停止環境
```

### Database workflow

```bash
orbit db diff AppDB
orbit db publish AppDB
orbit db reset AppDB         # 破壞性操作：丟棄本機資料
```

`publish` 在保留資料的情況下套用 schema diff；`reset` 會恢復乾淨的開發資料庫並
要求確認。詳情請見 [SQL workflow](docs/sql-workflow.zh-TW.md)。

### Callback tunnels

```bash
orbit tunnel claim /callbacks/example -p 8080
```

只 claim 已授權的開發或 staging path。Callback traffic 可能含 credentials 或個人
資料。詳情請見 [Tunnel claims](docs/tunnel-claim.zh-TW.md)。

## 搭配 AI agent

Repository 內含 `plugins/orbit-agent`，可包裝成同版本的 Codex 與 Claude plugin。
它會要求 agent 先檢查狀態、使用 `--json`，並在破壞性 database 操作前確認。

不安裝 plugin 時，也可以讓 agent 直接閱讀
[skill](plugins/orbit-agent/skills/orbit/SKILL.md) 與
[JSON contract](docs/agent-cli.zh-TW.md)。

## Dashboard

Daemon 啟動後執行 `orbit open`。Dashboard 提供：

- dependency graph 與 service controls；
- environment preview 與切換；
- logs、設定與 health diagnostics；
- 本機 database 變更檢查與發布；
- traces 與 request playback。

Dashboard 位於 <http://localhost:19800>。

## 文件

使用者：

- [設定](docs/configuration.zh-TW.md)
- [SQL workflow](docs/sql-workflow.zh-TW.md)
- [Tracing](docs/tracing.zh-TW.md)
- [Tunnel claims](docs/tunnel-claim.zh-TW.md)
- [疑難排解](docs/troubleshooting.zh-TW.md)
- [版本與相容性](docs/versioning.zh-TW.md)

導入者與 contributors：

- [團隊導入](docs/team-adoption.zh-TW.md)
- [架構](docs/architecture.zh-TW.md)
- [開發](docs/development.zh-TW.md)
- [Agent CLI contract](docs/agent-cli.zh-TW.md)
- [程式碼慣例](docs/CODE_CONVENTIONS.zh-TW.md)

Repository-local `/orbit-review` skill 會依這些規範檢查變更。每次 commit 前執行
`make preflight`。

## 授權

[MIT](LICENSE)
