# 本地 Tracing

[English](./tracing.md) · [繁體中文](./tracing.zh-TW.md)

在 dashboard 或 CLI 查看 OpenTelemetry trace，追蹤一個 request 經過哪些
本機服務。用 `TraceId` 找到該 request 對應的 log。

## 預設開啟

Tracing **預設就是開的** —— Orbit 零設定就會收集本地 trace。daemon 會在
`127.0.0.1:4318` 跑一個 OTLP/HTTP receiver，並對每個 dev service 注入標準的
`OTEL_*` 環境變數。已設定 OTLP exporter 讀取這些變數的 service 會自動接上
receiver。

**Hybrid injection。** 已經自己設了 `OTEL_EXPORTER_OTLP_ENDPOINT` 的 service
（刻意接到外部 collector）會被保留不動 —— Orbit 讓路、不去改它的 telemetry
去向，因此那個 service 的 span 不會出現在 Orbit 裡。其餘每個 service 都會被
指向 Orbit 的 receiver。

**逐 env 關掉**（很少需要）：在 env YAML 加上明確的 opt-out，然後重新執行
一般啟動指令：

```yaml
tracing:
  enabled: false
```

```bash
orbit up
```

Orbit 會在中斷既有環境前先驗證修改，接著套用並恢復原先正在運行的 resources。

參數（`otlp_port`、`max_traces`）見
[configuration.zh-TW.md](configuration.zh-TW.md#tracing)。

Trace 保存在記憶體中（預設 1000 條），`orbit down` 即清空。超過條數或約
64 MiB 保留容量時，會淘汰最舊的整條 trace。這是保留資料的估計值，不是 process
RSS 上限。每條 trace 最多 2000 個 span；重送相同 span 會取代舊值，不另占一筆。

| 輸入 | 上限與結果 |
|---|---|
| 一次 traces／logs export | 壓縮前後各 8 MiB；超量在寫入前回 HTTP 413 |
| Trace 名稱、狀態訊息、kind／status | 16 KiB 內容；依 UTF-8 邊界截斷並加 `…[truncated]` |
| Trace／span／parent ID 與 service 名稱 | 各 1 KiB；超長身分會拒絕該 span，避免截斷後混淆不同身分 |
| Span attributes | 總計 16 KiB、最多 128 個；超量時省略整組 |

`orbit tracing status --json` 會回報保留 bytes、容量、整條 trace 淘汰數與被拒絕
的 span 數。Body 拒絕以 HTTP 413 表示，不推算尚未解碼的 span 數。每分鐘速率
使用固定的一秒計數桶，閒置時也會過期。這些固定上限不需要修改 YAML。

如果設定的 port 已被佔用，Orbit 會自動往下找下一個空的 port（除非你有釘死
`otlp_port`，這時衝突會被回報而不是靜默換 port）。`orbit tracing status` 會顯示
實際使用中的 port。

## CLI

```bash
orbit trace                  # 近期 trace，最新在前
orbit trace -f               # 即時串流新進的 trace
orbit trace --search item/1  # span 名稱或 attribute 值相符的 trace
orbit trace --service api    # 經過該 service 的 trace
orbit trace --json           # 結構化清單（給 agent）；搭配 -f 時輸出 NDJSON
orbit trace <id>             # 一條 trace 的 ASCII waterfall
orbit trace <id> --logs      # waterfall + 帶有這個 trace id 的 log 行與 log record
orbit trace <id> --json      # 完整 span tree 與每個 span 的資料
orbit trace logs             # OTLP log record（見下方 Log records）
orbit tracing status         # receiver 健康嗎？用哪個 port？計數器
```

沒有 trace 可顯示時，`orbit trace` 會依 receiver 的真實狀態說明原因 —— 關閉、
開著但 receiver 沒 bind 成功、或開著但目前沒流量 —— 讓你分得出這三種。
`orbit tracing status` 也會隨時回報同樣的健康狀態（並有 `--json` 供 script／agent）。

span 標籤不會被截斷：清單把 root span 印在最後一欄，waterfall 把每個標籤印在
bar 之後。

### Health probe 流量

Orbit 每隔幾秒探測每個 HTTP health check，有 instrument 的 service 會把每次探測
變成一條 trace。probe 送出的 User-Agent 是 `orbit-health-probe`；root span 記錄了
這個 user agent，或（instrumentation 沒記錄 user agent 時）root span 的 route／
path 等於該資源的 `health_check.path`，就算 health 流量。`orbit trace` 與
`GET /api/traces` 預設隱藏這類 trace（出錯的除外），人類可讀清單會說隱藏了幾條；
`--include-health`（`?include_health=1`）會顯示它們。dashboard 的清單目前還不會
隱藏。

刻意沒有 `enable`／`disable` 指令：tracing 預設開啟，要單獨關掉某個 env 就是
一行 `enabled: false` 的編輯 —— 跟其他每個 env 設定同一套心智模型。

## Dashboard

打開 **Tracing** 分頁（`http://localhost:19800/#/tracing`）：

- **清單** —— 最新在前，附即時的 spans/min 指示，以及 errored / 最小耗時 /
  搜尋等過濾條件（同步到 URL）。
- **詳情**（`#/tracing/:traceId`）—— span waterfall（bar 依 service kind 上色；
  錯誤有標記）加上 span inspector，顯示 attributes 與該 span 的 log 行
  （以 span/trace id 精確比對）。
  - **Open all logs** 開啟過濾成整條 trace 的 log 檢視器。
  - 選取 span 時，下方同步的 trace-log 面板會捲動並閃爍對應的 log 行。
  - **⧉ Copy** 可複製 trace（可貼上的 span tree）、其 logs、或兩者一起 ——
    適合回報 bug 或把 context 交給 agent。log 行也有逐行 📋 / 整條 trace 🧵
    的複製動作。
  - **Play on graph** 在 Services graph 上重播這個 request：每個 hop 依序
    亮起，失敗的服務以紅色脈動標示。可用播放列逐步前進。

Trace 詳情會合併密集的即時更新，依序刷新。切換 trace 或關閉視窗時會取消
尚未完成的請求；較晚回來的舊結果不會覆蓋另一條 trace 或其 logs。

在 Services graph 上，**Live** 開關會讓最近載有 trace 的 edge 跑流動光點 ——
一種「系統正在動」的氛圍視圖，與單一 trace 的重播是分開的。

## Log records

OpenTelemetry logging pipeline 有設定 OTLP exporter 的 service，會把 log record
送到同一個 receiver（`/v1/logs`）。它讀的是 Orbit 注入的 endpoint，不需要額外
設定。Orbit 保留每筆 record 的 severity、body、trace 與 span id、scope 和
attributes，包含 `exception.type`、`exception.message`、`exception.stacktrace`。
Record 跟 span 一樣依 resource 的 `service.name` 歸屬。送到 `/v1/metrics` 的
metrics 會被接受後丟棄。

`orbit trace logs` 列出 record，`orbit trace <id> --logs` 顯示單一 trace 的
record；`orbit logs <resource>` 不變，仍然顯示 resource 的 stdout。Flag、JSON
頁面與 `--since` 分頁見 [agent-cli.md](agent-cli.md#log-records)。

所有 service 共用一個 in-memory ring：最多 10,000 筆、約 32 MiB，超量先淘汰
最舊 record。`orbit down` 即清空。Body、severity、scope 和 attribute 值各保留
16 KiB 內容，超長加上 `…[truncated]`。身分欄位沿用 span 的 1 KiB 拒絕上限。
超過 16 KiB 的 attribute key 會省略；export 轉換最多保留 128 個 attributes
（直接 ingest 超過此數量時省略整組）。

## Stdout 歷史

Stdout 沿用既有 log／history 與 follow 格式。超長行切成 64 KiB 內容的片段，
加上 ` [line continues]`；直接寫入歷史的巨大單行則截斷並加
` [line truncated]`。標記另計 bytes。即使輸出一直沒有換行，也會持續讀取。

每個 service 最多保留 1000 行、1 MiB，所有 service 合計 16 MiB。最多保留
128 個 service 名稱，每個最多 256 bytes；更長的名稱會被拒絕。單一 service
先淘汰最舊行；總容量不足時，淘汰最久沒有寫入的 service 整份歷史。舊歷史被
淘汰後，follow 仍接收新輸出。內部保留計數器分別記錄淘汰、截斷與被拒絕的
service 行數。需要完整歷史時，請另行保存長期日誌。

## Logs ↔ traces

因為 orbit 同時擁有 trace store 和 log stream，而 log 本來就帶
`TraceId`/`SpanId`，所以 join 是**精確比對** —— 絕不是模糊的時間戳猜測：

- 帶有 trace id 的 log 行有 🔍 動作，可直接打開它的 trace waterfall。
- 選取的 span 在 inspector 裡只顯示該 span 的 log 行（以 `SpanId` 比對）；
  當 logger 沒輸出 span id 時退回 trace 層級的行。

## 運作方式

```
dev service ──OTLP/HTTP──▶ daemon receiver (:4318) ──▶ trace + log-record rings
                                                          │
   CLI (orbit trace)  ◀── unix socket ◀──────────────────┤
   dashboard          ◀── HTTP + SSE 'trace' event ◀──────┘
```

receiver 只走 HTTP；orbit 注入 `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`，
讓服務直接對準它，orbit 也就不需要 gRPC 相依。span 以 trace id 為 key 累積
（可跨多次 export），summary（root、耗時、services、狀態）在讀取時推導。
