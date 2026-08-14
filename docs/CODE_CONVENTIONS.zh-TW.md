# Code conventions

[English](./CODE_CONVENTIONS.md) · [繁體中文](./CODE_CONVENTIONS.zh-TW.md)

本文件記錄 Orbit 在結構與風格決策上所遵循的原則。寫下來是為了讓未來的貢獻者能做出一致的判斷，而不必去逆向工程整段重構歷史。

---

## 1. 哲學

### 以 domain 而非 layer 來組織

Code 是依照「它做什麼」來分組，而不是依照「它是哪一種物件」。`internal/daemon/handlers_settings.go` 屬於 settings 這個 domain；`internal/engine/dep_graph.go` 屬於 dependency-graph 這個概念。一個叫做 `services.go` 或 `helpers.go` 的檔案會讓人忍不住問「是哪個 domain？」——答案是什麼，就用那個答案來命名。

### Composition 優於 service indirection

優先直接呼叫 function，而不是把它包在一個注入進來的 service 裡。優先使用帶有明確小型 API 的 struct，而不是為了給某一層取名而存在的薄薄一層 interface。一個有用的反例：`Orchestrator` 暴露了像 `UpdateDetachedDeps`、`GetServiceInfo` 這類方法——這不是 service indirection，而是把 mutable state 封裝在一個明確邊界後面。

### 由命名承載意圖——comment 描述 rationale，不描述動作

如果你需要靠 comment 來說明某一行 code 在做什麼，那就表示這行 code 名字取得不夠好。把 function、variable、type 重新命名，直到 code 自己會說話為止。Comment 留給那些 code 本身無法表達的東西：某個不直覺選擇背後的 *why*、外部限制、有意識的取捨。

---

## 2. 何時該寫 comment

**預設是不寫。** 不是「不要寫 WHAT-comment」，而是預設一個都不寫。一則只是
把程式碼與命名已經表達的東西再講一次的 WHY-comment，同樣是雜訊；多數說明
應該放進命名、測試或 commit message，優先考慮這三者：

- 想說明一個 function 做什麼 → 改個更好的名字，並補一個能展示它的測試。
- 想說明為什麼這樣改 → 寫在 commit message，它會跟著這次變更走，不會留在
  檔案裡腐爛。
- 想說明某個 contract 或規則 → 寫在擁有它的文件裡；需要被強制時，用測試
  名稱指向它。

只有在「未來的讀者不知道就會重新引入 bug」時才寫 comment——那種任何命名或
測試都承載不了的隱形限制。門檻很高，以下範例才達得到。

### 該寫 comment 的時機：

**說明某個不直覺的選擇為什麼這樣做。**
`ui/src/lib/tooltip.svelte.ts` 裡的 tooltip singleton 是經典例子。`owner === node` 這個 guard 之所以存在，是因為 Svelte 的 `update()` 會在同一個 microtask 裡對頁面上 *所有* 還活著的 `use:tooltip` element 觸發一次——少了這個 guard，一個 SSE tick 就會把你正在 hover 的 tooltip 文字蓋掉，蓋成最後一個 update 的 element 的內容。這個 comment 解釋了一個從 code 上看不出來的 bug：

```ts
// The `owner` field is load-bearing. Svelte action `update()` fires on every
// reactive change to the bound props, and reactive store updates (e.g. SSE
// ticks) trigger update on *every* live `use:tooltip` on the page in the
// same microtask. Without `owner === node` gating, the hovered tip's text
// gets stomped on by whichever element happens to update last.
```

**記錄外部或環境上的限制。**
`internal/daemon/config_staleness.go` 說明了為什麼 transient editor save 與 atomic-rename window 會被視為 unknown，而不是 stale：

```go
// fileStamp hashes a config file. ok=false when the file can't be read —
// callers treat that as "unknown", never as stale (transient editor saves
// and atomic-rename windows shouldn't flap the flag).
```

**說明 wire-format 或 API shape 上的限制。**
`daemon/wire_more.go` 解釋了為什麼 `SettingsResponse` 要重複欄位、而不是直接 embed `APIResponse`：

```go
// SettingsResponse is the /api/settings response. Fields duplicate APIResponse
// rather than embed it so the wire shape stays flat for TS codegen.
```

**記錄 atomicity 或 concurrency 的保證。**
`atomicio/atomicio.go` 解釋了 rename-is-atomic 這個保證，以及它的前置條件：

```go
// writeFileAtomic writes data to path via a temp file + rename. The rename
// is atomic on POSIX when source and dest are on the same filesystem, which
// is guaranteed here because the temp file is created in the target's dir.
```

**標註一個有意識的取捨。**
如果你因為 N 有上限而接受了 O(N²)，就寫下來。如果你因為 profiling 顯示這條路徑是 cold path 才選了比較簡單的演算法，也寫下來。

**標記之後要回頭看的事項。**
TODO 如果帶有 context 是可以接受的：誰、何時、為什麼現在不做。光禿禿一個沒有 context 的 `// TODO` 只是雜訊。

### 不該寫 comment 的時機：

- 它一步一步描述 code 在做什麼。改用 rename 或 extract。
- 它只是把 function 名字用散文重講一次。
- 它總結了一個區塊，但區塊的名字已經說了同樣的事。
- 它是之前某個設計留下來的，現在已經不適用了。

---

## 3. Refactoring 訊號——WHAT-comment 規則

Martin Fowler 的 *Refactoring*（第 2 版，"Comments" smell）與 Robert C. Martin 的 *Clean Code*（第 4 章）得到了相同的結論：當你需要寫一段 comment 去解釋 code *做什麼* 的時候，這是個訊號，告訴你 code 需要更好的結構，而不是更多的說明。

Fowler：*「當你覺得需要寫 comment 的時候，先試著重構 code，讓那段 comment 變得多餘。」* 推薦的工具是 Extract Function 和 Change Function Declaration（rename）。

Robert C. Martin 把那些只是把 code 用散文重述一遍的 "noise comments" 視為實際有害——它們增加長度卻沒帶來資訊，而且過時時也不會發出任何警告。

**Review 時要抓的 pattern：**

| 你寫了這種 comment | 修法 |
|---|---|
| `// computes detached deps for this service` | 刪掉——`detachedDepsFor(name)` 本身就已經這樣說了 |
| `// loop over services to find the running ones` | Extract 成 `runningServices(services)`，刪掉 comment |
| `// build the list of enabled features` | Function 已經叫 `enabledFeatures(cfg)`——刪掉 |
| `// check if node is owner before mutating` | 已經寫在 guard 條件裡——刪掉 |
| 用一長段 comment 解釋一個 function 怎麼運作 | 把這個 function 拆開，直到每一塊都會自己解釋自己 |
| 需要靠 comment 才能看懂的 variable 名字 | 把 variable 重新命名 |

通過這層過濾還活下來的 comment，幾乎一定是 WHY comment，該留就留。

---

## 4. Domain 組織——Go

**檔案是以它擁有的 domain 命名，而不是以 layer 命名。**

好的命名：`handlers_settings.go`、`handlers_graph.go`、`config_staleness.go`。每個名字都在回答「這個檔案擁有哪個 domain？」。不好的命名：`services.go`、`helpers.go`、`utils.go`——它們回答的是「這是哪一種東西？」，而這完全沒告訴你它在做什麼。

**一個 package 回答的是「這個 domain 做什麼？」**

`internal/engine/` 擁有 dependency graph、orchestration、scheduling。`internal/daemon/` 擁有 HTTP API、SSE、settings、environment lifecycle。如果一個概念橫跨多個 package，問問哪個 package *擁有* 它。`detachedSet` 之所以放在 `engine/dep_graph.go`，是因為 detach 是 dependency-graph 的概念；它不會只因為 daemon 會呼叫它，就應該住在 daemon package 裡。

**跨 domain 的 helper 放在擁有該概念的 domain 裡。**

不要為了把雜七雜八的東西都塞進去就建一個 `shared/` 或 `common/` package。如果這個概念有家，就把它放回家。如果它真的找不到自然的歸屬，就建一個名字明確的最小 package（`internal/fsutil`，而不是 `internal/utils`）。

**檔案大小。**

目標是 200–400 行。檔案超過約 800 行通常是個訊號：某個 domain 被併進了鄰居 domain 裡；沿著你能找到的邊界把它切開。

---

## 5. Domain 組織——Svelte / TypeScript

**Component 檔案對應到使用者看得見的功能。**

`NodeDrawer.svelte` 是對的——drawer 是個 UI 概念，名字也說明了它畫什麼。`Components.svelte` 或 `Panel.svelte` 就不是可以接受的名字，因為它們描述的是技術 layer，而不是 feature。

**只服務於單一 parent 的 sub-component，放在 parent 旁邊。**

`NodeEnvPanel.svelte` 和 `NodeDepsPanel.svelte` 跟 `NodeDrawer.svelte` 一起住在 `components/graph/` 底下。它們不是可以通用重用的 component；它們存在是為了服務這個 drawer。Co-location 讓這件事顯而易見。

**Store 依 domain 拆分。**

`store.graph` 放 graph dashboard 的 view state（selection、preview、env-switch 進度）。`store.daemon` 放從 SSE 灌進來的 daemon live state。`store.ui` 放短暫的 UI state（toast、modal）。這樣拆是為了讓 state 能依「用途」被找到，而不是去捲一個大鍋飯式的 bag。標準寫法見 `ui/src/lib/stores.svelte.ts`。

這個拆法在正確性上也有後果：`resetForNewDaemon` 在重新連線時只清掉 `store.daemon`。`store.graph` 不清，因為清掉會讓畫面閃一下變空白；`store.ui` 也不清，因為使用者意圖（開著的 modal、目前的 SQL mode）必須在 daemon restart 後仍然存在。沒有這條 domain 邊界，這個決策就會變得隱形。

**純粹的 helper 屬於 `lib/<domain>.ts`，不屬於 `lib/utils.ts`。**

`lib/graphActions.ts`、`lib/logClassify.ts`、`lib/traceColor.ts`——每個名字都說了它擁有哪個 domain。一個 `lib/utils.ts` 檔案會變成各種無關 code 的磁鐵，最後很難導覽。

**通用 UI primitives**（任何 project 裡都可能存在的那種 component）屬於最上層的 `components/` 目錄：`Btn.svelte`、`Toast.svelte`、`ConfirmModal.svelte`。Feature 專屬的 component 則放在 `components/<feature>/` 底下。

---

## 6. Composition 優於 service indirection

優先直接呼叫，而不是透過注入進來的中間層。

```go
// Prefer this:
deps := graph.DepsOf(name)

// Over this:
deps := svc.GetDependenciesForService(name) // where svc wraps graph
```

第二種寫法只有在中間層真的增加了什麼價值（state 管理、caching、access control）時才合理。如果它只是個 pass-through，這一層付出的成本就比它帶來的還多。

**優先使用具體 struct + method，而不是薄薄一層 interface。**

Interface 在 package 邊界上（caller 和實作分屬不同 package），或是你需要在 test 裡 stub 時是有用的。在同一個 package 內，具體 type 反而更清楚。

**`Orchestrator` 不是 service layer。** 它之所以暴露 `UpdateDetachedDeps`、`GetServiceInfo`、`StartService` 等等，是因為它擁有需要被多個 goroutine 透過單一 lock 存取的 mutable state。這是 encapsulation，不是 indirection。差別在於：service layer 包住的邏輯其實可以直接被呼叫；orchestrator 擁有不能直接存取的 state。

---

## 7. Comment audit checklist

在 request review 之前，拿這份 checklist 跑過自己的 PR：

- [ ] 每一段 comment 都在回答「why」，而不是「what」
- [ ] 沒有任何 comment 是把 function signature 用散文重講一次
- [ ] 沒有光禿禿的 `// TODO`——每一個都有名字、日期、context
- [ ] 沒有比下面的 code 還舊的 comment（不確定的話 check git blame）
- [ ] 如果一個 function 需要 comment 才能導覽，這個 function 大概太長了——拆它

---

## 8. References

**Martin Fowler, *Refactoring: Improving the Design of Existing Code*, 2nd ed. (2018) — "Comments" smell (Chapter 3)。**
Fowler 主張 comment 常常被「當作除臭劑」蓋在爛 code 上。他的核心建議：在寫一段 comment 來解釋某個 block 之前，先試 Extract Function 或 Change Function Declaration（rename），讓 comment 變得不必要。撐過重構還留下來的 comment 就是合法的；它們解釋 why，不解釋 what。

**Robert C. Martin, *Clean Code: A Handbook of Agile Software Craftsmanship* (2008) — Chapter 4: Comments。**
Martin 區分了好的 comment（解釋意圖、警告後果、放大不直覺的重要性）和雜訊 comment（重述 code 在做什麼、多餘的 Javadoc、日記式 comment）。他的核心主張：每一段 comment 都是「沒能在 code 裡表達清楚」的失敗，目標是透過改 code 來把這種失敗降到最低，而不是用散文去打補丁。

---

## 9. Error handling (Go)

見 `.claude/rules/error-handling.md`。

用 `%w` wrap error；用 sentinel/typed error 加 `errors.Is` / `errors.As` 來分類，不要靠對 `err.Error()` 做 string matching。

## 10. Mutable state (Go)

見 `.claude/rules/go-mutability.md`。

Mutable shared state 必須封裝在一個擁有 lock 的 receiver type 裡面。任何需要持有 lock 才能存取的 exported 欄位，要在 comment 裡記下 lock 的前置條件。

## 11. Callback vs interface (Go)

見 `.claude/rules/go-callbacks.md`。

對於 package 內部的訊號傳遞，優先用 function-field callback（`OnOutput`、`OnExit`），而不是薄薄一層 interface。Interface 只在需要 test mocking 或跨 package 邊界時才定義。

## 12. Event-loop drop policy (Go)

見 `.claude/rules/go-event-loop.md`。

有 subscriber 的 event loop 必須在 type 的 comment 裡記下它的 drop policy。Non-blocking send 會把慢的 subscriber 直接丟掉（對 observational subscriber 來說可以接受）；control-plane 的 subscriber 則需要自己一條獨立的 channel。

## 13. SSE vs poll (Svelte / TS)

見 `.claude/rules/svelte-async.md` 與 `.claude/rules/svelte-error-surface.md`。

即時、多 consumer 的 state 用 SSE；長時間執行的任務用 polling。不要在同一個資料源上混用兩種 pattern。無法復原的 error 要透過 toast 或 panel state 浮現出來——絕對不要默默吞掉。

## 14. Component-scoped type vs `lib/types.ts` (Svelte / TS)

見 `.claude/rules/svelte-types.md`。

Type alias 預設定義在 `.svelte` script 裡，除非有 3 個以上的 consumer；超過再抽到 `lib/<domain>-types.ts`，並加上 domain 前綴。Export 出去的 component-scoped type 要在 comment 裡寫明它的 consumer。

## 15. Accessibility 基本原則 (Svelte)

見 `.claude/rules/svelte-a11y.md`。

所有可互動的 element 都要有看得見的文字或 `aria-label`。Modal 要用 `role="dialog"` + `aria-modal="true"`，並搭配 `aria-label` 或 `aria-labelledby`。Status indicator 用 `role="status"`。

## 16. Loading state pattern (Svelte)

見 `.claude/rules/svelte-loading.md`。

用 `$state` boolean flag（`loading`、`busy`）搭配 `disabled` 和 `aria-busy="true"`。除非內容高度會有顯著變化，否則不用做 skeleton screen。

## 17. Domain entry points (Go)

見 `.claude/rules/go-domain-model.md`。

當同樣的多步驟 setup 在 3 個以上的呼叫點重複出現，這就是個訊號：這個 domain 還沒暴露出該有的 method。把它連同對應的 sentinel error 和面向使用者的提示一起下推到擁有它的 domain。CLI 端那種 `requireFoo()` helper 是壞味道。

## 18. 在行為邊界寫測試

見 [testing.md](https://orbit.dotw.me/docs/testing)。

Orbit 偏好 end-to-end journey 與 sociable domain test。Solitary test 留給演算法、parser、escaping、安全邊界、concurrency invariant 和穩定的 wire contract——不是 DTO builder、getter、薄 wrapper 或 private 呼叫順序。寧可用一個測試涵蓋一個 public 行為，也不要為它的實作 helper 寫好幾個測試。

## 19. Definition of Done——commit 前 checklist

`make preflight` 把守可機器檢查的部分。以下是每次 commit 前要自問的判斷題：

- **不留死重。** 如果這次改動讓某個檔案、文件或段落過時，就在同一個 commit 裡刪掉。沒有「先留著以防萬一」。
- **文件簡單易懂、維護便宜。** 寫文件要讓人第一次讀就懂：句子短、指令具體、不灌水。避免會隨功能變動而需要跟著改的文件；真的需要時只保留一份權威來源，其他位置用連結指過去。
- **想解釋就是該重構的訊號。** 想寫文件或註解來解釋程式碼，代表應該先重構程式碼（§3）。
- **程式碼自己說出意圖。** 功能不是能動就算完成——實作完要再做一輪重構，讓命名與結構不靠旁白就能突顯意圖。
- **行為有測試保護。** 每個功能都要有守在行為邊界的測試——絕不寫一對一鏡射實作的單元測試（§18、[testing.md](https://orbit.dotw.me/docs/testing)）。

### 文件 ownership

每個 product contract 都只有一份 authoritative owner。Guides 依讀者情境摘要
workflow 並連回 owner，不重新定義 contract。

| 文件 | 負責內容 |
|---|---|
| `configuration.md` | YAML schema、config resolution 與 field semantics |
| `agent-cli.md` | JSON envelopes、payloads、errors 與 wire compatibility |
| `plugins/orbit-agent/skills/orbit/` | Agent decision policy 與 operational workflow |
| `instances.md`、`tracing.md`、`sql-workflow.md` 等 domain references | 單一 product domain 對使用者可見的行為與 safety model |
| `architecture.md` | Implementation model 與 extension boundaries |
| README 與 task-oriented guides | 依讀者情境提供入口，並連向 owning contract |

新 domain 沒有自然 owner 時，建立一份聚焦的 reference，不要把規則複製到每份
既有 guide。英文與 `.zh-TW.md` pair 必須保持行為等價；措辭可以不同，但 contract、
commands、safety 與 supported workflows 不可不同。

### 文件 impact checklist

每次改動使用者可見行為、CLI、config 或 wire contract 時：

- [ ] 寫 prose 前先指出 authoritative document。
- [ ] 更新 owner，再搜尋其他位置過時或重複的說法；讀者不需要完整內容時，刪除
      重複敘述並改成連結。
- [ ] 更新成對的 `.zh-TW.md` 文件；若沒有翻譯 pair，確認並記錄這一點。
- [ ] CLI targeting、JSON actions、recovery、setup、lifecycle 或 destructive-operation
      行為改變時，檢查 Orbit agent skill。
- [ ] 可執行路徑改變時檢查 README 與 task guides；isolation 或 verification 改變時
      檢查 `testing.md` 與 test matrix。
- [ ] 搜尋「永遠」、「絕不」、「固定」、「唯一」以及 “always”、 “never”、
      “fixed”、 “only” 等絕對用語，確認新行為沒有推翻其假設。
- [ ] 只有在 invariant 穩定，且違反時會導致錯誤 command 或 safety decision，才新增
      documentation gate。不要 snapshot prose，也不要強迫翻譯逐字相同。
