# BigQuery 開發與整合測試

[English](./bigquery.md) · [繁體中文](./bigquery.zh-TW.md)

Orbit 透過編譯進 binary 的 BigQuery extension 管理開發 profile 與隔離測試
dataset，使用 Google 官方 Go REST SDK。這不是本地 BQ emulator、SQL proxy、
query editor 或 migration 工具。SQL Server 與 BQ 都走內部 extension 接點；
agent skill/plugin 是操作指引的交付方式，不是資料庫功能的 runtime plugin。

## 啟用與 YAML 相容性

使用[完整範例](https://github.com/iml885203/orbit/blob/main/docs/examples/bigquery/orbit.yaml)，在 schema `version: "3"` 加上
選用的頂層 `bigquery:`。沒有這個 section 的環境不需要修改，也不會要求 GCP
憑證或連線。使用此 section 需要 **Orbit v1.2.0 以上**；舊版會拒絕未知 section，
不會默默略過。先升級團隊與 CI 的 CLI，再透過環境 repo 的 Git review 合併設定。
撤掉此 section 可回到舊 v3 binary 相容範圍，但應先處置 dataset、保留復原紀錄。
Schema version 描述共用格式，不代表每個版本 binary 都具有相同的 feature。

`identity`、profile 欄位、schema/fixture 格式與路徑限制見[設定參考](./configuration.zh-TW.md#bigquery)。

Orbit 依目前選定環境的 section 判斷「有設定 BQ」，這不代表已驗證登入、IAM、
SQL、網路、計費或 app readiness。Dashboard、doctor、resource snapshot 都只讀本地
設定與紀錄，不取得 ADC、不連 GCP。BQ 不加入啟動依賴圖，`orbit up` 不會自動準備資料。

## 認證與權限

只有明確執行雲端操作的 `orbit bq` CLI 程序使用自己的 ADC，包含 SDK 支援的
external-account/WIF ADC 設定。Orbit 不改 gcloud 全域 project、不授權角色、
不把憑證放進 YAML/receipt，也不沿用 daemon 身分。本地 user ADC 可在管理者指引下
使用 `gcloud auth application-default login`；只有 `gcloud auth login` 不等同 ADC。
CI 使用該平台的 WIF 設定與暫時 ADC 設定檔。

Shared inspect 需要 dataset metadata 讀取權限。Sandbox prepare 需要 data project
的 dataset 建立、新 dataset 中的 table 建立/寫入，以及 job project 的
`bigquery.jobs.create`。復原需要 job metadata 讀取；clean 需要 dataset get/delete
與 table 刪除權限。請限定開發 project 並採最小權限。應用程式有自己的 ADC/IAM；
操作者成功不代表 app 有權限。本功能的離線測試沒有用真實 GCP project 驗證。

## 準備、啟動 app、清理

在專案內執行，或用 `--config`、`--instance` 固定操作對象：

```sh
orbit --config docs/examples/bigquery/orbit.yaml bq inspect shared --json
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox prepare integration --run pr-270-a --timeout 2m --json
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox inspect integration --run pr-270-a --json
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox list --json
```

`inspect shared` 讀取雲端 metadata 並檢查 location，不執行 SQL，也不驗證 job 建立。
`sandbox inspect/list` 只讀本地 receipt，必須同時看觀測時間與 `sandbox_data_ready`，
不能把指令 exit success 當成雲端或應用程式驗證成功。

Prepare 建立隨機且永不重用名稱的 dataset、table，以及 `WRITE_EMPTY` load jobs。
操作前先保存 intent 與固定 job ID，帶 deadline 輪詢，區分 fatal error 與 warnings。
Pending/unknown 不算 ready；中斷後使用**相同 profile 與 run key**恢復原操作。
相同 key 若 profile/schema/fixture 內容改變會報 conflict。新 key 前先清理或明確記錄
前次資源處置；已 cleaned 的 key 不會配置另一個 dataset。

從 JSON envelope 的 `data.app_config` 以 JSON parser 讀取變數映射，例如
`ORDERS_DATASET=your-test-project.orbit_test_<random>`。Orbit 不會自動注入正在執行的
app。透過 app 原本設定機制帶入值，再用 app 自己的憑證啟動後端、呼叫真實 public API
驗證結果；不要 `eval` 未信任文字。`app_runtime` 一律維持 `unverified`：seed 成功
不等於 E2E 通過。範例 fixture 的業務斷言可為 A=300、B=50，但必須透過真正 app/SQL
取得，不是由 Orbit 的 fake query engine 宣稱成功。

Harness 必須在 **prepare 開始前**安裝 `finally`／CI `always` teardown。即使 prepare 或 assertion 失敗，仍停止自己啟動的 app、保留 canonical evidence；只有已授權且前提成立時才嘗試 clean。Job／generation 不明時保留資源走 recovery，不要換 run key 隱藏問題。SIGKILL 或 runner 消失無法保證 teardown 執行。

停止 app，等待**所有 app 與外部 job**結束，確認 writer 權限受限、沒有其他 actor
可替換 dataset 或重用名稱，再明確確認遞迴刪除雲端資料：

```sh
orbit --config docs/examples/bigquery/orbit.yaml bq sandbox clean integration \
  --run pr-270-a --yes --app-stopped --restricted-writers --json
```

這些 flag 是營運前提的聲明，不是 lock 或 IAM enforcement。Clean 會核對 canonical
owner/runtime 世代、不可變 target、**先前已觀測**的 dataset creation time/labels/location，
以及已提交 jobs。確定從未開始提交的 job 可略過；提交結果不確定或 job 狀態未知時拒絕。
`down`、環境切換、`instance clean` 都不會隱含刪除 BQ dataset。
Table 預設一天 expiration，但不是 dataset TTL、清理完成證明或費用上限。

## 復原與人工處置

Canonical 紀錄保存在 `<instance-base-home>/bigquery/<context-hash>/`，位於可刪除的
instance home 外。Profile+run 是穩定本地查找 key；設定與輸入 hashes 是不可變屬性，
不會用不同 hash 偷建另一個資源。同 canonical directory 的程序以 store UUID 與
OS lock 協調；不同電腦/store 的相同 run key 可建立不同 dataset，沒有全域去重或分散式鎖。

Instance home 內的 generation marker 防止同名重建繼承舊權威。`instance clean`
包含缺 manifest 的殘留清理，都保留外部 canonical 紀錄。List 仍顯示舊紀錄，但新 instance
不能自動修改舊資源。不要用複製 store 方式授權另一個 runner；receipt 副本只供診斷。
整個 store 的複製連 UUID 都會複製，對 clone 執行 mutation 不受支援。
解除安裝並 purge base home 前必須封存紀錄。

如果缺少 generation 證據、job 無法判定、target 已改變或 instance 被移除，導致自動
清理被拒絕：

1. 保留 canonical JSON、job ID 與 dataset 全名，不要改 receipt 繞過檢查。
2. 由有權限的雲端操作者確認精確 project/location/dataset，對帳紀錄中的 jobs 與
   app jobs，停止所有 writer。不要換另一個身分期待 Orbit 自動接管。
3. 用 GCP 工具等待 job terminal，必要時取消並等結束；確認 dataset 的預期 owner。
   若不確定，保留資源並交由 owner 處理。
4. 接受遞迴刪除與 replacement 風險後，由 GCP 工具明確刪除已確認的 dataset，將處置
   結果與 receipt 一起保存在 test run/issue。本地可能仍是 `cleanup_intent` 或 not-ready，
   不能當成真實雲端狀態，也不要為了讓畫面乾淨就直接抹除證據。

BQ 文件中的 dataset delete API 沒有 generation-match 前置條件；GET 再 DELETE
無法阻擋兩者之間的外部 replacement。Labels/creation time 是觀測證據，不是授權或
conditional delete。必須確保 restricted writers 與不重用名稱；無法成立時走具備適當
雲端控制流程的人工處置。

## 測試策略與選用 live smoke

離線 CI 以官方 SDK 連到無憑證 loopback HTTP fixture server，驗證 API target/location、
遺失回應的復原、schema/fixture、job diagnostics、OS/multiprocess locks、損毀紀錄、
獨立 store 及清理拒絕條件。不實作 GoogleSQL，不證明 ADC/WIF、IAM、配額、費用或 app 行為。

後端 unit test mock 自己的 BQ 邊界；有 test project 時，整合/E2E 使用真實隔離 dataset。
Emulator 可用於其確實支援的窄範圍案例，但 Orbit 不安裝它，也不把 emulator 通過當 BQ
相容性證明。不使用 BQ 的 Orbit 開發流程與離線 CI 不需要 GCP project。

選用 live smoke 需先備妥專用 project、明確 location、operator/app ADC 與權限。
依上面的 shared inspect → sandbox prepare → 真正 GoogleSQL/app assertion → verified
cleanup 流程執行。條件不足時回報 **SKIP — no real GCP test project/credentials**。
此 smoke 是明確由操作者執行的驗證，不會在預設 CI 偷跑，也不可由離線測試代替宣稱通過。

參考：[ADC](https://cloud.google.com/docs/authentication/application-default-credentials)、
[WIF](https://cloud.google.com/iam/docs/workload-identity-federation)、
[dataset delete](https://cloud.google.com/bigquery/docs/reference/rest/v2/datasets/delete)、
[JobStatus](https://cloud.google.com/bigquery/docs/reference/rest/v2/Job#JobStatus)、
[費用管理](https://cloud.google.com/bigquery/docs/best-practices-costs)。

唯讀 live smoke 入口：設定 `ORBIT_BQ_LIVE=1`、`ORBIT_BQ_LIVE_CONFIG`（明確 YAML 路徑）、`ORBIT_BQ_LIVE_PROFILE`（shared profile），執行 `go test ./internal/bigquery -run TestLiveSharedMetadata -v`。這只驗證 metadata，不能取代完整 app journey。

已觀測 table 世代會持久保存；過期、消失或替換時，同 run 不重建或重新 seed。已寫入 submission intent 的 job 若回傳 404，也不會自動重送：可能是 metadata 保留期或不確定的提交結果，須保留證據走人工處置。只有確定從未開始提交的 planned job 可首次送出。
