# 為你的團隊採用 orbit

[English](./team-adoption.md) · [繁體中文](./team-adoption.zh-TW.md)

Orbit 是本地開發協調器：一份 YAML env 檔描述 containers 與 services，`orbit up` 會依照相依順序啟動，並提供 health checks、logs、tracing，以及位於 <http://localhost:19800> 的 dashboard。

這個單一 repo 同時包含中性 engine、CLI、daemon、UI，以及透過明確 extension seams 接入的 ExampleTeam 功能集。其他團隊一般直接使用已發行的 Orbit binary，只在獨立 env repo 維護自己的環境設定。

## 純設定採用

1. 建立一個含 `envs/` 目錄與 env YAML 檔的 git repo。[examples/quickstart/dev.yaml](examples/quickstart/dev.yaml) 可作為最小起點。
2. 將 Orbit 指向該 repo 並選擇環境：

   ```sh
   orbit env sync --url <your-env-repo-git-url>
   orbit switch dev
   orbit up
   ```

   `orbit init` 會以互動方式完成相同設定。開發期間可用 `orbit env sync --path /path/to/your-env-repo` 指向本地 checkout。若目前使用中的環境有變更，sync 會詢問是否更新目前環境，並且只恢復原先運行中的資源。只有必須延後中斷時才需要 `--no-apply`；Orbit 會印出之後完成更新的精確指令。

已發行 binary 會提供 distribution defaults。若自訂 build 沒有這些預設，`orbit env sync` 可設定 `env_repo_url` 或 `ORBIT_ENV_REPO_URL`，`orbit update` 可設定 `ORBIT_INSTALL_URL`。其餘 services、containers、graph、logs、health checks、doctor 與 dashboard 只需 env 設定即可運作。Tracing 預設開啟；env 可用明確的 `tracing.enabled: false` opt out。

## 編譯時客製

當純設定不夠用時，extension contract 仍然可用。團隊可 fork 這個 repo，或將它作為 Go module require，提供自己的 `cmd/orbit`，並將額外的 `extension.Extension` 傳給 `app.Main`。每個 extension 可提供 CLI commands、daemon setup 與 hooks、doctor 與 init 行為，以及 distribution defaults；請見 [extension/extension.go](../extension/extension.go)。

UI build 在 [ui/vite.config.ts](../ui/vite.config.ts) 保留三個編譯時 seam：

- `ORBIT_UI_EXT` 用團隊的 navigation、routes、panels、settings sections 與 lifecycle hooks 取代 `$ext` module。
- `ORBIT_UI_TYPES` 取代 generated-types barrel。
- `ORBIT_UI_OUTDIR` 選擇由團隊 binary embed 的 dashboard build output。

這條路線代表團隊必須自行維護 build 與 release line。只在 env 設定無法表達需求時使用，並將中性改進留在這個 repo，避免複製 core。
