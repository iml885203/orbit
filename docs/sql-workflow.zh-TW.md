# SQL schema workflow

[English](./sql-workflow.md) · [繁體中文](./sql-workflow.zh-TW.md)

Orbit 對使用者只提供四個資料庫指令：`list`、`diff`、`publish`、`reset`。
Publish 全程在 host 上進行——用 `dotnet build` 建置 SQL project，再用
`sqlpackage` 把 dacpac 推到執行中的 sql-server。快速 reset 的底層機制由
Orbit 自己管理。

## Volume 與持久化模型

sql-server container 跑起來時，會把一個 named Docker volume 掛在
`/var/opt/mssql`，SQL Server 在那裡寫入它的 `.mdf` / `.ldf` 檔案。因為資料
存在 volume 裡，你累積的 schema 與資料會在以下情境之間都被保留：

- `orbit restart sql-server`
- Docker daemon 重啟
- 主機重新開機
- `orbit down` 之後再 `orbit up`

只有當 volume 本身被移除時，資料才會被丟掉：

```bash
orbit down
docker volume rm orbit_sql_server
```

移除 volume 會永久刪除其中所有本機資料庫。只有一顆 database 需要乾淨資料
時，請使用 `orbit db reset <dbname>`。

全新的 volume 一開始是空的；`orbit db publish --all` 會建立並 publish
所有設定好的資料庫。

## 什麼情境用什麼指令

| 情境 | 指令 | 成本 |
|---|---|---|
| 想知道 SQL source 有沒有變更 | `orbit db diff <dbname>` | 通常不到一秒 |
| 你剛改了一支 stored proc / 一張 table | `orbit db publish <dbname>` | 約 15 秒，冪等，沒有 downtime |
| 你從 `main` merge 了 schema，想在本地套用 | `orbit db publish <dbname>`（或 `--all`） | 每個 DB 約 15 秒，資料保留 |
| DB 塞滿不要的測試資料 | `orbit db reset <dbname>` | 幾秒，丟棄本機資料 |
| 初始化全新的 SQL Server | `orbit db publish --all` | 建立並 publish 所有 DB |

## `orbit db publish`:快速通用路徑

`orbit db publish <db>` 在 **host 上**建置 SQL project(`dotnet build`),
再用 host 的 `sqlpackage` 把 dacpac 直接發佈到 sql-server 的 published
port——不重建 image、不用 container 內工具,Apple Silicon 上是原生 arm64。
冪等:project 沒變時幾秒內收斂為 no-op,資料一律保留(破壞性變更預設擋下,
`--force` 才放行)。

DB schema 會收斂到 project：新增、修改或刪除 stored procedure、table
或其他 project object，都會產生對應的 create、alter 或 drop。可能造成
資料遺失的 drop 會顯示在 `db diff`，publish 預設擋下，直到使用者明確
加上 `--force`。

前置需求(`orbit doctor` 會檢查):host 上的 .NET SDK 與 sqlpackage——
`dotnet tool install -g microsoft.sqlpackage`。

「發佈到哪」與「發佈哪些專案」是兩件分開的事:

- **哪個容器**接收發佈 —— env 的選配 `sql_projects` 區塊,它唯一的欄位是
  `target`(容器名稱)。省略時,feature set 會自動偵測 `sql-server` 容器。

  ```yaml
  sql_projects:
    target: sql-server
  ```

- **哪些專案**要發佈 —— team-shared allowlist,放在 `envs/data/db-projects.yaml`
  (與 `claim.yaml` 同層,隨 env repo 一起出貨,一份清單涵蓋所有 env)。它列出
  SQL 專案的目錄名,以大小寫不敏感方式對應你 workspace 底下的資料夾:

  ```yaml
  # envs/data/db-projects.yaml
  projects:
    - billing.payment
    - billing.wallet
  ```

  在這裡宣告一個目錄,就是讓該專案加入 DB workflow 的方式 —— 沒有掃描猜測的
  fallback。沒有 allowlist 就沒有任何資料庫。

### 一次整個環境：`--all`

`orbit db publish --all` 依 project merge 順序逐顆 publish 所有資料庫,
遇到第一個失敗即停止。加上 `--parallel[=N]` 可同時 publish 最多 N 顆
(只有在已建置過的 server 上才安全)。Dashboard 的 `Publish all` 按鈕透過
daemon 做同一件事。對空的 SQL Server 執行時，同一個指令會建立缺少的
資料庫並部署 referenced shared objects。修好失敗的 project 後直接重跑；
已成功的資料庫會收斂為 no-op。

官方 image 讀的是 `MSSQL_SA_PASSWORD`(`SA_PASSWORD` 別名已棄用)——
container 兩個都宣告,內建 envs 已如此。

### 乾淨重置：`orbit db reset`

`orbit db reset <db>` 會中斷現有連線、丟棄本機資料並套用最新 schema。
不需要先執行任何設定指令。Orbit 有可用的快速還原狀態時會直接使用，否則
從 SQL project 重建；這個差異不需要使用者處理。

## Dashboard visibility

Local DB 頁面會在進入或回到視窗時檢查 source 變更。每顆資料庫都會顯示
是否同步，並提供 Check、Publish 與 Reset。

### 從 dashboard 執行 publish

Local DB 頁面為每個 db 提供 Publish 與 Reset，並提供 Publish all。
Publish 的串流輸出顯示在 log panel；Reset 丟棄資料前一定會要求確認。

整個 daemon 一次只能跑一個 db operation —— 當另一個 op 進行中時按鈕
都會停用。Publish 偵測到可能資料遺失時會先阻擋；使用者檢視警告並明確
確認後，才能執行 force publish。

## 延伸閱讀

- [configuration.zh-TW.md](configuration.zh-TW.md) —— `sql_server_image`、`sql_server_pull_policy` 與相關設定
- [docs/troubleshooting.md](troubleshooting.zh-TW.md) —— 更完整的錯誤列表
