# <img src="ui/public/orbit-logo-badge.svg" width="32" height="32" alt=""> Orbit

用一份宣告式環境設定，同時管理本機開發 process 與 containers。

[安裝](docs/development.zh-TW.md) · [設定](docs/configuration.zh-TW.md) ·
[架構](docs/architecture.zh-TW.md) · [Agent CLI](docs/agent-cli.zh-TW.md) ·
[English](README.md)

> **1.0 前預覽版：** Orbit 已開放給 early adopters 與 contributors，但 1.0
> UX 與相容性契約仍在打磨中；`v1.0.0` 前可能有 breaking changes。

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

設計取捨與比較請見[為什麼是 Orbit](docs/why-orbit.zh-TW.md)。

## 安裝

以下 installer 會安裝最新發布的 preview release，不是尚未發布的 `main`
build。

公開 demo 需要：

- [Git](https://git-scm.com/downloads) 同步 environment；
- [Docker](https://docs.docker.com/get-docker/) 執行 Redis；以及
- [Python 3](https://www.python.org/downloads/) 執行 host-side services。

Orbit 只安裝自己的 CLI。它會從選定的 environment 偵測 project runtimes，
並在啟動任何東西前提供明確修復方式；不會暗中安裝或更換 project toolchain。

```bash
curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
```

## 五分鐘上手

從任何目錄都能開始。Orbit 會下載並選取公開 demo environment；不需要先 clone
這個 repository，也不需要自己建立 config：

```bash
orbit init --yes
orbit up
orbit status
orbit open demo-shop
```

`orbit init --yes` 會使用公開的
[Orbit demo environment](https://github.com/iml885203/orbit-demo)：包含 storefront、
三個使用 Python 標準函式庫的 API 與各自的 SQLite database，以及 container 內
的 Redis。若缺少必要工具，setup 會在啟動前停止並直接顯示修復方式；若預設
port 已被占用，Orbit 會為整張 dependency graph 自動選擇可用 port。

選擇 **Run checkout**，頁面會顯示同一個 request 如何穿過 catalog、
inventory、Redis 與 orders，並保留 product、reservation、order 的關聯。
**Try 99 items** 則顯示庫存 `7 → 7`、新增 reservation `+0`、新增 order
`+0`，並保留先前成功的 order。這不只是證明五個 processes 能啟動，而是直接
展示 Orbit 協調一套有用的混合 runtime application。完成後再執行
`orbit open`，即可從 dashboard 檢視與控制同一套環境。試用結束後，用一個
指令停止全部資源：

```bash
orbit down
```

準備套用到真實 checkout 時，從[一份 project-local
`orbit.yaml`](docs/local-first.zh-TW.md) 開始。這條十分鐘路徑不需要
environment repository、不需要 `orbit init`，也不需要手動編輯 `~/.orbit`
設定；驗證完成後，文件會再說明何時、如何升級為團隊共享 environment。

需要更多細節時：

```bash
orbit doctor             # 說明尚未滿足的 setup 條件
orbit logs shop-order-api -f # 追蹤 checkout path
orbit status --json      # 穩定的 machine-readable 狀態
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
orbit init --yes
orbit up
orbit status
orbit open demo-shop
```

Unix 的 export 會在 installer 使用 `~/.local/bin` 時，讓目前 shell
立即找到新安裝的指令；把同一行加入 shell profile，之後開啟的 terminal
也能直接使用。Windows installer 會同時更新目前 PowerShell process 與 user
PATH。

`orbit init` 會直接採用這些發行版預設，不會詢問 demo 沒有使用的
environment repository 或 project workspace。從 Orbit source checkout
還可以執行[擴充版 mini-shop](docs/examples/mini-shop/README.md)，查看供
contributors 使用的八 API 版本。`docs/examples/` 底下使用
repository-relative path 的命令不屬於上方的安裝使用者路徑。
Orbit 不會自動安裝專案 runtime 或 dependencies；`orbit doctor` 會回報所選
environment 需要的工具，並在啟動前偵測未滿足的 Python
`requirements.txt`。
`orbit up` 完成後，Orbit 會直接顯示 healthy application 的 URL 與唯一開啟
指令；dashboard 則保留為次要選項。

升級、rollback、移除、手動下載與測試尚未發布的 `main`
請見[安裝與開發](docs/development.zh-TW.md)。

macOS 與 Linux 為正式支援平台；Windows build 為 Beta。詳情請見
[平台支援與安裝](docs/development.zh-TW.md#平台支援)。

## 常用操作

```bash
orbit up                     # 啟動 services 與 dependencies
orbit status --json          # 取得穩定的 machine-readable 狀態
orbit logs shop-order-api -f # 追蹤預設 checkout path
orbit env sync --json        # 更新共享 environment files
orbit switch quickstart      # 選擇預設 demo environment
orbit doctor --json          # 診斷本機設定
orbit down                   # 停止環境
```

一般啟動只需使用 `orbit up`。只有刻意想單獨啟動 containers、不啟動 host
services 時，才使用 `orbit up --infra`。需要縮小啟動範圍時，請選擇指定
resource names 或一個以上的 `--group`；Orbit 會拒絕混用，不會默默忽略部分指令。

使用團隊自己的 environment 時，請把 `demo-shop` 與 `quickstart` 換成
`orbit status` 和 `orbit env list` 顯示的名稱。切換後，Orbit 會在
`orbit up` 前依 project version files 回報不相容的 runtime，或尚未安裝的
project packages。

`orbit env sync` 會更新共享設定；若 active environment 有變更，會詢問是否
立即套用並恢復原本正在運行的 resources，原先停止的 resources 仍維持停止。
官方 demo 會固定在每個 Orbit release 對應的 revision；`orbit env list` 與
`orbit status` 會顯示 repository、要求的 ref 與實際 commit。團隊也可以用
`orbit init --env-repo <url> --env-ref <tag-or-commit>` 得到相同的可重現性。
若要延後中斷，使用 `--no-apply`，Orbit 會顯示之後完成套用的確切指令。

若 environment 設定了對應的 infrastructure container，`orbit query redis`、
`orbit query mongo` 與 `orbit query postgres` 會開啟 container 的原生 client。
PostgreSQL 會沿用 container 的 `POSTGRES_USER` 與 `POSTGRES_DB`；只有要查其他
database 時才需要傳入 `--database`。只有一個符合的 container 時 Orbit 會
直接選取；有多個時則列出候選，要求 `--container <name>`，不會猜測。這些
query helper 與下方選用的 SQL Server schema-project workflow 是兩件不同的事。

## 選用 workflows

除非 active environment 明確啟用對應 extension，相關 dashboard 頁面與設定
檢查不會出現，指令也不會啟用。單純管理 host processes 與 containers 完全
不需要理解或設定這些功能。

### SQL Server Database Projects

```bash
orbit db list
orbit db diff AppDB
orbit db publish AppDB
```

Environment 透過 `sqlserver:` section 明確啟用。`publish` 會保留資料並套用
schema diff；`reset` 與 forced publish 等破壞性路徑都要求確認。詳情請見
[SQL Server workflow](docs/sql-workflow.zh-TW.md)。

### Callback tunnels

```bash
orbit tunnel claim /callbacks/example -p 8080
```

只 claim 已授權的開發或 staging path。Callback traffic 可能含 credentials 或個人
資料。詳情請見 [Tunnel claims](docs/tunnel-claim.zh-TW.md)。

## 搭配 AI agent

Repository 內含 `plugins/orbit-agent`，可包裝成同版本的 Codex 與 Claude plugin。
它會要求 agent 先檢查狀態、使用 `--json`，並在破壞性操作前確認。

不安裝 plugin 時，也可以讓 agent 直接閱讀
[skill](plugins/orbit-agent/skills/orbit/SKILL.md) 與
[JSON contract](docs/agent-cli.zh-TW.md)。

## Dashboard

Daemon 啟動後執行 `orbit open`。Dashboard 提供：

- dependency graph 與 service controls；
- environment preview 與切換；
- logs、設定與 health diagnostics；
- active environment 啟用後才出現的 SQL Server schema 檢查與發布；
- traces 與 request playback。

Dashboard 位於 <http://localhost:19800>。

## 文件

核心使用者文件：

- [在自己的專案使用 Orbit](docs/local-first.zh-TW.md)
- [設定](docs/configuration.zh-TW.md)
- [Tracing](docs/tracing.zh-TW.md)
- [疑難排解](docs/troubleshooting.zh-TW.md)
- [版本與相容性](docs/versioning.zh-TW.md)

選用 workflows：

- [SQL Server Database Projects](docs/sql-workflow.zh-TW.md)
- [Tunnel claims](docs/tunnel-claim.zh-TW.md)

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
