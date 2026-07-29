# 為你的團隊採用 orbit

[English](./team-adoption.md) · [繁體中文](./team-adoption.zh-TW.md)

Orbit 是本地開發協調器：一份 YAML env 檔描述 containers 與 services，`orbit up` 會依照相依順序啟動，並提供 health checks、logs、tracing，以及位於 <http://localhost:19800> 的 dashboard。

這個 repo 包含中性 engine、CLI、daemon、UI，以及透過明確 extension seams 接入的選用功能 packages。團隊一般直接使用已發行的 Orbit binary。Environment repository 是之後的發布方式，不是本機證明 Orbit 是否適用的前置條件。

## 純設定採用

1. 先在一個真實專案旁驗證 environment。[local-first
   指南](local-first.zh-TW.md) 使用 project-root `orbit.yaml`，走過隊友之後也會
   使用的 `doctor`、`up`、`open`、`logs`、`down` 迴圈；完全不需要 Git
   environment repository 或永久 Orbit settings。
2. 確認設定有價值後，再建立 Git repository 與 `envs/dev.yaml`。把
   `path: .` 這類 project-relative path 改成明確的 `${WORKSPACE_ROOT}` path，
   commit 後從 project checkout 初始化：

   ```sh
   orbit init --env-repo <your-env-repo-git-url> --env dev
   orbit up
   ```

   Orbit 只會在選到的設定實際引用 `${WORKSPACE_ROOT}` 時詢問 workspace；
   只有 containers 的 environment 不會暴露這個概念。`orbit doctor` 會檢查
   每個解析後的 service 目錄，並在 `orbit up` 啟動任何相依資源前只給一個
   修正指令。

   Host service 可使用任何本機已安裝的 runtime；只有 `dotnet` 具有特殊的 build 行為：

   ```yaml
   services:
     api:
       type: python
       path: ${WORKSPACE_ROOT}/api
       command: python3 -m http.server 8080
       ports:
         http: 8080
   ```

   開發期間可用 `orbit env sync --path /path/to/your-env-repo --yes` 指向本地、
   尚未 commit 的 environment files。若目前使用中的 environment 有變更，
   sync 會詢問是否更新，並且只恢復原先運行中的資源。只有必須延後中斷時才
   需要 `--no-apply`；Orbit 會印出之後完成更新的精確指令。

已發行 binary 會提供 distribution defaults。若自訂 build 沒有這些預設，`orbit env sync` 可設定 `env_repo_url` 或 `ORBIT_ENV_REPO_URL`，`orbit update` 可設定 `ORBIT_INSTALL_URL`。其餘 services、containers、graph、logs、health checks、doctor 與 dashboard 只需 env 設定即可運作。Tracing 預設開啟；env 可用明確的 `tracing.enabled: false` opt out。

## 編譯時客製

當純設定不夠用時，extension contract 仍然可用。團隊可 fork 這個 repo，或將它作為 Go module require，提供自己的 `cmd/orbit`，並將額外的 `extension.Extension` 傳給 `app.Main`。每個 extension 可提供 CLI commands、daemon setup 與 hooks、doctor 與 init 行為，以及 distribution defaults；請見 [extension/extension.go](../extension/extension.go)。

UI build 在 [ui/vite.config.ts](../ui/vite.config.ts) 保留三個編譯時 seam：

- `ORBIT_UI_EXT` 用團隊的 navigation、routes、panels、settings sections 與 lifecycle hooks 取代 `$ext` module。
- `ORBIT_UI_TYPES` 取代 generated-types barrel。
- `ORBIT_UI_OUTDIR` 選擇由團隊 binary embed 的 dashboard build output。

這條路線代表團隊必須自行維護 build 與 release line。只在 env 設定無法表達需求時使用，並將中性改進留在這個 repo，避免複製 core。
