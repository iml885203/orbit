# 版本與相容性

Orbit CLI 使用[語義化版本](https://semver.org/lang/zh-TW/)，candidate version
記錄在 repository root 的 `VERSION`。`plugins/orbit` plugin 獨立編版，讓只修改
skill 的更新不必連帶發布 CLI。

## 發布順序

- GitHub private 演練版從 `v0.0.1` 開始。
- Source repository 已在 1.0 產品打磨期間先行公開；所有 `0.x` release
  都是 preview，可能包含 breaking changes。
- GitHub 上的 `0.x` title 統一為 `Orbit vX.Y.Z (Preview)`。它仍是 repository
  可安裝的 latest release，讓預設 installer 與 `orbit update` 永遠取得最新
  支援的 preview，不要求使用者理解或選擇 release channel。
- 第一個 stable release 是 `v1.0.0`。發布這個 tag 代表下列相容性契約
  已有文件與測試，並已準備好提供外部使用者使用。
  [1.0 test matrix](https://orbit.dotw.me/docs/1.0-test-matrix) 記錄該 tag 所需的平台證據，
  以及目前實際已驗證的項目。
- 已發布的 GitHub Release 不可修改；其 tag 與 assets 都不能變更或重用。
  修正必須發布成新版本。

## Preview 批次原則

`main` 上的 commit 都是尚未發布的工作，可以累積多個彼此相關的修正。只有當
整個批次交付一個完整、能以 installed-user journey 描述並驗證的使用者成果時，
才切出 preview。單一 commit 通過測試或只修好一個 edge case，本身不構成發布
邊界。

Preview batch 依照下列順序 freeze：

1. 完成相關 implementation，以及實務上最強的 journey 驗證。
2. 一次 review 相較於前一版的完整使用者差異。
3. 決定下一個版本並更新 `VERSION`；此時先不要建立 Orbit tag。
4. 準備並 review 對使用者說明的 release notes。
5. 執行 candidate 與 platform gates，再手動 approve 發布。Release workflow
   只會在所有 gate 通過後建立 Orbit tag，失敗的 candidate 不會留下 release
   tag。GitHub 會在 release 發布時鎖定 tag 與完整 asset set，並產生涵蓋其 digest
   與 target commit 的 immutable-release attestation。
6. Workflow 會先驗證 release attestation 與每個發布 asset，才允許更新 package
   repository。Homebrew 與 Scoop 也會在取得寫入權限前重複相同的唯讀驗證。

Release notes 在 approve release workflow 時輸入，保留於 GitHub Releases，
不再累積在 source tree。內容先描述整個 batch 的使用者成果，個別修正則作為
支援細節。

Package updates 使用 private `iml885203-package-sync` GitHub App。只將 App
安裝到 `iml885203/homebrew-tap` 與 `iml885203/scoop-bucket`，並授予
`Actions: Read and write` 與 `Contents: Read`。將 client ID 設為
`PACKAGE_SYNC_APP_CLIENT_ID` repository variable，private key 設為
`PACKAGE_SYNC_APP_PRIVATE_KEY` repository secret。

Immutable Releases 是由 repository owner 管理的發布前提。Workflow 不持有
repository administration credential；發布後若 GitHub 沒有回報 immutable 且
已 attested 的 release，流程會 fail closed 並阻止 package promotion，但不能把
已發布的 mutable release 追溯改成 immutable。Build provenance 與 SBOM
attestation 是 binary 如何產生的獨立證據；immutable-release attestation 則綁定
已發布的 tag、target commit 與 release assets。

1.0 前的 release 之間可以有 breaking change。從 `v1.0.0` 起：

- PATCH：向後相容的修正；
- MINOR：向後相容的新功能；
- MAJOR：可能不相容地修改穩定契約。

## v1 起的穩定契約

以下介面屬於相容性契約：

- CLI command 名稱、flags、exit 行為與文件化操作流程；
- Agent 使用的 JSON envelope 與具名 schema version；
- Environment YAML schema 與 validation 行為；
- 儲存於本機的 user settings；
- Daemon HTTP API；
- Public extension API。

MINOR release 可以加入 additive change。JSON 與 HTTP consumer 必須忽略
未知欄位。移除或重新命名欄位、command、flag、設定 key 或 public Go symbol，
必須建立新的具名 schema version，或發布新的 MAJOR Orbit 版本。

未文件化的 internal implementation、dashboard markup 與 styling、log 文字及
test fixtures 不屬於相容性契約。

## Plugin 版本

Plugin 使用 `YEAR.MONTH.N`，其中 `N` 是該月第幾次 plugin release；例如
`2026.8.1` 代表 2026 年 8 月第一版。每次 plugin 發布都要一起更新：

- `plugins/orbit/.codex-plugin/plugin.json`
- `plugins/orbit/.claude-plugin/plugin.json`

兩份 manifest 必須彼此一致，但不需要等於 Orbit CLI version。只有 plugin
內容變更時才增加 plugin version。Plugin 必須偵測已安裝的 CLI，不能教導其
支援版本尚未提供的 command 或 contract。
