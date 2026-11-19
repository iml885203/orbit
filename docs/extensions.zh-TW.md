# 擴充功能

[English](./extensions.md) · [繁體中文](./extensions.zh-TW.md)

在環境 YAML 啟用專案需要的工作流程。Orbit 官方 extensions 隨 CLI 一起提供，
不需另外安裝 runtime plugin。

- **[SQL Server](./sql-workflow.zh-TW.md)**：編譯 SQL Database Project，將 schema
  發布到開發資料庫。
- **[BigQuery](./bigquery.zh-TW.md)**：檢查共用 dataset，替後端測試準備隔離的 fixture
  資料。設計 CI 或處理中斷時，再讀[測試與復原](./bigquery-testing.zh-TW.md)。
- **[Callback Tunnel](./tunnel-claim.zh-TW.md)**：在本地環境接收 staging callbacks。

從對應指南確認前置條件、設定與預期結果。共用環境 YAML 放進 Git，讓團隊一起審查修改。

連接 Orbit 的第三方工具見[整合](./integrations.zh-TW.md)；extension 實作方式見[架構](./architecture.zh-TW.md)。
