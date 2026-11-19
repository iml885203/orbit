# Extensions

[English](./extensions.md) · [繁體中文](./extensions.zh-TW.md)

Enable the workflows your project needs in its environment YAML. Official Orbit
extensions ship in the CLI; there is no separate runtime plugin to install.

- **[SQL Server](./sql-workflow.md)** — build and publish a SQL Database Project
  to your development database.
- **[BigQuery](./bigquery.md)** — inspect shared datasets and prepare isolated
  fixture data for backend tests. Continue to [testing and recovery](./bigquery-testing.md)
  when planning CI or handling an interrupted run.
- **[Callback Tunnel](./tunnel-claim.md)** — receive staging callbacks in your
  local environment.

Start with the relevant guide for prerequisites, configuration and the expected
result. Keep shared environment YAML in Git so the team reviews changes together.

For third-party tools that connect to Orbit, see [Integrations](./integrations.md).
For extension implementation details, see [Architecture](./architecture.md).
