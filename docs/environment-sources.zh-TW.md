# Environment sources

[English](./environment-sources.md) · [繁體中文](./environment-sources.zh-TW.md)

Environment source 用來散布 Orbit 管理的 environments。每個 source 是含有
`envs/` 的 Git repository 或持久化本機目錄，也可以綁定一個由全部
environments 共用的 application workspace。受管理 environment 在 CLI JSON、
daemon API、runtime state 與 dashboard 都使用 `<source>/<environment>` 身分。
不同 sources 可以有相同短名稱；裸名稱只從 default source 解析。Project
`orbit.yaml` 與明確 `-c <path>` config 仍然獨立。

## 新增 sources

```sh
orbit source add company --url https://github.com/example/company-environments.git \
  --workspace /work/company --default
orbit source add env-dev --path /work/orbit-environments \
  --workspace /work/company
```

沒有 `--ref` 時會追蹤 repository default branch，並回報解析後的 branch 與
commit。本機同步包含尚未 commit 的檔案。Source 與 workspace 可為相同路徑，
但 Orbit 不會互相推論；移除 local source 不會變更使用者目錄。

## 管理與同步

```sh
orbit source list
orbit source info company
orbit source sync
orbit source sync env-dev
orbit source sync --all
orbit source update company --ref release/2026.08
orbit source update company --clear-ref
orbit source update company --workspace /worktrees/company-pr-42
orbit source update company --clear-workspace
orbit source update company --default
orbit source remove env-dev
```

一般流程只需要 `add`、`list`/`info`、`sync`、`update` 與 `remove`。
舊的 `set-default`、`set-workspace`、`clear-workspace` 會在一個遷移週期內
保留為 deprecated compatibility aliases。只修改 metadata 不會同步 source
內容。

同步會先驗證 staged cache，再替換 current cache。失敗時保留最後有效的
environments，錯誤會留在 source 狀態；舊 cache versions 保存在 Orbit 管理的
空間。`sync --all` 會嘗試全部 sources，任一失敗就以失敗結束。只有 active
managed environment 所屬 source 的更新可能需要 `orbit env apply`。

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
environment 所屬 source 需要確認或 `--yes`，並清除 selection。仍有其他
source 時必須先指定新的 default；Orbit 不會猜測替代項目。

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

遷移期間，不帶 repository 變更 flags 的 `orbit env sync` 仍會同步 default
source，並顯示 deprecated 提示。舊的 `--url`、`--path`、`--ref` 不會隱含修改
source；它們會失敗並提供精確的 `orbit source` 替代指令，且不會靜默忽略任何
已接受的 flag。
