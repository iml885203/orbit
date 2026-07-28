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
    "code": "unknown_service",
    "message": "unknown service: redisx",
    "hint": "Run orbit status --json to list known services.",
    "retryable": false,
    "next_command": "orbit status --json"
  },
  "recommended_actions": [
    {
      "command": "orbit status --json",
      "reason": "List known services and current states."
    }
  ]
}
```

Agent 應優先依據 `error.next_command` 與 `recommended_actions` 行動,而不是從錯誤訊息文字去猜測恢復路徑。

## Converted Commands

下列指令在加上 `--json` 時，目前都使用 `orbit.cli.v1` envelope：

| Command | JSON 行為 |
|---|---|
| `orbit version --json` | 回傳目前安裝的 Orbit 版本。 |
| `orbit doctor --json` | 在 `data` 中回傳診斷檢查結果。 |
| `orbit inspect --json` | 回傳 agent-ready 狀態快照，包含 readiness、daemon/env 摘要、service risks，以及建議後續指令。 |
| `orbit status --json` | 在 `data` 中回傳 setup readiness、daemon 與已設定 service 的狀態。 |
| `orbit logs <service> --json` | 以單一 JSON 物件回傳最近的 log 行。 |
| `orbit logs <service> -f --json` | 以 NDJSON 串流事件，每行一個 JSON 物件。 |
| `orbit up --json` | 回傳請求的 services、觀察到的最終 state、降級或逾時的 services，以及建議的後續指令。 |
| `orbit down --json` | 在停止 services 後回傳最終的 lifecycle 結果。 |
| `orbit down <service> --json` | 回傳指定 service 的最終 lifecycle 結果。 |
| `orbit restart --json` | 回傳最終 lifecycle 結果，並驗證 restart 的證據。 |
| `orbit env use <env> --json` | 回傳選取的 env、env 名稱、daemon 是否正在執行，以及是否需要 restart。 |
| `orbit env sync --json` | 回傳 sync source、destination、dry-run 狀態、寫入檔案、daemon 狀態，以及 restart 建議。 |
| `orbit switch <env> --json` | 回傳選取的 env、daemon start/restart action、最終 daemon 狀態、config path、dashboard URL，以及新 env 的 prerequisite checks/readiness。 |
| `orbit daemon start --json` | 回傳 daemon running 狀態、PID、config path 與 dashboard URL。 |
| `orbit daemon stop --json` | 回傳停止後狀態、先前 PID，以及是否要求 service shutdown。 |
| `orbit daemon restart --json` | 回傳先前/新的 daemon 狀態、PID、config path、dashboard URL 與 service shutdown 影響。 |
| `orbit uninstall --json` | 預覽 binary artifacts 與 user data 是否保留；只有加上 `--yes` 才會移除。 |
| `orbit trace --json` | 回傳近期 trace summaries 於 `data.traces`，最新在前。 |
| `orbit trace -f --json` | 串流 NDJSON trace-summary event，一行一個 JSON 物件。 |
| `orbit trace <id> --json` | 回傳一條完整 trace（summary 欄位 + `spans`）於 `data`。 |
| `orbit tracing status --json` | 回傳 receiver 健康狀態、使用中的 port 與 ingest 計數器於 `data`。 |

Lifecycle 指令在 JSON 模式下會抑制裝飾性的進度輸出，讓 stdout 保持可解析。

Lifecycle actions 會依結果提供。成功的 `up` 只回傳一個主要下一步：
`orbit open --json`。啟動失敗時則建議查看 status、根因 resource 的 logs，
並在修正原因後只 restart 該 resource；不會把 agent 導向無關的 setup 診斷。

第一次 setup 前，status 仍是成功的狀態查詢，但會回傳
`data.setup_required: true`、可讀的 `setup_message`，以及唯一的
`orbit init --yes --json` action。若 environment 檔案存在但內容無效，則回傳
`invalid_environment` error，因為 Orbit 無法安全使用它啟動。

已轉換控制指令的穩定 `data.operation` 值：

| Command | `data.operation` |
|---|---|
| `orbit env use <env> --json` | `env_use` |
| `orbit env sync --json` | `env_sync` |
| `orbit switch <env> --json` | `switch` |
| `orbit daemon start --json` | `daemon_start` |
| `orbit daemon stop --json` | `daemon_stop` |
| `orbit daemon restart --json` | `daemon_restart` |
| `orbit uninstall --json` | `uninstall` |

對 `switch` 而言，`daemon_running_before` 與 `daemon_running_after` 描述
daemon transition。新 env 缺少必要 runtime 或 package installation 時，
`prerequisites_ready` 會是 false；`prerequisites` 使用與 Doctor 相同的 checks，
而 Orbit 能判斷修復方式時，`recommended_actions` 會包含可直接執行的指令。

當先前已有 daemon 在跑時，daemon stop/restart payload 可能包含 `stop_method`。穩定值為 `graceful`、`terminated`、`killed`。

## Legacy JSON Commands

部分指令早於 envelope 存在，為了相容性會刻意保留既有的 JSON 形狀：

| Command | 行為 |
|---|---|
| `orbit daemon status --json` | 回傳 legacy daemon status 物件。 |
| `orbit history --json` | 保持既有的 history payload。 |
| `orbit history gaps --json` | 保持既有的 history gaps payload。 |

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
`orbit.inspect.v1`。

Inspect payload 包含：

| Field | 意義 |
|---|---|
| `readiness` | 穩定的決策狀態，包含 `state`、`blocked`、`summary`。 |
| `daemon` | daemon 是否執行、PID、版本、更新資訊，以及可用時的 dashboard URL。 |
| `env` | 目前 config path、env 名稱、preview-only flag，以及可用時 daemon 回報的 env。 |
| `services` | 依 state 分組的 service 摘要。 |
| `risks` | 排序過的 machine-readable risks，例如 `config_invalid`、`env_mismatch`、`daemon_unreachable`、`status_unavailable`、`service_degraded`、`service_converging`、`service_stopped`。 |
| `recommended_actions` | agent 應考慮的安全下一步指令。 |

穩定的 `readiness.state` 值：

| State | Blocked | 意義 |
|---|---:|---|
| `config_invalid` | true | 選到的 config 無法載入。 |
| `needs_daemon` | true | config 可載入，但 daemon 目前不可連線，或 daemon 正在以不同 env 執行。 |
| `degraded` | false | 至少一個 service 回報 `degraded`。 |
| `converging` | false | 至少一個 service 是 `pending`、`starting`、`building`、`stopping` 或 `restarting`。 |
| `partial` | false | 至少一個 service 停止，且沒有更高優先級的風險。 |
| `ready` | false | inspect 沒有偵測到風險。 |

當 daemon service status 暫時不可用時，也可能回傳 `converging`。這種情況會用
`status_unavailable` risk 說明原因。

Agents 應把 `blocked: true` 視為需要先恢復的狀態。Agents 可以直接執行
non-destructive 的 `recommended_actions`，但任何 `destructive: true` 的 action
仍必須先取得明確 human approval。

當 `needs_daemon` 是由 `env_mismatch` 造成時，agents 應先 restart 或 switch
daemon 再依據 service state 行動，因為 daemon 回報的可能是不同 env 的狀態。

## Recommended Agent Workflow

從 inspect 開始，做動作，再重新 inspect：

```bash
orbit inspect --json
orbit up --infra --json
orbit inspect --json
orbit logs redis --json
```

對於失敗中的 service：

```bash
orbit status --json
orbit logs <service> --json
orbit doctor --json
```

如果 JSON response 帶有 `recommended_actions`，請先照著走，再退回到臨時 debug。

## Exit Codes

| Code | 意義 |
|---|---|
| `0` | 成功。 |
| `1` | 錯誤。在 `--json` 模式下，已轉換的指令仍會輸出結構化 JSON。 |
