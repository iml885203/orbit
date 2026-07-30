# Orbit — Agent 指引

[English](./AGENTS.md) · [繁體中文](./AGENTS.zh-TW.md)

Orbit 是團隊中性的本地開發 orchestrator(Go CLI + daemon + Svelte UI)。這個單一 repo 包含中性 engine、CLI、daemon、UI，以及受 gate 掃描的功能 package `internal/devdb`（SQL schema 工作流程）與 `internal/tunnel`（staging callback tunnel），在 dashboard 透過 `ui/src/lib/devdb` 與 `ui/src/lib/tunnel` 呈現。專案背景請讀 [README.zh-TW.md](README.zh-TW.md) 與 [docs/](docs/) 底下的結構化文件。

這份檔案是針對 agent 的指引。專案規範放在 [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) 與 `.claude/rules/`。

## 界線

### 一律照做
- 解析 CLI 輸出時加 `--json`。人類可讀的輸出不是穩定 contract。完整的 agent JSON contract（`orbit.cli.v1` envelope、結構化錯誤、converted commands、NDJSON log streaming、legacy JSON 例外）請讀 [docs/agent-cli.zh-TW.md](docs/agent-cli.zh-TW.md)。
- 在**每次 commit 前**（不是只在最後）跑 `make preflight`（完整 CI gate：build、tests、vet、verify-types、check-neutral）。它存在是為了抓兩個雷：(1) 改動任何 Go struct 或 config 欄位後 —— 連 tygo 會輸出的 doc comment 也算 —— 要跑 `make gen-types` 並把結果 stage，否則 `verify-types` 會因為漂掉的 `ui/src/lib/types/*.gen.ts` 而失敗；(2) `check-neutral` 會拒絕整個 tree 裡任何品牌/團隊名稱（「example」、「dbproject」…），包含受 gate 掃描的功能 package `internal/devdb` 與 `internal/tunnel` —— 全部保持中性（許可的例外列在 `scripts/check-neutral.sh`）。`make lint` 是更嚴格的 golangci 檢查。
- 動非瑣碎的改動前先讀 [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md)。

### 先問再動
- 直接編輯 `~/.orbit/settings.json` —— 改走 UI/CLI 路徑。
- 破壞性操作：`docker volume rm`、`orbit sqlserver reset`（把單一 DB 回復到最新 schema 的乾淨狀態，會丟掉本機資料變更）、`orbit sqlserver publish --force`（允許可能造成資料遺失的 schema 變更）。舊的 image-build flow 與 container 端 apply 已移除；publish / reset 全程在 host 上執行。

### 永遠不要
- `git push --force`、`git commit --no-verify`。
- WHAT-comments。註解只回答 WHY。（CODE_CONVENTIONS §2）
- Service indirection / 薄薄一層的 interface。（CODE_CONVENTIONS §6）
- 把跨領域的 helper 塞進 `utils.go` / `shared/`。（CODE_CONVENTIONS §4）
- 在 production 程式碼用 `strings.Contains(err.Error(), ...)` 做錯誤分類。（CODE_CONVENTIONS §9）

## Rules

專案規範放在 `.claude/rules/`。每個檔案的 frontmatter 都有 `paths:` 宣告何時生效。

- **Claude Code**：當你 Read 到匹配的檔案時，rule 會自動載入。
- **Codex CLI / 其他 agent**：編輯前要顯式讀進來：
  - Go 檔（`*.go`）：`.claude/rules/error-handling.md`、`.claude/rules/go-*.md`、`.claude/rules/domain-organization.md`
  - Svelte / TS 檔：`.claude/rules/svelte-*.md`、`.claude/rules/domain-organization.md`
- 動 UI 設計前先讀 [DESIGN.md](DESIGN.md)；那是 Orbit dashboard 視覺系統面向 agent 的權威來源。

完整的 rationale 與範例放在 [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md)。

## CLI workflow

```bash
orbit up --infra              # start containers
orbit up                      # start services
orbit status --json           # verify state
orbit switch <env>            # change env and restart daemon
orbit logs <service>          # plaintext; -f to stream
orbit doctor                  # checks setup; prints fixes
orbit sqlserver query "SELECT TOP 5 * FROM Users"
orbit sqlserver publish <dbname> # fast dev-loop, preserves data
```

如果 `orbit up` 抱怨缺 env，跑 `orbit init` 或 `orbit env sync`。
`orbit up` 會自動把 daemon 起起來；不必另外 `orbit daemon start`。

## Code review

- `/orbit-review` —— review unstaged + staged 的改動
- `/orbit-review <base>` —— review 分支 vs base（例如 `/orbit-review main`）

## 想看更多

- [README.zh-TW.md](README.zh-TW.md) —— 入口
- [docs/CODE_CONVENTIONS.md](docs/CODE_CONVENTIONS.md) —— 完整 coding 規範
- [architecture.zh-TW.md](docs/architecture.zh-TW.md) —— state machine 與 event model
- [docs/troubleshooting.zh-TW.md](docs/troubleshooting.zh-TW.md) —— runtime 與 optional DB workflow 錯誤
