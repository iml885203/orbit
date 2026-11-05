# Environment sources

[English](./environment-sources.md) · [繁體中文](./environment-sources.zh-TW.md)

新增 environment source，就能使用並同步 Git repository 或本機目錄
`envs/` 裡的環境。用 `<source>/<environment>` 選擇指定來源的環境，
或用環境名稱選擇第一個 source 的環境。專案的 `orbit.yaml` 與透過
`-c <path>` 指定的設定檔各自獨立。

## 新增 sources

```sh
orbit source add company --url https://github.com/example/company-environments.git
orbit source add env-dev --path /work/orbit-environments
```

沒有 `--ref` 時會追蹤 repository default branch，並回報解析後的 branch 與
commit。本機同步包含尚未 commit 的檔案；移除 local source 不會變更使用者目錄。

## 管理與同步

```sh
orbit source list
orbit source sync
orbit source sync env-dev
orbit source sync --all
orbit source remove env-dev
```

若要更換 source 的位置，先移除再重新新增。Git 與本機 source 使用同一套流程。

同步會先驗證 staged cache，再替換 current cache。失敗時保留最後有效的
environments，錯誤會留在 source 狀態；舊 cache versions 保存在 Orbit 管理的
空間。`sync --all` 會嘗試全部 sources，任一失敗就以失敗結束。

## 選擇、檢查與移除

```sh
orbit env list
orbit switch e2e
orbit switch env-dev/e2e
orbit env info env-dev/e2e
```

`env info` 不會選擇、套用或啟動參數。只有 daemon 服務相同 qualified
identity 時才顯示觀察值。同步移除 selected、stopped environment 時，身分會
保持 unavailable，直到明確 switch；不會改用另一 source 的同名 environment。
執行中的 environment 即使從新 cache 消失，也會保留已載入身分與 config，
直到 switch 或 down。

不能移除擁有 running environment 的 source。移除 selected、stopped
environment 所屬 source 需要確認或 `--yes`，並清除 selection。移除第一個
source 時，Orbit 會自動使用下一個 source 解析裸 environment 名稱。

## 初始化與遷移

```sh
orbit init --source company --url https://github.com/example/company-environments.git \
  --workspace /work/company --env e2e --yes
orbit init --source env-dev --path /work/orbit-environments \
  --workspace /work/company --env e2e --yes
orbit init --source company --env e2e --yes
```

指定已註冊的 source 而不帶 `--url` 或 `--path` 時，會依註冊內容重新同步；
`--ref`（僅限 Git source）與 `--workspace` 會在同步成功後取代已儲存的值。
指定未註冊的 source 而不帶位置時，會像首次執行單純的 `orbit init` 一樣，以該名稱
註冊預設 repository（未覆寫時為官方 demo）。

既有單一 repository 設定、cached environments、selection 與 workspace 會在
無網路時一次遷移到 `default`。Orbit 會回報保留了哪些狀態，並建議檢查及同步
遷移後的 source。Source 與 selection 成功儲存後才移除舊設定。
