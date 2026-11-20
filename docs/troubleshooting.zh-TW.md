# 疑難排解

[English](./troubleshooting.md) · [繁體中文](./troubleshooting.zh-TW.md)

先執行 `orbit doctor` 檢查環境並查看修正建議。若問題仍在，依下方的
症狀或錯誤訊息找出下一步。

| 症狀 | 從這裡查 |
| --- | --- |
| 環境起不來、卡在 health check、port 被占用 | [啟動](#啟動) |
| 無法連線到 DB、Publish 失敗、資料表不見 | [SQL Server](#sql-server) |
| CLI 連不到 daemon、Dashboard 打不開、更新待套用 | [Daemon](#daemon) |
| Orbit 目錄無法寫入、找不到容器 | [檔案權限](#檔案系統與權限) · [CLI](#cli) |
| 需要看 log 或縮小問題範圍 | [診斷](#診斷) |

## 啟動

### `orbit.yaml` 出現未知 field 或 value

在專案內執行 `orbit doctor`。Orbit 會在不啟動 resource 的情況下驗證 core
與 extension sections，並指出來源行號。能辨識的 typo 也會直接給修正，例如
`did you mean "depends_on"?`；套用後再執行 `orbit up`。若沒有 suggestion，
請對照[設定參考](configuration.zh-TW.md)，不要猜測欄位。

### `orbit up` 卡在 "waiting for <service> to be healthy"
container 已經啟動，但 health check 一直沒成功。

- **看 logs**：`orbit logs <resource>`。大部分啟動失敗都會在這裡出現。
- **第一次初始化很慢**：database 還原許多 schema 之類的重型 container,在空 volume 上可能需要幾分鐘。先持續查看 log,再判斷是否真的卡住。
- **Apple Silicon 上的 Rosetta 模擬**：任何標 `platform: linux/amd64` 的 container 都會跑在 Rosetta 上 —— 啟動時間預期會多一倍。可以用 `docker inspect --format '{{.Platform}}'` 確認。
- **超過 health budget 仍是 `starting`**：`orbit status --json` 會顯示該 resource 的 `starting_phase` 與 `health_progress`；判讀方式見 [agent-cli.zh-TW.md](agent-cli.zh-TW.md)。重啟任何東西之前，先用 `kill -USR1 <daemon pid>`（macOS/Linux）取得 daemon 的 goroutine stack；它們會附加到 `~/.orbit/daemon.log`，daemon 會繼續執行。

### `port already in use`

用 Orbit 顯示的檢查指令找出占用 port 的程序。停止該程序，或改環境設定中的
host port，再跑 `orbit up`。

### 私有 registry `pull unauthorized` / `pull access denied`
你的 Docker client 沒辦法 pull image。中性版 `orbit doctor` 會確認 Docker 是否可用,但不會探測 private registry。請向你的 registry provider 驗證身份,例如：

```bash
docker login <registry-host>
```

如果你沒有 registry 權限，請聯絡 image owner 或管理員開通。

### `orbit up` 噴錯：「no env configs found in ~/.orbit/envs」
你還沒跑 `orbit init`（或者 sync 失敗了）。修法：

```bash
orbit source add team --url https://git.example.com/your-env-repo.git
orbit switch example
```

### 環境變更正在等待套用

先用 `orbit env apply --dry-run` 預覽，再執行 `orbit env apply`。
流程見[預覽待套用更新](./environment-sources.zh-TW.md#先預覽待套用的更新)。
Orbit 會先驗證更新後的環境；未變更的資源保持原行程或容器，變更的資源與依賴它的資源會重啟。
不相關的已停止資源保持停止；新需要的依賴可能啟動。Daemon 層級變更會交接並恢復原本運行中的資源。

若想先下載團隊更新、暫時不中斷目前環境，可使用
`orbit source sync <name> --no-apply`，準備好後再套用。

## SQL Server

### Target 正在執行，但 `publish`、`diff` 或 `reset` 回報 `sql_server_unavailable`

SQL Server listener 可能已接受 TCP connection，但 instance 還不能接受登入。
執行 `orbit doctor`；如果它警告 SQL Server readiness，請把 target 缺少或僅有
TCP 的 health check 換成它印出的認證 `exec` probe。接著執行
`orbit restart <sqlserver.target>`，讓 `orbit up` 等到登入成功後才放行 workflow。

如果 target 已使用該 probe，請用 `orbit sqlserver query "SELECT 1"` 驗證認證，
並用 `orbit logs <sqlserver.target>` 查看啟動失敗原因。

### `orbit sqlserver publish` 之後，DbGate 沒看到新的物件
DbGate 是按連線快取 schema 的。在連線上右鍵 → Disconnect → Connect，或在 database 節點按重新整理。再用下面的指令確認物件真的在：

```bash
orbit sqlserver query "SELECT name FROM YourDB.sys.procedures ORDER BY create_date DESC"
```

### `publish` 失敗：`CommonFiles.dacpac could not be resolved`
`dotnet build` 沒有把共用的 dacpac 跟那個 DB 自己的 dacpac 一起產出來。
清理受影響的 project 後再 build：

```bash
dotnet clean /path/to/Database.sqlproj
orbit sqlserver publish <dbname>
```

### `publish` 拒絕執行：「data loss might occur」
你正在縮欄位、drop table，或類似的破壞性變更。要嘛接受資料遺失：

```bash
orbit sqlserver publish <dbname> --allow-data-loss     # 顯示影響範圍並再次確認
```

要嘛把變更拆成更小的步驟。已檢視影響的非互動執行可使用
`--allow-data-loss --yes`。要丟棄本機資料並套用最新 schema，執行
`orbit sqlserver reset <dbname>`。

### 重新啟動設定指定的 SQL Server target 後，我的 SP 不見了
在 volume-seeding 那個修法之後不應該再發生。如果還是遇到：

1. 確認 DB 的檔案是放在 volume 裡：
   ```bash
   orbit sqlserver query "SELECT physical_name FROM sys.master_files WHERE database_id = DB_ID('YourDB')"
   ```
   路徑應該要以 `/var/opt/mssql/data/` 開頭（持久化 volume）。如果某顆資料庫整個不見了，用 `orbit sqlserver publish <db>` 重新發佈（或用 `orbit sqlserver publish --all` 發佈全部）。
2. 確認環境設定宣告的 volume 或 bind mount 還存在，而且不是剛剛才被重建。

### 移除 SQL Server volume 時失敗：「volume is in use」
設定指定的 SQL Server target 還掛在上面。如果你確定要永久丟棄所有本機
資料庫，先停掉環境，再移除環境設定宣告的 volume：

```bash
orbit down
docker volume rm <volume-name>
orbit up <sqlserver.target>
```

### `orbit sqlserver publish` 回報 SQL Server credential unavailable

確認 `sqlserver.password_env` 指向 target container 中非空的環境變數，然後
執行 `orbit restart <target>`。Orbit 不接受第二組密碼 override，避免悄悄
連到與 active environment 宣告不同的 target。

## Daemon

### `orbit daemon status` 說在跑，但 `orbit up` 連不到它
是 pid / socket 檔案殘留。Orbit 在下一次檢查時會自動偵測到死掉的 PID 並清掉這兩個檔案，所以直接重試就好：

```bash
orbit daemon status   # 或：orbit up
```

如果還是失敗，那就手動把檔案刪掉當 fallback：

::: details 手動清理

```bash
rm ~/.orbit/orbit.sock ~/.orbit/orbit.pid
orbit daemon start
```

:::

### Dashboard 的 :19800 port 打不開
Orbit 通常會自動選擇其他可用的 port，`orbit open` 也會使用實際位址。如果你有
明確設定 `ORBIT_DASHBOARD_PORT`，請移除設定或改用其他 port：

```bash
unset ORBIT_DASHBOARD_PORT
orbit daemon restart
```

Named instance 會自行選擇 dashboard port；請見
[隔離與 ports](instances.zh-TW.md)。

### 升級後 Orbit 顯示 update ready
這通常代表 binary 是手動替換的；`orbit update` 會自動重新連接正在執行的
環境。在 Dashboard 選擇 **Restart now**；Orbit 會在 daemon handoff 後重新
連線，並還原先前正在執行的 resources。如果 Dashboard 無法重新連線，請
執行 `orbit status` 顯示的精確指令，例如：

```bash
orbit daemon restart
```

## 檔案系統與權限

### `~/.orbit/` 變成 read-only 或是 owner 不對
通常是你之前用 root 裝了 Orbit，現在用自己帳號跑造成的。修 ownership：

```bash
sudo chown -R $(whoami) ~/.orbit
```

## CLI

### Lifecycle 指令還在等待，或觀測已結束

先看輸出的 operation ID 與 phase，再帶相同的 instance 與明確指定的 config flags
執行 `orbit operation <id> --json`。沒有 ID 時，使用 `orbit status --json`。
查詢成功仍可能得到失敗的 operation，請檢查 `data.state` 與清理警告；daemon
重新啟動後歷史會消失。停止緩慢可能仍在使用設定的 shutdown 緩衝時間。
Pipe 輸出也會有 elapsed heartbeat，讓已接受的工作有持續可見的進度。

`orbit down --timeout 30s` 限制 CLI 的觀測時間；Ctrl-C 則 detach。兩者都不會
撤銷 daemon 已接受的工作，重送前請先檢查目前狀態。即使 tracked resources
已顯示 stopped，失敗的 operation 仍會回報 stop 或清理錯誤。Docker 觀測失敗時，
用 `orbit doctor` 排查並恢復 Docker 存取，polling 會自動恢復。
只有 host resources 時，不需要 Docker 也能停止；但 `cleanup: "unavailable"`
的警告表示 orphan container 清理尚未驗證。結果與歷史紀錄
遺失的語意見 [lifecycle 觀測](agent-cli.zh-TW.md#lifecycle-觀測)。

### `orbit exec` 抱怨 container 名字不存在
Orbit 預設命名是 `orbit-<service>`。如果你設了 `ORBIT_NAMESPACE=foo`，名字會變成 `foo-<service>`。可以用 `docker ps --format '{{.Names}}'` 確認。

## 診斷

卡住的時候：

1. **`orbit doctor`** —— 檢查 Docker、Orbit 設定、daemon 狀態、ports 與必要的 host tools。每一項會回 `CheckPass` / `CheckWarn` / `CheckFail` 加一句提示。Extension 可以加入額外檢查,包括發行版專屬的 registry 或 repository 檢查。
2. **`orbit status`** —— 確認 Orbit 認為哪些 service 是健康的、以及哪些跟實際情況對不上。
3. **`orbit logs <resource> -f`** —— 即時看實際輸出。
4. **`ORBIT_LOG_LEVEL=debug orbit daemon restart`** —— 在 `~/.orbit/daemon.log` 打開 verbose daemon log。
5. **`docker ps -a`** / **`docker logs <container>`** —— 完全繞過 orbit，排除是它自己記帳記錯的可能性。

如果你遇到這份文件沒蓋到的失敗模式，請補上去、開 MR —— 這份清單的價值會隨著每筆新條目累積。

## 延伸閱讀

- [docs/architecture.zh-TW.md](architecture.zh-TW.md) —— event model 與 state machine 的背景
- [docs/configuration.zh-TW.md](configuration.zh-TW.md) —— YAML 欄位說明
- [安裝與更新](installation.zh-TW.md) — 安裝與升級方式
- [docs/sql-workflow.zh-TW.md](sql-workflow.zh-TW.md) —— 更深入的 SQL 流程
