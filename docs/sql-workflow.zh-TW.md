# SQL Server Database Projects

[English](./sql-workflow.md) · [繁體中文](./sql-workflow.zh-TW.md)

這組 command 命名為 `orbit sqlserver`，因為這個 optional workflow 實作的是
SQL Server Database Project semantics，不是 Orbit 的 generic database
abstraction。Redis、MongoDB 與 PostgreSQL 的 client convenience 仍放在
`orbit query`。

Environment 只有在明確啟用 `sqlserver` 時，才會出現五個 Database Project
指令：`list`、`diff`、`publish`、`reset`、`query`。其他 environment 不會顯示
這套 workflow。
Publish 全程在 host 上進行——用 `dotnet build` 建置 SQL project，再用
`sqlpackage` 把 dacpac 推到設定指定的 SQL Server target。快速 reset 的
底層機制由 Orbit 自己管理。

## Volume 與持久化模型

環境設定應把持久化儲存空間掛在 `/var/opt/mssql`；SQL Server 會在那裡寫入
`.mdf` / `.ldf` 檔案。有這個 mount 時，你累積的 schema 與資料會在以下
情境之間保留：

- `orbit restart <sqlserver.target>`
- Docker daemon 重啟
- 主機重新開機
- `orbit down` 之後再 `orbit up`

Orbit 不會自動移除該儲存空間。刪除 volume 或 bind mount 內的資料會永久
刪除所有本機資料庫；只有一顆 database 需要乾淨資料時，請使用
`orbit sqlserver reset <dbname>`。

全新的 volume 一開始是空的；`orbit sqlserver publish --all` 會建立並 publish
所有設定好的資料庫。

## 什麼情境用什麼指令

| 情境 | 指令 | 成本 |
|---|---|---|
| 想知道 SQL source 有沒有變更 | `orbit sqlserver diff <dbname>` | 通常不到一秒 |
| 你剛改了一支 stored proc / 一張 table | `orbit sqlserver publish <dbname>` | 約 15 秒，冪等，沒有 downtime |
| 你從 `main` merge 了 schema，想在本地套用 | `orbit sqlserver publish <dbname>`（或 `--all`） | 每個 DB 約 15 秒，資料保留 |
| DB 塞滿不要的測試資料 | `orbit sqlserver reset <dbname>` | 幾秒，丟棄本機資料 |
| 初始化全新的 SQL Server | `orbit sqlserver publish --all` | 建立並 publish 所有 DB |

## `orbit sqlserver publish`：日常快速路徑

`orbit sqlserver publish <db>` 在 **host 上**建置 SQL project(`dotnet build`),
再用 host 的 `sqlpackage` 把 dacpac 直接發佈到設定 target 的 published
port——不重建 image、不用 container 內工具,Apple Silicon 上是原生 arm64。
冪等:project 沒變時幾秒內收斂為 no-op,資料一律保留(破壞性變更預設擋下,
`--allow-data-loss` 才放行)。

Agent 與 script 可用 `orbit sqlserver publish <db> --json` 執行這條一般路徑。
成功時，`orbit.cli.v1` envelope 會列出所有已 publish 的 database。Force
publish 不會在 JSON mode 執行：error envelope 會保留原本 scope 與
`--allow-data-loss`，回傳一個 `destructive: true`、不含 `--yes` 的人工操作，確保執行
的人仍會看到確認提示。

DB schema 會收斂到 project：新增、修改或刪除 stored procedure、table
或其他 project object，都會產生對應的 create、alter 或 drop。可能造成
資料遺失的 drop 會顯示在 `orbit sqlserver diff`，publish 預設擋下，直到使用者明確
加上 `--allow-data-loss`。Force publish 會列出所有受影響的 database 並再次要求確認；
非互動執行時，只有在檢視影響後才使用 `--allow-data-loss --yes`。

前置需求(`orbit doctor` 會檢查):host 上的 .NET SDK 與 sqlpackage——
`dotnet tool install -g microsoft.sqlpackage`。

一個明確的 section 同時決定「發佈到哪」與「發佈哪些專案」：

```yaml
sqlserver:
  target: database
  username: sa
  password_env: MSSQL_SA_PASSWORD
  projects:
    - path: database/Accounts/Accounts.sqlproj
      databases: [AccountsDev, AccountsE2E]
    - path: database/Orders/Orders.sqlproj
```

`target` 指向接收 publish 的 container；project 是 workspace-relative
的 `.sqlproj` 檔案。預設 database name 來自檔名；`databases` 可明確把同一
project 部署到多個名稱；指定時至少要有一個名稱。每個 database name 只能對應一個 project，因此
basename 相同的不同 project files 會被拒絕。沒有 image sniffing、慣例
container 名稱、目錄掃描，
也沒有另一份 per-machine allowlist。

### 一次整個環境：`--all`

`orbit sqlserver publish --all` 依 project merge 順序逐顆 publish 所有資料庫,
遇到第一個失敗即停止。加上 `--parallel[=N]` 可同時 publish 最多 N 顆
(只有在已建置過的 server 上才安全)。Dashboard 的 `Publish all` 按鈕透過
daemon 做同一件事。對空的 SQL Server 執行時，同一個指令會建立缺少的
資料庫並部署 referenced shared objects。修好失敗的 project 後直接重跑；
已成功的資料庫會收斂為 no-op。

`password_env` 指定 target container 裡存放密碼的 key。Orbit 只在 DB
操作執行時讀取解析後的值，不會在 status、logs 或 JSON output 暴露密碼。

### 乾淨重置：`orbit sqlserver reset`

`orbit sqlserver reset <db>` 會中斷現有連線、丟棄本機資料並套用最新 schema。
不需要先執行任何設定指令。Orbit 有可用的快速還原狀態時會直接使用，否則
會先 drop 並重建整顆 database，再 publish SQL project。移除任何資料前，
確認提示會明確說明將執行哪一條路徑。

`orbit sqlserver query` 刻意只提供 CLI 操作。Dashboard 專注於 project drift、
publish 與 reset，而不內嵌通用 SQL console。

## Dashboard visibility

SQL Server 頁面會在進入或回到視窗時檢查 source 變更。每顆資料庫都會顯示
是否同步，並提供 Check、Publish 與 Reset。

### 從 dashboard 執行 publish

SQL Server 頁面為每個 db 提供 Publish 與 Reset，並提供 Publish all。
Publish 的串流輸出顯示在 log panel；Reset 丟棄資料前一定會要求確認。尚未有
reset point 時，頁面會先說明第一次 reset 將重建資料庫，並為之後保存 reset
point。

整個 daemon 一次只能跑一個 db operation —— 當另一個 op 進行中時按鈕
都會停用。Publish 偵測到可能資料遺失時會先阻擋；使用者檢視警告並明確
確認後，才能執行 force publish。

## 延伸閱讀

- [configuration.zh-TW.md](configuration.zh-TW.md#sqlserver) —— 完整的 target container 與 `sqlserver` 設定
- [troubleshooting.zh-TW.md](troubleshooting.zh-TW.md) —— 更完整的錯誤列表
