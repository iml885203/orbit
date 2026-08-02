# 疑難排解

[English](./troubleshooting.md) · [繁體中文](./troubleshooting.zh-TW.md)

常見的失敗模式、它們的意思、以及如何修。先從 `orbit doctor` 開始 —— 它會抓到大部分的 setup 問題並直接告訴你怎麼修。

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

### `port already in use`
有別的 process 佔住了 Orbit 想用的 port。

Orbit 會自動重新辨識自己 namespace 內的 container 與已記錄的 host
process，包含 daemon 非預期結束後的情況。因此看到這個訊息，代表目前
owner 無法對應到所選的 Orbit environment；正常的 Orbit resource 不需要
使用者手動清理。

Orbit 會指出受影響的 resource 與 port，並提供一條唯讀指令查看目前 owner。
請透過管理該 process 的工具將它停止，或在 shared environment 中修改該
resource 的 host port，再重新執行 `orbit up`。讀取 container logs 或盲目
restart resource 都無法釋放由其他 process 佔用的 host port。

### 私有 registry `pull unauthorized` / `pull access denied`
你的 Docker client 沒辦法 pull image。中性版 `orbit doctor` 會確認 Docker 是否可用,但不會探測 private registry。請向你的 registry provider 驗證身份,例如：

```bash
docker login <registry-host>
```

如果你沒有 registry 權限，請聯絡 image owner 或管理員開通。

### `orbit up` 噴錯：「no env configs found in ~/.orbit/envs」
你還沒跑 `orbit init`（或者 sync 失敗了）。修法：

```bash
orbit env sync --url https://git.example.com/your-env-repo.git
orbit switch example
```

### 環境變更正在等待套用

執行 `orbit env apply`。Orbit 會先驗證更新後的環境，再記住目前運行中的
資源、套用更新並恢復這些資源；原本已停止的資源仍會保持停止。

若想先下載團隊更新、暫時不中斷目前環境，可使用
`orbit env sync --no-apply`，準備好後再套用。

## SQL Server

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

## Daemon

### `orbit daemon status` 說在跑，但 `orbit up` 連不到它
是 pid / socket 檔案殘留。Orbit 在下一次檢查時會自動偵測到死掉的 PID 並清掉這兩個檔案，所以直接重試就好：

```bash
orbit daemon status   # 或：orbit up
```

如果還是失敗，那就手動把檔案刪掉當 fallback：

```bash
rm ~/.orbit/orbit.sock ~/.orbit/orbit.pid
orbit daemon start
```

### Dashboard 的 :19800 port 打不開
Orbit 通常會自動選擇其他可用的 port，`orbit open` 也會使用實際位址。如果你有
明確設定 `ORBIT_DASHBOARD_PORT`，請移除設定或改用其他 port：

```bash
unset ORBIT_DASHBOARD_PORT
orbit daemon restart
```

### 升級後 Orbit 顯示 update ready
這通常代表 binary 是手動替換的；`orbit update` 會自動重新連接正在執行的
環境。resource mutation 會先暫停，避免跨版本操作。請執行 `orbit status`
顯示的精確指令，例如：

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

### `orbit exec` 抱怨 container 名字不存在
Orbit 預設命名是 `orbit-<service>`。如果你設了 `ORBIT_NAMESPACE=foo`，名字會變成 `foo-<service>`。可以用 `docker ps --format '{{.Names}}'` 確認。

### `orbit sqlserver publish` 回報 SQL Server credential unavailable

確認 `sqlserver.password_env` 指向 target container 中非空的環境變數，然後
執行 `orbit restart <target>`。Orbit 不接受第二組密碼 override，避免悄悄
連到與 active environment 宣告不同的 target。

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
- [docs/development.zh-TW.md](development.zh-TW.md) —— development setup 與 workflow
- [docs/sql-workflow.zh-TW.md](sql-workflow.zh-TW.md) —— 更深入的 SQL 流程
