# Configuration 參考

[English](./configuration.md) · [繁體中文](./configuration.zh-TW.md)

Orbit environment 檔案完整的 YAML schema。專案可以把 `orbit.yaml` 放在程式碼
旁；團隊也可以透過 `~/.orbit/envs/` 發布檔案，再用 `orbit switch <name>`
選擇。可參考 `envs/example.yaml` 這個可直接執行的範例。

**目錄**

- [Config 選擇](#config-選擇)
- [繼承 environment](#繼承-environment)
- [最上層結構](#最上層結構)
- [`settings`](#settings)
- [`tracing`](#tracing)
- [`containers`](#containers)
- [`services`](#services)
- [`groups`](#groups)
- [`externals`](#externals)
- [選用 extension：`sqlserver`](#sqlserver)
- [Extension 擁有的 section](#extension-擁有的-section)
- [User settings (`~/.orbit/settings.json`)](#user-settings-orbit-settings-json)
- [變數替換](#變數替換)
- [底層 runtime 覆寫](#底層-runtime-覆寫)

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

## 繼承 environment

當一個 environment 只修改另一個 environment 的部分設定時，使用 `extends`：

```yaml
extends: backoffice.yaml

services:
  antares:
    env:
      ASPNETCORE_ENVIRONMENT: docker
      ENFORCE_PERMISSION_CHECKS: "true"
```

Parent path 以 child 檔案所在目錄為基準解析。每一層 mapping 都依 key 合併：
child 的值會取代同名 scalar 與 list，child 未出現的 key 則從 parent 繼承。
因此上述範例只需取代兩個環境變數，不必重複 `antares` 的其他設定或其他
services。不支援 list append 或 delete marker。

Inheritance 只允許一層；`extends` 指向的檔案不能再使用 `extends`。Orbit 會先
分別替換各檔案中的 `${VAR}` 與 `${VAR:-default}`，接著合併，再以 strict mode
decode 並驗證結果。合併後的所有相對 config paths 都以 child 檔案的目錄為
基準解析。例如 `extends: base/shared.yaml` 的 parent 若提供
`path: ./apps/api`，`apps/api` 會解析到 child 旁，而不是 `base/` 之下。

Parent path 必須是相對路徑。可將抽象 parent 放在 `envs/base/` 這類子目錄，
避免它出現在可選 environments 中；`orbit source sync` 仍會複製該目錄，並
透過每個 child 驗證內容。編輯 child 或 parent 都會把執行中的 environment
標記為 stale。Parent 持續缺失兩次 status 檢查後才會回報，避免 editor
暫存檔案時產生 false alarm。

尚未支援 `extends` 的 Orbit 版本會把此 key 視為 unknown。Shared environment
repository 採用前，請先更新所有使用者。

## 最上層結構

```yaml
version: "3"
extends: base.yaml     # optional 單層 parent；以此檔案所在目錄為基準
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
| `version` | string | yes | Schema 版本，必須是 `"3"`；受管理的 shared env 用 `orbit source sync` 更新，project-local 檔案由 maintainer 遷移；env 比 Orbit 新時執行 `orbit update` |
| `extends` | string | no | Parent environment 檔案；只允許一層，並以 child 檔案的相對位置解析 |
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
有問題的 field 與來源行號；若能明確判斷，還會建議最接近的合法 field 或
enum value。`orbit doctor`、`orbit up` 與 `orbit inspect --json` 都會提供
同一份指引；Orbit 不會安靜忽略它無法理解的設定。

先前的 top-level `previewOnly` field 已移除。請從既有 environment files
刪除此 field；Orbit 會拒絕過時設定並顯示遷移指引。現在每個合法 environment
都可以被選擇、啟用、檢視與管理。

### Schema 2 遷移到 3

Schema 3 將資料庫專屬 seed fields 改成一條在 container 內執行的 command。
Orbit 不會自動修改 project files。

```yaml
# schema 2
version: "2"
seed:
  type: mongo
  database: app
  files: [./seed.js]

# schema 3
version: "3"
seed:
  command: mongosh --quiet app
  files: [./seed.js]
```

shared environment repository 由 maintainer commit schema-3 檔案，使用者再執行
`orbit source sync`。Project-local `orbit.yaml` 則依上方 mapping 直接修改。
command 引用的 credentials 應放在 container `environment`，而不是
seed-specific fields。

## `settings`

套用到整個 env 的全域參數。

```yaml
settings:
  shutdown_timeout: 30s
  health_check_interval: 5s
  docker_poll_interval: 2s
  image_pull_concurrency: 0
```

| Field | Type | Default | 說明 |
|---|---|---|---|
| `shutdown_timeout` | duration | `30s` | graceful stop 最多等多久，超過就 SIGKILL |
| `health_check_interval` | duration | `5s` | `http` / `tcp` / `exec` / Docker `healthcheck` probe 多久跑一次 |
| `docker_poll_interval` | duration | `2s` | container poller 多久呼叫一次 `docker inspect` |
| `image_pull_concurrency` | int | `0` | 同時拉取不同 Docker image 的上限；`0` 維持無上限平行拉取。同一 image 與 platform 的並行請求一律共用一次 pull |
| `health_check.timeout` | duration | `5s` | 當 `health_check` 沒指定 `timeout` 時，每次 probe 套用的預設逾時 |
| `health_check.retries` | int | `12` | `health_check` 未指定時套用的啟動重試次數（以預設 5s interval 計約 1 分鐘）。預算用盡後 Orbit 仍會每 10s 探測，資源恢復時自動回到 healthy |
| `health_check.failure_threshold` | int | `3` | healthy 資源連續幾次 runtime 探測失敗後才轉為 degraded；一次成功即可恢復。`log` 是僅供 readiness 的一次性檢查，不會持續監測 |

Duration 字串使用 Go 格式：`500ms`、`10s`、`2m`、`1h30m`。

Pull 上限只影響 image 下載與解壓縮的並行數；image inspect、container 建立、
health check 與 host process 啟動仍彼此獨立。每個 container 在自己的 image ready
後就會啟動，不必等待環境內所有 image。Docker storage driver 無法穩定處理並行
解壓縮時可設為 `1`。

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

Tracing **預設開啟**。省略整段，或只是加入 `otlp_port` / `max_traces`
調整 receiver，都會維持啟用。只有明確設定 `enabled: false` 才會關閉。

| Field | Type | Default | 說明 |
|---|---|---|---|
| `enabled` | bool | `true` | 啟動 receiver 並對 dev service 注入 `OTEL_*`。只有明確 `false` 才會關閉 |
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
| `ports` | map | no | `alias: "hostPort:containerPort"`。只有一個 endpoint 時，也會供省略的 `health_check.port` 使用 |
| `environment` | map | no | Container env vars。`${VAR}` 會從 host 替換進來 |
| `volumes` | list | no | Docker volume 或 bind mount 字串 |
| `command` | list | no | 覆寫 image 預設的 command |
| `user` | string | no | Container user，對應 `docker --user`，例如 `"0:0"`——需要在全新 named volume 上以 root 執行的 image 會用到 |
| `entrypoint` | list | no | 覆寫 image 的 entrypoint |
| `kind` | string | no | `frontend` \| `backend` \| `infra`（預設）— graph 節點色調 |
| `health_check` | object | no | 判定 container ready 的條件（見下節） |
| `depends_on` | list | no | 必須先 `Healthy` 的其他 container/service 名稱 |
| `seed` | object | no | container healthy 後執行一次的 SQL/init 腳本 |
| `init` | object | no | 有型別的 init hook（Kafka topics、Mongo replica set） |
| `sidecars` | list | no | 隸屬於該 container 的 sidecar container（web UI、agent 等） |

### Database 原生 clients

`orbit query redis`、`orbit query mongo` 與 `orbit query postgres` 會執行
configured container 內已存在的 client。即使 container 使用 domain-specific
名稱，也能以 `redis`、`mongo`、`postgres` 或 `postgresql` port alias 讓 Orbit
找到 target：

```yaml
containers:
  primary-data:
    image: postgres:18
    ports:
      postgres: "5432:5432"
```

只有一個符合的 container 時 Orbit 會直接選取。有多個時不會依 map 順序猜測；
command 會列出名稱並要求 `--container <name>`。其他 database client 可使用
`orbit exec <container> <client...>`。

這些 commands 是連線便利層，不是泛用 schema lifecycle。選用的 `sqlserver`
workflow 擁有 SQL Server Database Project 的 diff、publish 與 reset semantics，
因為這些操作無法誠實地套用到所有 database。

### Port 解析

Default runtime 會把每個宣告的 host port 視為固定值。衝突是一個會指出占用
程式與 remedy 的錯誤，因為位址被默默換掉會破壞 Orbit 管不到的消費者——寫死
的 env 值、書籤與測試設定。`orbit doctor` 會在任何東西啟動前，回報已被占用
的宣告 port 與其擁有者。

以 `--instance` 選擇的 named runtime 會把宣告的 host ports 視為 preferences，
並持久化可用的解析 port 以維持 restart 穩定。請使用 `up`、`status` 或
`instance list` 回報的實際 endpoints。完整 ownership 與 cleanup model 見
[隔離的 runtime instances](instances.zh-TW.md)。

Orbit 會把 `<ALIAS>_PORT` 注入 host service；只有一個 port 的 service
也會收到慣用的 `PORT` 變數。

舊的 `{preferred, target}` 寫法仍可解析，意義與 `"host:target"` 相同的
固定 port——它原本代表的搬移行為已移除。若團隊想在不改檔案的情況下做
機器層級的覆寫，port 值內可以使用 environment substitution
（`http: "${API_PORT:-28080}"`）。

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
  port: <int>           # http、tcp；只有一個 port 時可省略，http 會優先用同 scheme 的 alias
  scheme: http          # http；http（預設）或 https
  tls_skip_verify: false # http；本機開發時可略過 TLS 憑證驗證
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
| `http` | `GET <scheme>://localhost:<port><path>`，最終 response 為 2xx 才算通過 |
| `tcp` | 對該 port 建立一條 TCP 連線 |
| `exec` | 在 container 內執行 `command`，exit code 0 視為 healthy |
| `log` | tail container logs 比對 regex（僅為一次性 readiness 訊號，不是 runtime liveness probe） |
| `healthcheck` | 直接用 image 自己的 `HEALTHCHECK`，由 `docker inspect` 回報 |

`http` 與 `tcp` 在 resource 只有一個 port 時可省略 `port`。有多個 port
時，`http` check 會優先選擇與 scheme 同名的 `http` 或 `https` alias；只有
endpoint 仍有歧義時，Orbit 才要求明確指定 health-check port。

HTTP check 的 `scheme` 預設為 `http`；HTTPS-only endpoint 可設為
`scheme: https`。憑證驗證預設保持開啟；使用 self-signed 或 development
certificate 的本機服務，可針對該 check 明確設定 `tls_skip_verify: true`。
此放寬會沿用到 redirect：該 check 的每一跳都會略過驗證，包含 redirect 到
其他 host 的情況。最終 response 仍必須是 2xx。

Resource 沒有明確設定 `health_check` 時，如果只宣告一個 port，或多個 port
中包含 `http`，Orbit 會自動使用 TCP readiness check。Host service 與
container 採用同一規則：宣告 endpoint 就足以讓 Orbit 等到它可用再放行
dependent。若「能連線」仍不足以代表應用程式 ready，請明確使用 HTTP、
log、`exec` 或 image `healthcheck` probe。沒有 port 的 host worker 會先
通過一小段 startup stabilization window；沒有 port 的 container 若需要
readiness 保證，則必須宣告明確 probe。當其他 resource 依賴一個沒有 probe、
且 Orbit 無法選定單一 endpoint 的 container 時，`orbit doctor` 會直接警告
並指出應補上的 `health_check` path，不會默默暗示它已經 ready。

### `seed`

```yaml
seed:
  command: mongosh --quiet app
  files:
    - envs/seeds/mongo/001-something.js
```

執行 `orbit seed` 時，Orbit 會在已啟動的 container 內執行 `command`，
並依序把每個檔案送到 standard input。這個模型不綁定資料庫；command
可以使用 image 內附的任意 client。例如 PostgreSQL：

```yaml
seed:
  command: psql -v ON_ERROR_STOP=1 -U app -d app
  files:
    - envs/seeds/postgres/001-schema.sql
    - envs/seeds/postgres/002-data.sql
```

command 可讀取 container environment，因此 credentials 留在 container
環境，不需要 Orbit 專屬 seed 欄位。已套用的 seed 會依 command 與檔案內容
記錄；之後重跑 `orbit seed` 會跳過，除非加 `--force`（這是 CLI flag，
不是 config 欄位）。command 或檔案改變時，Orbit 會要求 `--force`，不會
默默把內容套用到不同 target。

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
    kind: backend                 # frontend | backend | infra — graph 節點色調
    path: ./src/my-api            # 預設為 orbit.yaml 所在目錄
    command: pnpm dev
    url: https://localhost:5001
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
| `type` | string | no | 常見 Python、Node、Bun 與 Go command 會自動推斷。要使用 build-then-run（`watch: true` 則 `dotnet watch`）時明確設定 `dotnet` |
| `kind` | string | no | `frontend` \| `backend`（預設）\| `infra` — 決定 graph 節點顏色；只表身分，不表健康 |
| `path` | string | no | `.csproj` 路徑（dotnet）或 `command` 的工作目錄；預設為 `orbit.yaml` 所在目錄 |
| `command` | string | no | 非 dotnet type 要執行的 process。引號可組成單一 argument，`$VAR` 會從 service environment 展開；Orbit 不會隱含啟動 shell |
| `watch` | bool | no | 僅 dotnet：用 `dotnet watch` 取代編譯後執行（預設 `false`） |
| `url` | string | no | 標準 URL；`orbit open <service>` 會用它。已有 `http` 或 `https` port 時可省略 |
| `ports` | map | no | 固定數字。單一 port 可供省略的 health check 使用；`http`/`https` 也會成為預設 open URL |
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

當 `depends_on` 中的 service 宣告 `url` 或 `http`/`https` port，Orbit 會把
endpoint 以 `<DEPENDENCY_NAME>_URL` 注入下游 process：

```yaml
services:
  catalog-api:
    type: python
    path: ./catalog
    command: python3 main.py
    ports:
      http: 3001

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

`orbit up --group back_office` 會啟動該 group 的 resources 與其相依；
`orbit down --group back_office` 只停止該 group 自己的 resources，讓其他
groups 仍可使用共享相依。`enabled: false` 的 group 預設會被跳過，除非在
command line 明確指定。

Orbit 會在 lifecycle action 前驗證 group 名稱；未知名稱會失敗並列出可用
groups，不會變成看似成功的 no-op。Resource names、`--group` 與 `--infra`
在 `up` 與 `down` 都使用同一套互斥選取模式，不能混用。

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
      databases: [AccountsDev, AccountsE2E]
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
workspace-relative 的 `.sqlproj` 檔案。省略 `databases` 時，Orbit 會從
`.sqlproj` 檔名推導一個 database name（例如 `Orders.sqlproj` 會得到
`Orders`）。若一份 project schema 要 publish 到多個 database，請用
`databases` 明確列出至少一個名稱。每個 database name 只能出現在一個 project 下；
兩條 project path 若推導出相同 basename，validation 會拒絕，不會靜默指向
同一個 database。Orbit 不會掃描相鄰目錄，也不會從
container 名稱或 image 猜測。見 [sql-workflow.zh-TW.md](sql-workflow.zh-TW.md)。

## Extension 擁有的 section

Core schema 會保留由目前 binary 所編譯進的功能 package 註冊之最上層 section。
它們的名稱與形狀不屬於中性 core schema；請查閱該發行版的功能文件。例如,
`claim`(tunnel)與 `sqlserver`(SQL workflow)由受 gate 掃描的功能 package
註冊。若 binary 沒有註冊對應的 handler,就會拒絕該 feature 擁有的 section。

## User settings (`~/.orbit/settings.json`)

env config 透過環境變數（例如 `WORKSPACE_ROOT`、`API_ROOT`）參照專案目錄。如果你的
checkout 不在預設 workspace 位置，請用 `orbit settings` 設定：

```bash
# WORKSPACE_ROOT：移除 source，再用 --workspace 重新新增。
orbit settings set-env API_ROOT /path/to/api
orbit settings list                              # 顯示目前的值
```

任何支援的流程都不需要手動編輯 `settings.json`。這些指令會先驗證路徑才寫入，
所以打錯字會立刻被指出，而不是之後才以「service 目錄不存在」的形式浮現。

Source workspace 會以 `WORKSPACE_ROOT` 輸出給該來源的 env config。
只有所選 environment 實際引用 workspace projects 時，`orbit init` 才會詢問；
只有 containers 或自給自足的 environment 不會把任意 current directory 存成
workspace。它可以在 daemon 尚未啟動時設定。其他引用的變數由 `set-env` 存放在
`user_env` 底下。

上面的指令實際產生的內容如下（僅供參考）：

```json
{
  "source": { "name": "team", "workspace": "/path/to/workspace" },
  "user_env": {
    "API_ROOT": "/path/to/api"
  }
}
```

選擇 environment 後，`orbit doctor` 會驗證解析完成的 service paths。對於由
Python interpreter 啟動的 `type: python` service，也會使用該 interpreter
檢查專案根目錄的 `requirements.txt`，過程不會下載或安裝任何內容。若
requirements 尚未滿足，Doctor 會提供明確的 `pip install` 指令。command
已指向 virtual environment 時會沿用它；否則套件只會安裝到 user
installation，且只有 interpreter 表明需要時才加入 pip 的
externally-managed override。

對於 `package.json` 有宣告 dependencies 的 `type: node` service，Doctor
也會確認 packages 已安裝。Install command 會優先採用 `packageManager`，
其次依 lockfile 判斷，否則使用 npm；workspace metadata 與已安裝 packages
可放在最近的 repository root。除了 package-manager command，像
`node server.js` 這種直接啟動方式也會檢查，Doctor 也會確認選定的 package
manager 本身已安裝。若 manager 缺少，這會是唯一與 packages 相關的下一步；
工具可用後 Orbit 才會繼續檢查 project packages。

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

## 底層 runtime 覆寫

這些 variables 會暴露 default runtime 使用的底層隔離 primitives。平行
checkout、agent 與 CI job 應優先使用 `--instance`；詳見
[隔離的 runtime instances](instances.zh-TW.md)。

| Variable | 用途 | Default |
|----------|---------|---------|
| `ORBIT_HOME` | `envs/`、socket、PID、settings 的路徑 | `~/.orbit` |
| `ORBIT_NAMESPACE` | Docker container / label 名稱的 prefix | empty（legacy `orbit-<svc>`） |
| `ORBIT_DASHBOARD_PORT` | 固定 dashboard TCP port；未設定時若衝突會自動避開 | `19800` |
| `ORBIT_LOG_LEVEL` | Daemon log 等級（`debug`/`info`/`warn`/`error`） | `info` |

請在 `orbit up` 之前設定底層 overrides——daemon 已經跑起來後再改的話，需要 `orbit daemon restart`。

## 延伸閱讀

- [docs/architecture.md](architecture.md) —— 這些欄位如何餵給 orchestrator
- [envs/example.yaml](https://github.com/iml885203/orbit/blob/main/envs/example.yaml) —— 完整可執行範例
