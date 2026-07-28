# 為什麼是 Orbit？——賣點背後的對照數據

[English](./why-orbit.md) · [繁體中文](./why-orbit.zh-TW.md)

README「Why Orbit?」的每一條主張，都能回溯到 2026 年 7 月的一次實測對照：把我們真實的
dev 環境（5 個 containers、一個 .NET 後端、一個 pnpm 前端）移植到 .NET Aspire 13.4，
在同一台機器上與 Orbit 並排跑完整開發迴圈——改 DB schema、改後端 API、改前端 codegen。
本文記錄數據，包括 Aspire 贏的地方。

## 結構性差異

Aspire 的模型是「**環境即程式碼**」：AppHost 是編譯出來的 C# 程式，這個程式本身就是
orchestrator。Orbit 的模型是「**環境即設定＋常駐控制面**」：env 是 YAML，由長駐 daemon
對它執行動詞。

環境即程式碼在 outer loop 是真優勢——型別安全、可 review、（配合 `aspire do`）同一份
model 可部署。在 inner loop 它變成稅：動到環境就得重建並重啟 orchestrator 本身。
界線實測如下：

| inner-loop 動作 | Aspire 13.4 | Orbit |
|---|---|---|
| 改自己服務的程式碼 | ✅ 重啟單一資源（~36s，dotnet 重建為主） | ✅ 等價 |
| 改環境變數／連線字串 | ❌ 改 C# → 重啟 AppHost 世界 | 改 YAML → 只重啟該服務 |
| 加減服務 | ❌ AppHost 重啟；session containers 陪葬 | 改 YAML → daemon 重啟會 **reconnect** 運行中的服務 |
| 臨時只啟動部分資源 | ❌ 須事先寫好 `WithExplicitStart` | runtime 使用 `orbit up <resource>` 或 `orbit up --group <group>` |
| 切換環境 | ❌ 無 env 原語——停世界＋AppHost 重建（15–25s）＋冷起 | `orbit switch`：秒級；共用 infra 不中斷 |
| 套用 DB schema 變更 | ❌ 不在守備範圍：手動 `dotnet build` + `sqlpackage`（8 個參數） | `orbit db publish`——一個動詞 |

Aspire 自己的 issue tracker 佐證了這個診斷：官方提案把 DCP 與 dashboard 從 AppHost
拆離（讓重啟不再拖垮一切），正是往「常駐控制面」的形狀移動；「running subsets of
services」也被官方 roadmap 列為已知缺口。

## 實測結果（2026 年 7 月，Apple Silicon，image 已快取）

- **Aspire 跑得動真實 stack。** 257 行 env YAML 移植成約 120 行 AppHost C#，一次跑通：
  `aspire start` 到後端 health＋前端可服務共 76 秒。「跑不跑得動」**不是**差異點。
- **DB 迴圈是最大的鴻溝。** 對真實 SSDT 專案加一張 table：Aspire 世界用了 3 個工具、
  踩了 2 個坑（MSB3030 referenced-project 錯誤——解法 `dotnet build -o` 屬於隱性知識；
  再加 8 個參數的 `sqlpackage`），機器時間約 24 秒，且 Aspire 本體全程缺席。Orbit：
  一個冪等指令，30 天真實使用 median 8.6 秒。誠實註腳：no-op 重部署 Aspire（1.1s）
  比 Orbit（6.4s，每次做完整 sqlpackage 比對）快。
- **`orbit db reset` 在對照範圍內沒有等價物。** median 0.9 秒（database snapshot
  revert），對比刪 volume＋重新 bootstrap＋重新 seed 的數分鐘。
- **後端／前端迭代平手。** 資源重啟（~36s）與 codegen（~4s）兩邊相同。不是賣點。
- **進程清理：優雅停止平手，crash 場景 Aspire 贏。** 正常停止兩邊都零孤兒。`kill -9`
  orchestrator 本體時，Aspire 的 DCP 仍會收乾淨；Orbit 的 process-group kill 需要
  daemon 活著，孤兒會留到下次 daemon 啟動 reconnect 接管為止。我們把對自己不利的
  結果也寫出來，因為這份文件的其餘部分值得你的信任。
- **Agent 介面。** Aspire 的 MCP server（13.4）提供 14 個工具：觀測很強（resources、
  logs、traces、docs），控制動詞只有一個（`execute_resource_command`），沒有 stack
  起停、env、seed、DB 動詞，且需要 AppHost 在跑＋專案脈絡。Orbit 的合約是全域 binary
  加結構化 JSON 錯誤（自帶建議的下一個指令）。在 Aspire 上 agent 能「看」、能「按
  資源上的按鈕」；在 Orbit 上 agent 能「管理環境」。

## 時間差值多少

以下頻率不是估計——來自一位開發者真實 30 天的指令歷史（1,537 條）。機器時間差為實測；
多工具流程的人工開銷（每次約 30 秒）為估計值，已明確標注。

| 操作 | 30 天次數 | 每次差 | 每月代價 |
|---|---|---|---|
| schema 迭代（publish + diff） | 176 | ~45s | ~2.2 小時 |
| 乾淨資料還原 | 32 | ~6 分鐘（Aspire 無等價物） | ~3.2 小時 |
| 切換 env | 22 | ~90s | ~0.6 小時 |
| 部分啟動 | 77 | ~47s | ~1.0 小時 |

**≈ 每位開發者每月 7 小時**，近半來自 `db reset` 一項。誠實邊界：n=1，且該開發者以
coding agent 驅動 Orbit，操作頻率偏高——但這正是重點：agent 驅動的開發會倍增環境被
操作的頻率，也就倍增每次操作的差距。人工開銷估計砍半，數字仍約 5 小時。

同一份歷史裡，DB 操作是最高頻的指令家族（1,537 條中佔 250 條），94% 的指令走 CLI 而
非 dashboard——上述工作流主張描述的是工具實際被使用的方式，不是我們希望它被使用的方式。

## Aspire 贏的地方

若下列項目對你的團隊比 inner loop 更重要，用 Aspire：

- **Telemetry 廣度**：structured logs、metrics、trace 工具集中在一個 dashboard。
  Orbit 只有 in-memory traces。
- **型別化 .NET 整合**：AppHost 層的 service discovery 與 client integrations 深入
  應用程式碼；Orbit 止步於 YAML 與環境變數。
- **部署**：`aspire do` 把同一份 model 帶到 Azure/AKS/Helm。Orbit 刻意只做本地。
- **生態系**：Microsoft 支持、龐大的整合目錄、公開社群。

## 方法備註

單機；除註明 median 者外，wall-clock 為單次量測；median 來自 30 天使用記錄。Aspire
移植重用了 Orbit 的資料 volumes（因此數字不含首次資料庫 bootstrap），並略過 sidecar
UI containers。Aspire 的 SQL Database Projects community toolkit 有評估，但它綁定
Aspire 自管的資料庫資源，不涵蓋發佈到既有 server 的場景。版本：Aspire CLI 13.4.6、
`CommunityToolkit.Aspire.Hosting.SqlDatabaseProjects` 13.4.1-beta、Orbit commit
`d6d0fcc`。
