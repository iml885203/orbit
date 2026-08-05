# Environment sources

[English](./environment-sources.md) · [繁體中文](./environment-sources.zh-TW.md)

Environment source 是含有 `envs/` 的 Git repository 或本機目錄。只需新增
一次，之後想取得最新 environments 時執行同步。受管理 environment 使用
`<source>/<environment>` 身分；裸名稱來自第一個 source。Project `orbit.yaml`
與明確 `-c <path>` config 仍然獨立。

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

指令只有 `add`、`list`、`sync` 與 `remove`。若要更換 source 位置，
移除後重新新增即可；Git 與本機 source 使用同一套流程。

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
```

既有單一 repository 設定、cached environments、selection 與 workspace 會在
無網路時一次遷移到 `default`。Orbit 會回報保留了哪些狀態，並建議檢查及同步
遷移後的 source。Source 與 selection 成功儲存後才移除舊設定。

遷移期間，不帶 repository 變更 flags 的 `orbit env sync` 仍會同步第一個
source，並顯示 deprecated 提示。舊的 `--url`、`--path`、`--ref` 不會隱含修改
source；它們會失敗並指向明確的移除後重新新增流程，且不會靜默忽略任何已接受
的 flag。
