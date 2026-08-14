# 為什麼是 Orbit？

[English](./why-orbit.md) · [繁體中文](./why-orbit.zh-TW.md)

本機環境的 setup 知識不該只存在開發者腦中。在 microservice 專案裡，記住要啟動
哪些 processes、順序、ports、dependencies 與 health checks，是顯著的認知負擔，
也很難可靠地委派給 coding agent。

Orbit 把這些知識變成可執行的 environment contract，讓開發者與 coding agents
共用同一套控制方式，管理在 host 上執行的 application code，以及放在
containers 裡的 database、queue、cache 與支援工具。

## 使用者需要理解的模型

Orbit 刻意把一般開發迴圈維持在四個指令：

```text
orbit up       啟動環境
orbit status   查看實際 ready 的狀態
orbit logs <resource>
               檢查 application output
orbit down     停止環境
```

`orbit.yaml` 描述 environment intent：resources、dependencies、endpoints 與
health。背後的 mechanics 由 Orbit 負責：

- 依 dependency 順序啟動 host processes 與 containers；
- 等待真實 readiness，不把「process 存在」誤當成「可以使用」；
- 把實際選定的 endpoints 注入 dependents；
- 透過常駐於本機的 daemon 保留 logs 與 state；
- Orbit 或 config 改變後重新連接原本的環境；
- 讓 CLI、dashboard 與 structured JSON 看到同一份狀態。

Daemon 在一般流程中只是 implementation detail。需要時 `orbit up` 會自動啟動
它、套用有效的 config 編輯，並恢復原先正在運行的 resources。

## 先在單一專案證明價值，再決定是否分享

一個專案可以只在程式碼旁放一份 `orbit.yaml` 開始，不必先採用全域 workspace
layout 或 environment repository。確認環境值得讓團隊共用後，再把同一份設定
移到有版本控制的 Git repository，並以 `orbit switch` 選取。

這讓兩個經常被不必要綁在一起的決策分開：

1. 證明 Orbit 能改善一個專案的本機開發迴圈；
2. 把維護過的 environment 分發給整個團隊。

完整路徑請見[在你的專案使用 Orbit](local-first.zh-TW.md)。

## Orbit 整合了哪些零散工具

當一個專案累積多套互相重疊的工具時，Orbit 最有價值：

| 現有方式 | Orbit 整合的責任 |
|---|---|
| Shell scripts 或 task runners | dependency order、長期 state、health、logs 與 recovery |
| 只有 containers 的 orchestration | 把 host application services 與 containers 放進同一張 graph |
| 每個 service 一個 terminal | 統一起停，仍可針對單一 resource start、stop、restart、看 logs |
| 人工維護的 setup 說明 | 可執行的 readiness checks，以及一個明確下一步 |
| Agent 各自撰寫 automation | 人與 coding agents 共用同一份有版本的 JSON contract |

Orbit 不要求替換專案既有的 build system、package manager 或 container images。
Service command 仍是開發者原本就在使用的 command。

這些決策與具體工具的對照，見本頁最後的[比較](#比較)一節。

## Infrastructure 不預設 database 世界觀

Containers 是 generic resources。Health checks、dependency injection、seed
commands 與 lifecycle 都不假設特定 database engine。

Orbit 只為常見的 container-native clients 提供小型便利層：

- `orbit query redis`
- `orbit query mongo`
- `orbit query postgres`
- 其他工具使用 `orbit exec <container> <client...>`

Seed 同樣以 command 為基礎：environment 宣告 client command 與要傳入的 files，
Orbit 記錄哪些內容已執行，但不解讀資料格式。

SQL Server Database Projects 具有額外的 diff、publish、reset semantics，無法
誠實泛化到所有 database，因此放在明確選用的 extension。只有 environment 啟用
`sqlserver` 時，它的 commands、checks 與 dashboard page 才會出現；核心 lifecycle
完全不依賴它。

## Recovery 必須可信

如果指令結束後使用者仍不知道發生什麼事，再快也沒有價值。Orbit 把 recovery
視為 command contract 的一部分：

- 無效 config 會在中斷既有可用環境前被拒絕；
- port 衝突會指出占用的程式與 remedy，而不是默默移動資源；
- dependency 失敗會指出受阻 resources 與下一個有效動作；
- 切換專案時不會默默控制另一個專案的 resources；
- 破壞性 database 操作要求明確確認；
- JSON errors 帶有穩定 code 與可直接執行的 recommended actions。

目標不是隱藏失敗，而是讓下一個正確動作顯而易見。

## 在 E2E 測試套件底下

服務日常開發的同一個環境，也可以作為 E2E 測試套件的基底——在開發者自己的
機器或 CI runner 上。Orbit 的職責刻意收得很小：提供共用的基礎設施，並誠實
回答關於它的問題。測試 harness 擁有測試資料與受測系統；它透過
`orbit env info --json` 驗證基底而不是重新 provision，缺少必要 resource 時
快速失敗並給出可執行的訊息。

分層模式請見[在 E2E 測試底下使用 Orbit](e2e-testing.zh-TW.md)。

## Orbit 刻意不負責什麼

Orbit 不是：

- production deployment platform；
- package manager 或 runtime installer；
- multi-user remote control service；
- 泛用 database schema framework；
- application-level service discovery 或 telemetry SDK 的替代品。

它是 single-user、single-machine 的 control plane，服務開發與測試環境。
Dashboard 只綁定 loopback，project toolchain 仍由開發者控制；Orbit 專注縮短
inner loop，不改變 application 的部署方式。

這些邊界本身就是產品的一部分：承諾越少，使用者需要理解的模型越小，Orbit 在自己
負責的範圍內也越可預期。

## 比較

比的是設計決策，不是功能清單。其他工具的敘述以 2026 年 8 月的公開文件為準。

### Orbit 與 Docker Compose

|  | Compose | Orbit |
|---|---|---|
| 執行對象 | containers | host processes + containers，同一張 graph |
| 「Ready」的定義 | container 已啟動 | endpoint 有回應 |
| 指令之間 | — | 常駐 daemon 保留 state、logs、health |
| 機器介面 | — | 有版本的 `orbit.cli.v1` JSON contract |

**選 Compose**：整個 stack 都已容器化，且一份 compose 檔同時服務開發與部署。

### Orbit 與 Aspire

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
