# Configuration 參考

[English](./configuration.md) · [繁體中文](./configuration.zh-TW.md)

Orbit environment 檔案完整的 YAML schema。專案可以把 `orbit.yaml` 放在程式碼
旁；團隊也可以透過 `~/.orbit/envs/` 發布檔案，再用 `orbit switch <name>`
選擇。可參考 `envs/example.yaml` 這個可直接執行的範例。

**目錄**

- [Config 選擇](#config-選擇)
- [最上層結構](#最上層結構)
- [`settings`](#settings)
- [`tracing`](#tracing)
- [`containers`](#containers)
- [`services`](#services)
- [`groups`](#groups)
- [`externals`](#externals)
- [選用 extension：`sqlserver`](#sqlserver)
- [Extension 擁有的 section](#extension-擁有的-section)
- [User settings (`~/.orbit/settings.json`)](#user-settings-orbitsettingsjson)
- [變數替換](#變數替換)
- [Per-instance 覆寫](#per-instance-覆寫)

## Config 選擇

Orbit 對每個 command 依以下順序選出一份 config：

1. `--config <path>` / `-c <path>`；
2. 目前目錄或任一上層目錄中最近的 `orbit.yaml`；
3. 透過 `orbit switch` 選擇的 environment；
4. distribution 的預設 environment。

因此在專案內執行 command 時，不必重複輸入 flag，就會使用該專案的 intent。
此時 `orbit status --json` 會回報
`data.environment.source: "project"` 與確切的 `selected_path`。將本機檔案
提升為共享 environment repository 後，請移除專案副本，讓共享檔案成為唯一
真相來源。

## 最上層結構

```yaml
version: "2"
previewOnly: false   # optional：此 env 只能檢視、不能在本機啟用
settings:            # global timing, poll intervals
tracing:             # optional 內建 OpenTelemetry receiver
containers:          # Docker-managed infrastructure
services:            # dev processes (dotnet, node, shell)
groups:              # 具名 service 集合 — 批次啟動 + dashboard 分群
externals:           # 非 orbit 系統的佔位節點（kafka edges）
<extension-key>:     # optional：由編譯進 binary 的 extension 擁有
```

| Key | Type | Required | 用途 |
|---|---|---|---|
| `version` | string | yes | Schema 版本，必須是 `"2"`；不符時只提供一個動作：共享 env 較舊時執行 `orbit env sync`，Orbit 較舊時執行 `orbit update` |
| `previewOnly` | bool | no | env 可在 dashboard 檢視但不能在本機啟用（防誤點，非安全邊界） |
| `settings` | object | no | 全域 timeout 與 polling 間隔 |
| `tracing` | object | no | 內建本地 OpenTelemetry receiver（預設開啟 —— 省略整段即自動啟用；加 `enabled: false` 才關閉） |
| `containers` | map | no | Docker container 定義 |
| `services` | map | no | dev process service 定義 |
| `groups` | map | no | 具名 service 集合 — 批次啟動與 dashboard 分群 |
| `externals` | map | no | 非 orbit 管理的 producer/consumer，畫成佔位 graph 節點 |
| `sqlserver` | object | no | 明確啟用 SQL Server Database Projects |
| `<extension-key>` | any | no | 由編譯進此 binary 的 extension 註冊並擁有；可接受的 key 與形狀依發行版而定 |

Orbit 會嚴格 decode core schema 與已註冊的 extension sections。未知 key
或拼錯的 field 會在任何 container 或 host process 啟動前失敗，錯誤也會指出
有問題的 field 與來源行號；Orbit 不會安靜忽略它無法理解的設定。

## `settings`

套用到整個 env 的全域參數。

```yaml
settings:
  shutdown_timeout: 30s
  health_check_interval: 5s
  docker_poll_interval: 2s
```

| Field | Type | Default | 說明 |
|---|---|---|---|
| `shutdown_timeout` | duration | `30s` | graceful stop 最多等多久，超過就 SIGKILL |
| `health_check_interval` | duration | `5s` | `http` / `tcp` / `exec` / Docker `healthcheck` probe 多久跑一次 |
| `docker_poll_interval` | duration | `2s` | container poller 多久呼叫一次 `docker inspect` |
| `health_check.timeout` | duration | `5s` | 當 `health_check` 沒指定 `timeout` 時，每次 probe 套用的預設逾時 |
| `health_check.retries` | int | `12` | `health_check` 未指定時套用的啟動重試次數（以預設 5s interval 計約 1 分鐘）。預算用盡後 Orbit 仍會每 10s 探測，資源恢復時自動回到 healthy |
| `health_check.failure_threshold` | int | `3` | healthy 資源連續幾次 runtime 探測失敗後才轉為 degraded；一次成功即可恢復。`log` 是僅供 readiness 的一次性檢查，不會持續監測 |

Duration 字串使用 Go 格式：`500ms`、`10s`、`2m`、`1h30m`。

## `tracing`

啟用 Orbit 內建的本地 OpenTelemetry receiver。開啟後 daemon 會跑一個
OTLP/HTTP receiver，並對每個 dev service 注入 `OTEL_*` 環境變數,讓 span 自動
流進 Orbit。服務必須設定 OTLP exporter 讀取這些標準環境變數。

```yaml
tracing:
  enabled: false      # opt out —— 整段省略即維持開啟
  otlp_port: 4318     # OTLP/HTTP receiver port（僅綁 loopback）
  max_traces: 1000    # in-memory ring buffer 容量（trace 數）
```

Tracing 是**三態、預設開啟**：整段 `tracing` 省略即為啟用（零設定，Aspire 風格）。
一個存在但沒有 `enabled:` 鍵的 section 會被讀成 opt **out** —— 所以若你只是為了
`otlp_port` / `max_traces` 而加上這段，記得明確設 `enabled: true`。

| Field | Type | Default | 說明 |
|---|---|---|---|
| `enabled` | bool | section 省略時為開 | 啟動 receiver 並對 dev service 注入 `OTEL_*`。section 省略 → 開；明確 `false` → 關；section 存在但沒這個鍵 → 關 |
| `otlp_port` | int | `4318` | receiver 綁定在 `127.0.0.1` 的 OTLP/HTTP port。未釘死時遇衝突會自動往後找 |
| `max_traces` | int | `1000` | ring buffer 上限，超過淘汰最舊；不落盤，`orbit down` 即清空 |

開啟時注入每個 dev service 的變數：`OTEL_SERVICE_NAME`（orbit 的服務名）、
`OTEL_TRACES_EXPORTER=otlp`、`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`、
`OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:<otlp_port>`、
`OTEL_TRACES_SAMPLER=always_on`（本地流量小，全採樣）。

Dashboard 與 CLI 的使用流程見 [tracing.zh-TW.md](tracing.zh-TW.md)。

## `containers`

Container 是 Orbit 啟動、做 health-check、可選擇性 seed 或初始化的 Docker service。

```yaml
containers:
  <name>:
    image: string
    icon: string                # optional Iconify slug for infra dashboard logo
    platform: string            # optional, e.g. linux/amd64
    pull_policy: string         # always | if_not_present | never
    ports:
      <alias>: "<host>:<container>"
    environment:
      KEY: value
    volumes:
      - volume-name:/mount/path
    command: [string]
    health_check: {...}
    depends_on: [name, ...]
    seed: {...}
    init: {...}
    sidecars: [{...}]
```

### Container 欄位

| Field | Type | Required | 說明 |
|---|---|---|---|
| `image` | string | yes | 完整 image reference，支援 `${VAR}` 變數替換 |
| `icon` | string | no | graph dashboard infra logo 用的 Iconify icon slug，例如 `devicon:postgresql` |
| `platform` | string | no | Platform 覆寫（`linux/amd64` 用於強制 emulation） |
| `pull_policy` | string | no | `always`（預設）、`if_not_present`、`never` |
| `ports` | map | no | `alias: "hostPort:containerPort"`。alias 是 Orbit UI 與依賴串接用的標籤，不會被 `health_check.port` 解析 |
| `environment` | map | no | Container env vars。`${VAR}` 會從 host 替換進來 |
| `volumes` | list | no | Docker volume 或 bind mount 字串 |
| `command` | list | no | 覆寫 image 預設的 command |
| `entrypoint` | list | no | 覆寫 image 的 entrypoint |
| `kind` | string | no | `frontend` \| `backend` \| `infra`（預設）— graph 節點色調 |
| `health_check` | object | no | 判定 container ready 的條件（見下節） |
| `depends_on` | list | no | 必須先 `Healthy` 的其他 container/service 名稱 |
| `seed` | object | no | container healthy 後執行一次的 SQL/init 腳本 |
| `init` | object | no | 有型別的 init hook（Kafka topics、Mongo replica set） |
| `sidecars` | list | no | 隸屬於該 container 的 sidecar container（web UI、agent 等） |

### 可自動調整的 port

專案 port 預設固定；衝突仍會報錯，因為 API 或資料庫偷偷換 port
可能破壞 Orbit 以外的工具。可攜式 demo 或其他可拋棄環境，可以用
`ORBIT_AUTO_PORT_*` fallback，明確允許單一 host port 自動避開衝突：

```yaml
containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "${ORBIT_AUTO_PORT_DEMO_REDIS:-26379}:6379"
    health_check:
      type: tcp
      port: ${ORBIT_AUTO_PORT_DEMO_REDIS:-26379}

services:
  demo-api:
    type: python
    path: .
    command: python3 app.py
    url: http://localhost:${ORBIT_AUTO_PORT_DEMO_API:-28080}
    ports:
      http: "${ORBIT_AUTO_PORT_DEMO_API:-28080}"
    health_check:
      type: http
      path: /health
      port: ${ORBIT_AUTO_PORT_DEMO_API:-28080}
```

偏好 port 可用時 Orbit 會照常使用；若已被占用，Orbit 會選擇可用的
host port，同步更新 health check 與 loopback URL，並把
`<ALIAS>_PORT` 注入 host service。只有一個 port 的 service 也會收到
慣用的 `PORT` 變數。`orbit status` 與 `orbit open <service>` 永遠使用
實際選定的 runtime port；daemon 重啟後，既有 managed container
也會沿用相同 port。

### Dashboard 上的 infra logo

Graph dashboard 的節點與抽屜 header 會顯示 infra container 的 logo。當 env 需要控制 logo 時，把 `containers.<name>.icon` 設成 Iconify icon slug：

```yaml
containers:
  postgres:
    image: postgres:16
    icon: devicon:postgresql
```

如果省略 `icon`，graph UI 會顯示一個通用齒輪 icon。Orbit 不會從 container 名稱或 image 字串推測 logo。

### `health_check`

```yaml
health_check:
  type: http | tcp | exec | log | healthcheck
  # plus type-specific fields:
  port: <int>           # http, tcp — literal port number, not a ports alias
  path: /health         # http
  command: [string]     # exec
  pattern: "ready"      # log — regex against container stdout
  interval: 5s          # 輪詢頻率（預設用 settings.health_check_interval）
  timeout: 30s
  retries: 10
  failure_threshold: 3 # 連續幾次 runtime 失敗後轉為 degraded
```

| Type | 語意 |
|---|---|
| `http` | `GET http://localhost:<port><path>`，任何 2xx 都算通過 |
| `tcp` | 對該 port 建立一條 TCP 連線 |
| `exec` | 在 container 內執行 `command`，exit code 0 視為 healthy |
| `log` | tail container logs 比對 regex（僅為一次性 readiness 訊號，不是 runtime liveness probe） |
| `healthcheck` | 直接用 image 自己的 `HEALTHCHECK`，由 `docker inspect` 回報 |

### `seed`

```yaml
seed:
  type: mongo
  database: PlatformDB
  files:
    - envs/seeds/mongo/001-something.js
```

當 container 進入 `Healthy` 後，依序套用每個檔案。已套用的 seed 會被記錄；
之後重跑 `orbit seed` 會跳過，除非加 `--force`（這是 CLI flag，不是 config 欄位）。
SQL Server seed 必須明確指定登入資訊與 container env key：

```yaml
seed:
  type: sqlserver
  username: sa
  password_env: MSSQL_SA_PASSWORD
  files:
    - envs/seeds/sqlserver/001-something.sql
```

### `init`

針對特定 container 家族、有型別的初始化 hook。

```yaml
init:
  type: kafka_topics
  topics_file: ./data/kafka-topics.yaml
```

```yaml
init:
  type: mongo_rs
  rs_members: ["localhost:27017"]
```

| Type | 用途 |
|---|---|
| `kafka_topics` | 把一份 YAML topic 清單套用到 Kafka broker，idempotent |
| `mongo_rs` | 用給定的 members 初始化 MongoDB replica set |

### `sidecars`

跟著父 container 一起啟動、共用生命週期的 container。

```yaml
sidecars:
  - name: dbgate
    image: dbgate/dbgate:latest
    pull_policy: if_not_present
    ports:
      ui: "13000:3000"
    environment:
      CONNECTIONS: con1
```

## `services`

Service 是 Orbit 拉起、監看、可重啟的 dev process。

```yaml
services:
  my-api:
    type: python                   # dotnet 有特殊行為；其他值是 runtime label
    kind: backend               # frontend | backend | infra — graph 節點色調
    path: ./src/MyApi/MyApi.csproj
    command: pnpm dev           # 非 dotnet type：在 `path` 下執行的指令
    watch: false                # 僅 dotnet：改用 `dotnet watch` 而非 build+run
    url: https://localhost:5001/swagger
    ports:
      http: 5000
      https: 5001
    env:
      KEY: value
    build_env:
      NuGetAudit: "false"       # 僅 dotnet：只給 build 步驟的 env，不進 process
    env_toggles:
      KEY:
        label: "Human label"
        description: "What ON vs OFF means"
        default: true
    pre_start: ["./scripts/migrate.sh"]
    health_check: {...}
    depends_on: [redis]
    kafka:
      produces: [topic-a]
      consumes: [topic-b]
```

| Field | Type | Required | 說明 |
|---|---|---|---|
| `type` | string | yes | `dotnet` 走 build-then-run（`watch: true` 則 `dotnet watch`）；其他值（`node`、`shell`…）在 `path` 下執行 `command` |
| `kind` | string | no | `frontend` \| `backend`（預設）\| `infra` — 決定 graph 節點顏色；只表身分，不表健康 |
| `path` | string | yes | `.csproj` 路徑（dotnet）或 `command` 的工作目錄 |
| `command` | string | no | 非 dotnet type 要執行的 process。引號可組成單一 argument，`$VAR` 會從 service environment 展開；Orbit 不會隱含啟動 shell |
| `watch` | bool | no | 僅 dotnet：用 `dotnet watch` 取代編譯後執行（預設 `false`） |
| `url` | string | no | 標準 URL；`orbit open <service>` 會用它 |
| `ports` | map | no | service 監聽的 port。alias 為 UI 上的 port 標籤；`health_check.port` 收 literal int |
| `env` | map | no | Process env。`${VAR}` 在載入時替換 |
| `build_env` | map | no | 僅 dotnet：傳給 `dotnet build` 的 env，不進執行中的 process |
| `env_toggles` | map | no | dashboard 控制的單一 env key 開關 |
| `pre_start` | list | no | service 啟動前依序執行的 shell 指令；output 會串流到 service log，任一指令 exit code 非 0 即中止啟動 |
| `health_check` | object | no | 形狀同 container 的 `health_check` |
| `depends_on` | list | no | 必須先 `Healthy` 才能啟動的名稱 |
| `kafka` | object | no | `produces` / `consumes` topic 清單 — 在 graph 上畫成 async edge |

例如 `command: python3 -m http.server "$PORT"` 會把 Orbit 實際選到的單一
service port 當成一個 argument。只有確實需要 pipe、redirect 或其他 shell
operator 時，才明確使用 `sh -c "..."`。

### Service dependency URL

當 `depends_on` 中的 service 宣告了 `url`，Orbit 會把 endpoint 以
`<DEPENDENCY_NAME>_URL` 注入下游 process：

```yaml
services:
  catalog-api:
    type: python
    path: ./catalog
    command: python3 main.py
    url: http://localhost:${ORBIT_AUTO_PORT_CATALOG:-3001}
    ports:
      http: "${ORBIT_AUTO_PORT_CATALOG:-3001}"

  checkout-api:
    type: python
    path: ./checkout
    command: python3 main.py
    depends_on: [catalog-api]
```

`checkout-api` 會收到 `CATALOG_API_URL`。若 Orbit 調整 upstream port，
注入值會使用實際選定的 runtime port。Environment 只需宣告一次 endpoint，
不必在每個下游 service 重複。明確設定的 `env.CATALOG_API_URL` 仍視為刻意
override，優先於自動注入；dashboard 也會標示該值來自哪個 dependency。

### `env_toggles`

讓 dashboard 不用改 YAML 就能把單一 env var 開或關。

```yaml
env_toggles:
  ORBIT_VITE_API_TARGET:
    label: "Override API Target"
    description: "ON: use orbit's value / OFF: let project .env win"
    default: true
```

ON 時，Orbit 注入 `env.ORBIT_VITE_API_TARGET` 的值；OFF 時，這個變數會從 process env 移除，讓 service 自己的 `.env` 生效。

## `groups`

具名的 service 集合，有兩個用途：批次啟動（`orbit up --group <name>`）與
graph dashboard 的視覺分群（每個 group 畫成一個框住其成員的標籤方框）。

```yaml
groups:
  back_office:
    enabled: true
    color: "#d97706"          # optional CSS 顏色；省略時由名稱推導穩定色相
    services: [api, web, db]
```

`orbit up --group back_office` 只啟動該 group 的 service（以及它們的相依）。
`enabled: false` 的 group 預設會被跳過，除非在 command line 明確指定。

Orbit 會在啟動前驗證 group 名稱；未知名稱會失敗並列出可用 groups，不會變成
看似成功的 no-op。Service names、`--group` 與 `--infra` 是互斥的 `up`
選取模式，不能混用。

## `externals`

orbit 不管理的系統（上游 feed、第三方 provider）的佔位節點，讓進出它們的
async Kafka edge 在 graph 上仍然可見。external 不參與啟動。

```yaml
externals:
  upstream-feed:
    label: "Upstream Feed"
    color: "#8b949e"          # optional
    kafka:
      produces: [sports-odds]
      consumes: []
```

## `sqlserver`

明確為這個環境啟用 SQL Server Database Projects。沒有這個 section 時，
Orbit 不會顯示 SQL Server UI、檢查或設定提示。

以下完整範例同時包含 target container、持久化儲存與 workflow section：

```yaml
containers:
  database:
    image: mcr.microsoft.com/mssql/server:2022-latest
    ports:
      mssql: "14333:1433"
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "${SQLSERVER_PASSWORD}"
    volumes:
      - orbit-sqlserver:/var/opt/mssql
    health_check:
      type: tcp
      port: 14333
      retries: 30

sqlserver:
  target: database
  username: sa
  password_env: MSSQL_SA_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
    - path: database/Orders/Orders.sqlproj
```

啟動 Orbit 前先在 host environment 設定 `SQLSERVER_PASSWORD`。Microsoft
image 初始化時需要 `MSSQL_SA_PASSWORD`；`password_env` 指向同一個 key，
所以 Orbit 會讀取它解析後的值。如果 image 使用不同的 bootstrap key，
請在 target container 同時宣告兩個 keys，並讓 `password_env` 指向 Orbit
應讀取的那一個。

`target` 指向同一個 env 裡的 container；`username` 預設為 `sa`。
`password_env` 是 target container 裡存放密碼的環境變數名稱，Orbit
只在執行時讀取解析後的值，不會儲存或輸出密碼。每個 project path 都是
workspace-relative 的 `.sqlproj` 檔案。Orbit 不會掃描相鄰目錄，也不會從
container 名稱或 image 猜測。見 [sql-workflow.zh-TW.md](sql-workflow.zh-TW.md)。

## Extension 擁有的 section

Core schema 會保留由目前 binary 所編譯進的功能 package 註冊之最上層 section。
它們的名稱與形狀不屬於中性 core schema；請查閱該發行版的功能文件。例如,
`claim`(tunnel)與 `sqlserver`(SQL workflow)由受 gate 掃描的功能 package
註冊。若 binary 沒有註冊對應的 handler,就會拒絕該 feature 擁有的 section。

## User settings (`~/.orbit/settings.json`)

env config 透過環境變數（例如 `WORKSPACE_ROOT`、`API_ROOT`）參照專案目錄。如果你的 checkout 不在預設 workspace 位置，請在 `~/.orbit/settings.json` 設定：

```json
{
  "workspace_root": "/path/to/workspace",
  "user_env": {
    "API_ROOT": "/path/to/api"
  }
}
```

`workspace_root` 是 first-class 設定。只有所選 environment 實際引用 workspace
projects 時，`orbit init` 才會詢問；只有 containers 或自給自足的 environment
不會把任意 current directory 存成 workspace。也可在 daemon 尚未啟動時用
`orbit settings set workspace-root <path>` 設定。它會以 `WORKSPACE_ROOT`
輸出給 env config。其他引用的變數不需要手動編輯 JSON：

```bash
orbit settings set-env API_ROOT /path/to/api
```

這些值會存放在 `user_env` 底下。

選擇 environment 後，`orbit doctor` 會驗證解析完成的 service paths。對於由
Python interpreter 啟動的 `type: python` service，也會使用該 interpreter
檢查專案根目錄的 `requirements.txt`，過程不會下載或安裝任何內容。若
requirements 尚未滿足，Doctor 會提供明確的 `pip install` 指令。command
已指向 virtual environment 時會沿用它；否則套件只會安裝到 user
installation，且只有 interpreter 表明需要時才加入 pip 的
externally-managed override。

`orbit up` 會在啟動 containers 或 host processes 前執行相同檢查，因此錯誤
workspace 或已知缺少的 project dependency 不會留下只啟動一半的
environment。安裝 dependency 仍是使用者明確執行的動作。

## 變數替換

Orbit 在載入時對所有 string 欄位進行 `${VAR}` 與 `${VAR:-default}` 替換。替換來源依優先順序：

1. `orbit` 執行當下的環境變數
2. `~/.orbit/settings.json` 裡的 user settings（例如 `WORKSPACE_ROOT`）
3. config 內 `:-default` fallback

沒宣告的變數會被當作空字串 —— 用 `:-` 給它一個合理的預設值：

```yaml
path: ${API_ROOT:-~/dev/api}
```

## Per-instance 覆寫

在同一台機器上隔離多個 Orbit instance（e2e 測試、sandbox）：

| Variable | 用途 | Default |
|----------|---------|---------|
| `ORBIT_HOME` | `envs/`、socket、PID、settings 的路徑 | `~/.orbit` |
| `ORBIT_NAMESPACE` | Docker container / label 名稱的 prefix | empty（legacy `orbit-<svc>`） |
| `ORBIT_DASHBOARD_PORT` | 覆寫 dashboard 的 TCP port | `19800` |
| `ORBIT_LOG_LEVEL` | Daemon log 等級（`debug`/`info`/`warn`/`error`） | `info` |

請在 `orbit up` 之前設定 —— daemon 已經跑起來後再改的話，需要 `orbit daemon restart`。

## 延伸閱讀

- [docs/architecture.md](architecture.md) —— 這些欄位如何餵給 orchestrator
- [envs/example.yaml](../envs/example.yaml) —— 完整可執行範例
