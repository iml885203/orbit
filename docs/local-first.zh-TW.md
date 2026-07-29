# 在自己的專案使用 Orbit

[English](./local-first.md) · [繁體中文](./local-first.zh-TW.md)

這條十分鐘路徑只需要在現有專案加入一個檔案。本機試用不需要 environment Git
repository、不需要 `orbit init`，也不需要編輯 `~/.orbit` 底下的設定。

## 1. 把開發意圖放在專案旁

在專案根目錄把以下內容存成 `orbit.yaml`：

```yaml
version: "3"

containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "${ORBIT_AUTO_PORT_LOCAL_REDIS:-26379}:6379"
    health_check:
      type: tcp

services:
  app:
    type: python
    kind: frontend
    path: .
    command: python3 -m http.server "$PORT"
    ports:
      http: "${ORBIT_AUTO_PORT_LOCAL_APP:-28080}"
    depends_on: [redis]
    health_check:
      type: http
      path: /
```

這個例子會用 Python 提供目前專案目錄，並先啟動 Redis；只使用公開 demo
已經需要的 Python 3 與 Docker。Orbit 會把實際選定的 application port 注入
為 `PORT`，因此偏好 port 被占用時不需要修改 config。每個 endpoint 只宣告
一次，Orbit 會把它同時用於 health check、`orbit open` 與 dependency URL。

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
驗證新專案、停止舊專案的 resource，再啟動目前專案。`down`、`logs`、`open`
等指令絕不會誤控另一個專案的 resource。

此時需要理解的只有：

```text
your-project/
├── 你的程式碼
└── orbit.yaml          這次本機試用的 environment intent
        │
        ├── app         host process，path 為 "."
        └── redis       Docker container

~/.orbit/               Orbit 管理的 runtime state；不要直接編輯
```

確認這個迴圈可用後，再把範例的 `command`、health check 與 dependency 換成
真實專案內容。所有欄位請見[設定參考](configuration.zh-TW.md)。

## 3. 分享已驗證的 environment

只在本機試驗時，可以一直保留 project-local file。需要讓隊友取得同一份
environment 時，再把驗證過的 config 放進獨立 Git repository：

```text
your-orbit-env/
└── envs/
    └── dev.yaml
```

複製前，把 service path 從：

```yaml
path: .
```

改成：

```yaml
path: ${WORKSPACE_ROOT}
```

相對 path 會跟著 config file；`orbit.yaml` 位於專案內時，`.` 是正確位置。
同步後的 config 位於 Orbit 管理的 environment directory，
`${WORKSPACE_ROOT}` 則明確指回每位開發者自己的 checkout。

Commit 並 push `envs/dev.yaml`。確認共享副本已安全提交後，移除專案內的
`orbit.yaml`，讓 team repository 成為唯一真相來源，再從 project checkout
初始化：

```bash
orbit init --env-repo https://github.com/you/your-orbit-env.git --env dev
orbit up
```

Orbit 只會在 `dev.yaml` 實際證明需要 `${WORKSPACE_ROOT}` 時詢問 workspace。
請輸入這個 project checkout 的 absolute path；若從 Git checkout 內執行
init，直接按 Enter 就會採用該 checkout。每位開發者回答一次即可，共享檔案
不會包含個人電腦的 absolute path。開發 environment repository 時，可以用
`orbit env sync --path /path/to/your-orbit-env --yes` 測試本機尚未 commit 的
檔案。

兩個階段各有一個用途：

```text
project-local orbit.yaml     不先處理 repository 儀式，專心學習與驗證
shared envs/dev.yaml         透過 Git 發布穩定的團隊意圖
```

Authentication、多 environment layout 與進階客製請見[團隊導入](team-adoption.zh-TW.md)。
