# 版本與相容性

Orbit 與 repository 內的 `plugins/orbit` plugin 共用同一個
[語義化版本](https://semver.org/lang/zh-TW/)。

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
- Release tag 不可修改；修正必須發布成新版本。

## Preview 批次原則

`main` 上的 commit 都是尚未發布的工作，可以累積多個彼此相關的修正。只有當
整個批次交付一個完整、能以 installed-user journey 描述並驗證的使用者成果時，
才切出 preview。單一 commit 通過測試或只修好一個 edge case，本身不構成發布
邊界。

Preview batch 依照下列順序 freeze：

1. 完成相關 implementation，以及實務上最強的 journey 驗證。
2. 一次 review 相較於前一版的完整使用者差異。
3. 決定下一個版本、更新兩份 plugin manifest、把 `cmd/orbit/extensions.go`
   的 `EnvRepoRef` 指向即將建立的 demo tag（讓 `orbit init` 內建與本版配對
   的 demo），並準備配對的 demo tag；此時先不要建立 Orbit tag。Demo tag 內
   的 `.orbit-release.json` 會記錄 release version 與 demo journey 實際
   build 的 Orbit candidate commit。
4. 準備並 review 對使用者說明的 release notes。
5. 執行 candidate 與 platform gates，再手動 approve 發布。Release workflow
   只會在所有 gate 通過後建立不可修改的 Orbit tag，失敗的 candidate 不會留下
   release tag。

Release notes 在 approve release workflow 時輸入，保留於 GitHub Releases，
不再累積在 source tree。內容先描述整個 batch 的使用者成果，個別修正則作為
支援細節。

Package updates 使用 private `iml885203-package-sync` GitHub App。只將 App
安裝到 `iml885203/homebrew-tap` 與 `iml885203/scoop-bucket`，並授予
`Actions: Read and write` 與 `Contents: Read`。將 client ID 設為
`PACKAGE_SYNC_APP_CLIENT_ID` repository variable，private key 設為
`PACKAGE_SYNC_APP_PRIVATE_KEY` repository secret。

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

每次發布都必須把以下兩份 plugin manifest 更新成 Orbit release 版本：

- `plugins/orbit/.codex-plugin/plugin.json`
- `plugins/orbit/.claude-plugin/plugin.json`

Plugin 只能使用同版 Orbit 已提供的 command 與 contract。任何一份 manifest
與 Orbit tag 不一致時，該 release 就尚未完成。
