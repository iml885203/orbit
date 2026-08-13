# 在自己的專案使用 Orbit

[English](./local-first.md) · [繁體中文](./local-first.zh-TW.md)

這條十分鐘路徑只需要在現有專案加入一個檔案。本機試用不需要 environment Git
repository、不需要 `orbit init`，也不需要編輯 `~/.orbit` 底下的設定。

請先安裝 [Orbit CLI](development.zh-TW.md#安裝-orbit)。下方範例也需要 Docker
與 Python 3；`orbit doctor` 會在啟動環境前檢查這三項。

## 1. 把開發意圖放在專案旁

在專案根目錄把以下內容存成 `orbit.yaml`：

```yaml
version: "3"

containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "26379:6379"

services:
  app:
    kind: frontend
    command: python3 -m http.server "$PORT"
    ports:
      http: 28080
    depends_on: [redis]
```

這個例子會用 Python 提供目前專案目錄，並先啟動 Redis；只使用公開 demo
已經需要的 Python 3 與 Docker。Orbit 會從 command 推斷 Python，並在
`orbit.yaml` 所在目錄執行；常見 Node、Bun 與 Go command 也採用相同規則。
只有 command 無法表達預期 runtime 或 working directory 時，才需要加入
`type` 或 `path`。Port 以檔案為準：Orbit 會把宣告的 port 注入為 `PORT`，衝突時會報錯並指出
占用的程式——不會有任何東西被默默移動。每個 endpoint 只宣告一次；資源只有一個
endpoint（或有名為 `http` 的 endpoint）時，Orbit 會先等它可用再啟動
dependents，並將它重用於 `orbit open` 與 dependency URL。只有 port 已經
listening 仍不足以證明 application ready 時，才需要明確加入
`health_check`。

## 2. 證明本機開發迴圈

從專案根目錄執行：

```bash
orbit doctor
orbit up
orbit open app
orbit logs app
orbit down
```

Orbit 會從目前目錄往上找到最近的 `orbit.yaml`，所以人在專案子目錄也能直接
使用。只有刻意要用另一份設定時，才需要用 `-c <path>` 明確覆寫。Orbit 會在
啟動前檢查實際的 service 目錄與工具、先啟動 Redis、再啟動 host process，
並開啟真正選到的 URL、保留 logs；本機 daemon 會自動啟動。

你可以直接移動到另一個同樣有 `orbit.yaml` 的專案。`orbit status` 會告訴你
另一個專案仍在執行，但不會把它的 resource 混入目前專案；`orbit doctor`
則會檢查目前專案是否已準備好。執行 `orbit up` 即可切換專案：Orbit 會先
驗證新專案；若舊專案仍有執行中的 resource，會明確列出兩個專案並在停止前
詢問確認，script 則使用 `orbit up --yes`。確認後才會停止舊專案並啟動目前
專案。`down`、`logs`、`open`
等指令絕不會誤控另一個專案的 resource。

此時需要理解的只有：

```text
your-project/
├── 你的程式碼
└── orbit.yaml          這次本機試用的 environment intent
        │
        ├── app         與這個檔案放在一起的 host process
        └── redis       Docker container

~/.orbit/               Orbit 管理的 runtime state；不要直接編輯
```

確認這個迴圈可用後，再把範例的 `command`、health check 與 dependency 換成
真實專案內容。所有欄位請見[設定參考](configuration.zh-TW.md)。環境運行中
修改 `orbit.yaml` 後，只要再執行 `orbit up`；Orbit 會在停止任何 resource
前先驗證新檔案，接著套用變更並恢復原本正在運行的 resources。

## 3. 分享已驗證的 environment

只在本機試驗時，可以一直保留 project-local file。需要讓隊友取得同一份
environment 時，再把驗證過的 config 放進獨立 Git repository：

```text
your-orbit-env/
└── envs/
    └── dev.yaml
```

複製後，替 service 加上這個 path：

```yaml
path: ${WORKSPACE_ROOT}
```

相對 path 會跟著 config file；`orbit.yaml` 位於專案內時，`.` 是正確位置。
省略 `path` 時，這也是預設值。同步後的 config 位於 Orbit 管理的
environment directory，
`${WORKSPACE_ROOT}` 則明確指回每位開發者自己的 checkout。

Commit 並 push `envs/dev.yaml`。確認共享副本已安全提交後，移除專案內的
`orbit.yaml`，讓 team repository 成為唯一真相來源，再從 project checkout
初始化：

```bash
orbit init --source team --url https://github.com/you/your-orbit-env.git --env dev
orbit up
```

Orbit 只會在 `dev.yaml` 實際證明需要 `${WORKSPACE_ROOT}` 時詢問 workspace。
請輸入這個 project checkout 的 absolute path；若從 Git checkout 內執行
init，直接按 Enter 就會採用該 checkout。每位開發者回答一次即可，共享檔案
不會包含個人電腦的 absolute path。開發 environment repository 時，可以用
`orbit source add local --path /path/to/your-orbit-env` 測試本機尚未 commit 的
檔案。

兩個階段各有一個用途：

```text
project-local orbit.yaml     不先處理 repository 儀式，專心學習與驗證
shared envs/dev.yaml         透過 Git 發布穩定的團隊意圖
```

Authentication、多 environment layout 與進階客製請見[團隊導入](team-adoption.zh-TW.md)。
