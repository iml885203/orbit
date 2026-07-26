# 本地 Tracing

[English](./tracing.md) · [繁體中文](./tracing.zh-TW.md)

Orbit 可以擷取已具備 OpenTelemetry instrumentation 的本機服務 traces，
並直接放在你已經使用的 graph 與 logs 旁邊，不需要額外的 trace UI。

一條 trace 就是一條穿過 dependency graph 的路徑；而 orbit 的 log 本來就帶著
`TraceId`。Tracing 是把兩者串起來的那層組織。

## 預設開啟

Tracing **預設就是開的** —— Orbit 零設定就會收集本地 trace，跟 .NET Aspire
dashboard 一樣。daemon 會在 `127.0.0.1:4318` 跑一個 OTLP/HTTP receiver，並對
每個 dev service 注入標準的 `OTEL_*` 環境變數。已設定 OTLP exporter 讀取這些
變數的 service 會自動接上 receiver。

**Hybrid injection。** 已經自己設了 `OTEL_EXPORTER_OTLP_ENDPOINT` 的 service
（刻意接到外部 collector）會被保留不動 —— Orbit 讓路、不去改它的 telemetry
去向，因此那個 service 的 span 不會出現在 Orbit 裡。其餘每個 service 都會被
指向 Orbit 的 receiver。

**逐 env 關掉**（很少需要）：在 env YAML 加上明確的 opt-out，然後重啟 daemon：

```yaml
tracing:
  enabled: false
```

```bash
orbit down && orbit up   # 重啟 daemon 生效
```

參數（`otlp_port`、`max_traces`）見
[configuration.zh-TW.md](configuration.zh-TW.md#tracing)。

Trace 存在 in-memory ring buffer（預設 1000 條），`orbit down` 即清空 ——
這是本地開發輔助，不是長期保存。Ingest 在三個維度上有上限，讓忙碌的 service
不會無限撐大記憶體：trace 數（`max_traces`）、每條 trace 的 span 數、每個 span
的 attribute bytes。被這些上限丟棄的 span 會被計數，並由 `orbit tracing status`
回報。

如果設定的 port 已被佔用，Orbit 會自動往下找下一個空的 port（除非你有釘死
`otlp_port`，這時衝突會被回報而不是靜默換 port）。`orbit tracing status` 會顯示
實際使用中的 port。

## CLI

```bash
orbit trace                  # 近期 trace，最新在前
orbit trace -f               # 即時串流新進的 trace
orbit trace --json           # 結構化清單（給 agent）；搭配 -f 時輸出 NDJSON
orbit trace <id>             # 一條 trace 的 ASCII waterfall
orbit trace <id> --logs      # waterfall + 帶有這個 trace id 的 log 行
orbit trace <id> --json      # 完整 span tree 與每個 span 的資料
orbit tracing status         # receiver 健康嗎？用哪個 port？計數器
```

沒有 trace 可顯示時，`orbit trace` 會依 receiver 的真實狀態說明原因 —— 關閉、
開著但 receiver 沒 bind 成功、或開著但目前沒流量 —— 讓你分得出這三種。
`orbit tracing status` 也會隨時回報同樣的健康狀態（並有 `--json` 供 script／agent）。

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

在 Services graph 上，**Live** 開關會讓最近載有 trace 的 edge 跑流動光點 ——
一種「系統正在動」的氛圍視圖，與單一 trace 的重播是分開的。

## Logs ↔ traces

因為 orbit 同時擁有 trace store 和 log stream，而 log 本來就帶
`TraceId`/`SpanId`，所以 join 是**精確比對** —— 絕不是模糊的時間戳猜測：

- 帶有 trace id 的 log 行有 🔍 動作，可直接打開它的 trace waterfall。
- 選取的 span 在 inspector 裡只顯示該 span 的 log 行（以 `SpanId` 比對）；
  當 logger 沒輸出 span id 時退回 trace 層級的行。

## 運作方式

```
dev service ──OTLP/HTTP──▶ daemon receiver (:4318) ──▶ ring buffer
                                                          │
   CLI (orbit trace)  ◀── unix socket ◀──────────────────┤
   dashboard          ◀── HTTP + SSE 'trace' event ◀──────┘
```

receiver 只走 HTTP；orbit 注入 `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`，
讓服務直接對準它，orbit 也就不需要 gRPC 相依。span 以 trace id 為 key 累積
（可跨多次 export），summary（root、耗時、services、狀態）在讀取時推導。
