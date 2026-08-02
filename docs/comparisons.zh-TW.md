# 比較

[English](./comparisons.md) · [繁體中文](./comparisons.zh-TW.md)

比的是設計決策，不是功能清單。其他工具的敘述以 2026 年 8 月的公開文件為準。

## Orbit 與 Docker Compose

|  | Compose | Orbit |
|---|---|---|
| 執行對象 | containers | host processes + containers，同一張 graph |
| 「Ready」的定義 | container 已啟動 | endpoint 有回應 |
| 指令之間 | — | 常駐 daemon 保留 state、logs、health |
| 機器介面 | — | 有版本的 `orbit.cli.v1` JSON contract |

**選 Compose**：整個 stack 都已容器化，且一份 compose 檔同時服務開發與部署。

## Orbit 與 Aspire

| 決策 | Aspire | Orbit |
|---|---|---|
| App model | code-first AppHost（C# / TypeScript） | 一份宣告式 `orbit.yaml` |
| 環境生命週期 | 隨 `aspire run` 存活 | 常駐 daemon，比 session 活得久 |
| 情境切換（共用 infra） | 重啟 AppHost 與其下所有 runtime/containers | 共用的 containers 持續運行 |
| 範圍 | 開發 → 部署（`publish` / `deploy`） | 到部署為止 |
| 分發單位 | solution 內的 AppHost | 任何 Git repo 裡的 YAML，可跨 repo |

**選 Aspire**：環境即程式碼、typed integrations、OpenTelemetry dashboard、
同一個 model 從開發帶到部署——尤其在 Azure 上。

**選 Orbit**：環境是一份可 review 的檔案、要比 terminal 活得久、跨多個
repo，以及給 coding agents 與測試 harness 的穩定 JSON contract。

### SQL Server schema 管理

|  | Aspire（Community Toolkit） | Orbit（`sqlserver` extension） |
|---|---|---|
| 套用 schema | 啟動時自動 publish dacpac；dashboard「Redeploy」 | `orbit sqlserver publish`，保留資料 |
| 預覽變更 | — | `orbit sqlserver diff` |
| 乾淨重置 | — | `orbit sqlserver reset`，要求確認 |
| 查詢 | — | `orbit sqlserver query` |
