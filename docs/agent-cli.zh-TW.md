# Agent CLI Contract

[English](./agent-cli.md) · [繁體中文](./agent-cli.zh-TW.md)

Orbit 的設計初衷就是讓 coding agent 透過 CLI 來驅動。Agent 在需要解析狀態、決定下一步動作，或從錯誤中復原時，應優先選擇結構化輸出，而非給人看的文字格式。

## Rule of Thumb

程式化的讀寫請使用 `--json`：

```bash
orbit status --json
orbit doctor --json
orbit up --infra --json
orbit logs redis --json
```

給人看的輸出是為 terminal 最佳化的，未來可能變動。JSON 輸出才是 agent 所依賴的 contract。

## JSON Envelope

已轉換到 agent contract 的指令會回傳一個 JSON 物件，最上層結構如下：

```json
{
  "schema_version": "orbit.cli.v1",
  "ok": true,
  "command": "doctor",
  "data": {},
  "error": null,
  "recommended_actions": []
}
```

欄位說明：

| Field | 意義 |
|---|---|
| `schema_version` | Contract 版本，目前為 `orbit.cli.v1`。 |
| `ok` | 成功為 `true`，失敗為 `false`。 |
| `command` | 產生這個 response 的 Orbit 指令。 |
| `data` | 成功時的 command-specific payload。 |
| `error` | 失敗時的結構化 error payload，否則為 `null`。 |
| `recommended_actions` | Agent 應考慮的後續指令。 |

已轉換的指令在 `--json` 模式下若失敗，Orbit 會在 stdout 印出單一 JSON 物件，並以 exit code `1` 結束。

## Error Shape

結構化錯誤使用以下形狀：

```json
{
  "schema_version": "orbit.cli.v1",
  "ok": false,
  "command": "logs",
  "data": null,
  "error": {
    "code": "unknown_resource",
    "message": "unknown resource: redisx",
    "hint": "Run orbit status --json to list configured resources.",
    "retryable": false,
    "next_command": "orbit status --json"
  },
  "recommended_actions": [
    {
      "command": "orbit status --json",
      "reason": "List configured resources and current states."
    }
  ]
}
```

Agent 應優先依據 `error.next_command` 與 `recommended_actions` 行動,而不是從錯誤訊息文字去猜測恢復路徑。

有些失敗沒有任何指令可以推進。`socket_path_too_long` 必須先把 `ORBIT_HOME`
換成較短的路徑，任何 Orbit 指令才能成功，因此它只帶 hint，不帶 `next_command`
或 recommended action。缺少 `next_command` 應理解為「依 hint 處理」，而不是回應格式有問題。

遇到 `resource_port_conflict` 時，`error.next_command` 是該平台用來查看
目前 port owner 的唯讀指令。Resource status 也會提供 `port_conflict`
evidence（`port`、`resource`、可取得時的 owner，以及 `inspect_command`）。
在停止 owner 或於 shared environment 選用其他 host port 前，不應盲目
retry 或讀取 logs。

只有 running daemon 已替該 resource 緩衝輸出時，resource status 才會包含
`logs_available: true`。欄位不存在代表目前沒有歷史輸出可檢查；dependency
卡住的 resource 若沒有這項證據，client 不應把 Logs 當成復原動作。若 resource
從未成功啟動，直接執行 `orbit logs <resource> --json` 會回傳
`logs_unavailable`，而不是空的成功結果。唯一 recommended action 會依即時
port 重驗結果決定：仍被占用時檢查目前 owner，釋放後則只 retry 該 resource。

Degraded host process 可能同時包含 `state_reason`（例如
`exited: exit status 1`）與 `failure_evidence`；後者是針對該次失敗
generation 保存的最後一行有效 application log。它是 lifecycle reason 的
佐證，不是取代 reason。

失敗的 `orbit up --json` 會把證據放在 `data.failed_resources`：每個沒有達到
healthy 的 requested resource 一筆，含 `name`、`state`、`state_reason` 與
`log_tail`（最多 20 行最近的 log）。直接從這裡讀失敗原因，不必再補一次
`orbit logs`。

需要明確授權的破壞性步驟使用穩定的 `confirmation_required` 錯誤碼。被拒絕
時什麼都不會改變；第一個 recommended action 就是加上 `--yes` 的同一個指令。
只有在 caller（或其操作者）確實想要這個破壞性結果時才執行它。

### 穩定錯誤碼

`error.code` 的值屬於版本化契約。完整清單只維護一份,見英文版
[agent-cli.md 的 Stable error codes](agent-cli.md#stable-error-codes)。

## Converted Commands

下列指令在加上 `--json` 時，目前都使用 `orbit.cli.v1` envelope：

每個指令的 JSON 行為只維護一份完整對照表，見英文版
[agent-cli.md](agent-cli.md#converted-commands)。新增或變更指令時只更新那份表。

Lifecycle 指令在 JSON 模式下會抑制裝飾性的進度輸出，讓 stdout 保持可解析。

Lifecycle actions 會依結果提供。成功的 `up` 只回傳一個主要下一步：
`orbit open --json`。啟動失敗時則建議查看 status、根因 resource 的 logs，
並在修正原因後只 restart 該 resource；不會把 agent 導向無關的 setup 診斷。

Lifecycle payload 全面使用 resource vocabulary：

- `requested_resources` 是 daemon 解析後的集合，包含被選中的相依，並排除
  由 groups 篩掉的 resources。
- `resources` 帶有該集合觀察到的最終狀態。
- `degraded_resources` 與 `timed_out_resources` 直接指出未成功的結果，不再
  把 container 稱為 service。
- `environment_changes` 只在啟動前發生 config handoff 時出現，包含
  `previously_running`、`restored_resources`、`started_dependencies` 與
  `unavailable_resources`。

指令只會等待這個集合進入 terminal state。Status 與 inspect 同樣把混合的
host processes 與 containers 放在 `resources`；log payload 與 NDJSON event
則使用 `resource` 標示來源。

command 找到最近的 project `orbit.yaml` 時，status 會把這份實際啟用的 config
回報為 `data.environment.source: "project"`，`selected_path` 則指向找到的
檔案。這個 project context 優先於其他位置選過的 managed environment。
Agent 應依回傳的 source 與 path 判斷，不要從 `~/.orbit/envs` 猜測 active
config。

若另一個 project-local environment 正在執行，status 仍會成功，而且只描述
目前專案設定的 resource。它會設定
`data.environment.context_switch_required: true`、回報 `running_name` 與
`running_path`，並只建議 `orbit up --json`。Doctor 會驗證目前專案，且把執行中
專案占用的 port 視為切換時可釋放。可能誤控另一個專案 resource 的操作指令會
以 `project_context_inactive` 失敗；agent 應遵循唯一的 `orbit up --json`
action。切換成功後，`up` 結果的 `data.context_switch` 會包含兩邊的專案名稱與
啟動前停止的 resource。
Inspect 遵守同一個 ownership boundary，不會把另一個專案的 resource 或
dashboard 回報成目前專案所有。

第一次 setup 前，status 仍是成功的狀態查詢，但會回傳
`data.setup_required: true`、可讀的 `setup_message`，以及唯一的
`orbit init --yes --json` action。若 environment 檔案存在但內容無效，則回傳
`invalid_environment` error，因為 Orbit 無法安全使用它啟動。
若原先選取的 environment 已被改名或移除，status 會改回傳
`data.selection_required: true` 與 `selection_message`，並在
`recommended_actions` 中提供現存 environment 的精確
`orbit switch <env> --json` 指令；這不代表需要重新 init。

`orbit init --yes --json` 不會把 current directory 猜成
`data.workspace_root`。自給自足的 environment 會省略此欄位。若自訂
environment 需要 `${WORKSPACE_ROOT}`，但沒有可證明的 local workspace，
init 會回傳 `service_working_directory_missing`，且唯一 action 為
`orbit settings set workspace-root "$PWD" --json`。
其他未解析的 path variable 會保留自己的名稱，並指向
`orbit settings set-env <NAME> "$PWD" --json`；不會產生 workspace-root
action。

當 GitHub 回覆 environment repo 不存在時，Orbit 會回傳
`env_repo_unavailable`，且不提供 recommended actions。GitHub 對拼錯／不存在
的 repo，以及目前 credentials 看不到的 private repo，刻意使用相同回覆；
因此 agent 不應自動登入或用原指令盲目重試。應先核對 owner 與 repo 名稱，
確認 URL 正確且 repo 為 private 後才進行 Git 認證。能明確判定的認證失敗仍
使用 `env_repo_access`。

已轉換控制指令的穩定 `data.operation` 值：

穩定的 `data.operation` 值只維護一份完整清單，見英文版
[agent-cli.md](agent-cli.md#converted-commands)。

`switch` 會先停掉正在運行的環境，所以在有資源運行且未加 `--yes` 時會回傳
`confirmation_required` 而不動手——切換環境是機器層級的 provisioning 步驟，
不該由 harness 隱式觸發。對 `switch` 而言，`previous_environment_stopped` 表示 Orbit 是否在選取前
停止了原本執行中的 environment。Orbit 停止時不會只為了記錄 selection
而被啟動；`orbit up` 仍是唯一啟動動作。新 env 缺少必要 runtime 或 package
installation 時，`prerequisites_ready` 會是 false；`prerequisites` 使用與
Doctor 相同的 checks，而 Orbit 能判斷修復方式時，`recommended_actions`
會包含可直接執行的指令。

Host service path 尚未解析或不存在時，會使用穩定的
`service_working_directory_missing` error code。`switch` 會讓 daemon 保持
停止，Doctor 或 `up` 會回傳精確的 workspace 設定或 config 修改 action。
執行該 action 後再重試；此時 `up` 尚未啟動任何相依資源。

當先前已有 daemon 在跑時，daemon stop/restart payload 可能包含 `stop_method`。穩定值為 `graceful`、`terminated`、`killed`。

## Legacy JSON Commands

部分指令早於 envelope 存在，為了相容性會刻意保留既有的 JSON 形狀：

| Command | 行為 |
|---|---|
| `orbit daemon status --json` | 回傳 legacy daemon status 物件。 |
| `orbit history --json` | 保持既有的 history payload。 |
| `orbit history gaps --json` | 保持既有的 history gaps payload。 |
| `orbit tunnel claim/list/release --json` | 輸出 Tunlease 形狀的 NDJSON 事件（`schema_version: 1`、每事件一個 `type`），不是 envelope；錯誤用同一形狀走 stdout。 |

## Passthrough Commands

以下指令包裝互動式或外部程式，所以（全域的）`--json` 會被接受但沒有效果——
輸出就是被包裝工具的原始串流：

`orbit exec`、`orbit query redis|mongo|postgres`、`orbit topics *`、
`orbit sqlserver query`、`orbit open`。`orbit seed` 成功時印人類可讀進度；
只有失敗會用錯誤 envelope。

不要預設每一個 `--json` 指令都有 envelope。解析前請先確認該指令各自的 contract。

## Logs

非串流的 logs 回傳單一 envelope：

```bash
orbit logs redis --json
```

成功時的 payload 包含 service 名稱、請求的行數、一個非 null 的 `lines` array，以及輸出是否被截斷。

串流的 logs 使用 newline-delimited JSON：

```bash
orbit logs redis -f --json
```

每一行都是一個完整的 JSON 物件。Agent 應當作 NDJSON 解析，而不是一整個 JSON array。

如果 followed log stream 在 NDJSON 輸出開始後失敗，Orbit 會輸出最後一筆 `type: "error"` 的 NDJSON event，而不是追加另一種格式的 envelope，然後以非 0 exit code 結束。

## Traces

Tracing 預設開啟；只有在使用中的 env 明確設 `tracing.enabled: false` 時才關閉
（見 [tracing.zh-TW.md](tracing.zh-TW.md)）。用 `orbit tracing status --json` 查
receiver 的即時狀態。

```bash
orbit trace --json             # 近期 trace；--limit N 控制筆數
orbit trace <trace-id> --json  # 一條完整 trace 與其 span tree
orbit trace -f --json          # NDJSON 串流 trace-summary 更新
```

trace summary 帶有 `traceId`、`rootService`、`rootName`、`startUnixNano`、
`durationMs`、`spanCount`、`services`（去重、首見順序）與 `status`
（`ok` | `error`）。完整 trace 多了 `spans`，每個 span 有
`traceId`/`spanId`/`parentId`、`service`、`name`、`kind`、
`startUnixNano`/`endUnixNano`/`durationMs`、`status`/`statusMsg` 與
`attributes`。這些欄位名由 contract test（`app/traces_json_test.go`）
釘住 —— 改名視同 breaking change。

`-f --json` 模式下，同一個 `traceId` 會隨 span 累積而重複發出；後到的
event 取代先前的。

典型 agent 工作流：`orbit trace --json` → 挑出 errored trace →
`orbit trace <id> --json` 找失敗的 span → 該 trace 的 log 行走 daemon 的
`GET /api/traces/{id}/logs`，或人類可讀輸出用 `orbit trace <id> --logs`。

## Inspect

`orbit inspect --json` 是 agents 理解 Orbit 目前是否適合自動化操作的建議第一步。
它回傳一般 `orbit.cli.v1` envelope，且 `data.schema_version` 是
`orbit.inspect.v2`。

Inspect payload 包含：

| Field | 意義 |
|---|---|
| `readiness` | 穩定的決策狀態，包含 `state`、`blocked`、`summary`。 |
| `daemon` | daemon 是否執行、PID、版本、更新資訊，以及可用時的 dashboard URL。 |
| `environment` | 與 status、env list 共用 `state`、`selected_name`、`selected_path`、`environments` selection object，另包含可用時的 preview/daemon 資訊。 |
| `resources` | 依 state 分組的 resource 摘要。 |
| `risks` | 排序過的 machine-readable risks，例如 `setup_required`、`environment_selection_required`、`orbit_update_pending`、`config_invalid`、`environment_stopped`、`env_mismatch`、`status_unavailable`、`dependency_readiness_ambiguous`、`resource_degraded`、`resource_converging`、`resource_stopped`。Dependency-readiness risk 是 advisory config evidence，因此可以與 blocking lifecycle risk 同時出現。 |
| `recommended_actions` | agent 應考慮的安全下一步指令。 |

穩定的 `readiness.state` 值：

| State | Blocked | 意義 |
|---|---:|---|
| `setup_required` | true | 尚未選到可用 environment；唯一下一步是 `orbit init --yes --json`。 |
| `selection_required` | true | 先前 selection 已失效；actions 會提供精確的 `orbit switch <env> --json` 選項，沒有候選時則提供 `orbit env sync --json`。 |
| `config_invalid` | true | 選到的 config 無法載入。較舊的共用 schema 唯一 action 是 `orbit env sync --json`；較新的 schema 唯一 action 是 `orbit update --json`。語法錯誤與未知欄位需要編輯回報的檔案，因此 Orbit 不會回傳沒有推進效果的 `inspect` 自我重試。 |
| `update_required` | true | 新版 Orbit binary 已安裝，但 daemon 仍執行舊版本；唯一 action 是 `orbit daemon restart --json`。 |
| `stopped` | true | 已設定 environment 但尚未執行；configured resources 會列為 stopped，唯一 action 是 `orbit up --json`。 |
| `needs_daemon` | true | running daemon 使用的 env 與選取的 config 不同。 |
| `degraded` | false | 至少一個 resource 回報 `degraded`。 |
| `converging` | false | 至少一個 resource 是 `pending`、`starting`、`building`、`stopping` 或 `restarting`。 |
| `partial` | false | 至少一個 resource 停止，且沒有更高優先級的風險。 |
| `ready` | false | inspect 沒有偵測到風險。 |

對於有 buffered output 的 terminal degraded resource，`inspect`、`status`
與 `doctor` 只會引導至 `orbit logs <resource> --json`。`logs` 回傳 exit
output 後會重新檢查即時狀態。若支援的 project dependency check 失敗，
唯一 action 會先安裝已宣告 dependencies，再只 restart 該 resource；否則
唯一 action 才是 `orbit restart <resource> --json`。Agent 應依序執行，不要
直接跳過 log；若期間出現新的 port、dependency 或 runtime 原因，Orbit
會用更安全的 cause-specific action 取代 blind restart。

當 daemon resource status 暫時不可用時，也可能回傳 `converging`。這種情況會用
`status_unavailable` risk 說明原因。

Agents 應把 `blocked: true` 視為需要先恢復的狀態。Agents 可以直接執行
non-destructive 的 `recommended_actions`，但任何 `destructive: true` 的 action
仍必須先取得明確 human approval。

readiness 是 `needs_daemon` 時，agents 應先執行建議的
`orbit switch <env> --json` action，再依據 service state 行動，因為 running
environment 與選取的 CLI config 不同。

Readiness 是 `update_required` 時，agents 必須先 restart Orbit 才能送出
resource mutation。這些 mutation 會回傳 `orbit_update_pending`，並且只建議
`orbit daemon restart --json`。

## Recommended Agent Workflow

從 inspect 開始，做動作，再重新 inspect：

```bash
orbit inspect --json
# 執行 response 的第一個 non-destructive recommended action
orbit inspect --json
```

對於失敗中的 service：

```bash
orbit status --json
orbit logs <resource> --json
orbit restart <resource> --json  # 修正回報的原因後
```

如果 JSON response 帶有 `recommended_actions`，請先照著走，再退回到臨時 debug。

## Exit Codes

| Code | 意義 |
|---|---|
| `0` | 成功。 |
| `1` | 錯誤。在 `--json` 模式下，已轉換的指令仍會輸出結構化 JSON。 |
