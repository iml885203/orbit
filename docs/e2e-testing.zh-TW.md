# 在 E2E 測試底下使用 Orbit

[English](./e2e-testing.md) · [繁體中文](./e2e-testing.zh-TW.md)

Orbit 在 E2E 架構裡的職責刻意收得很小：提供共用的基礎設施，並且誠實回答關於
它的問題。測試 harness 把所有測試專屬的東西疊在上面，永遠不重新配置整台機器。

## 疊加模式

把整個 stack 分成三層，各有各的擁有者：

| 層 | 擁有者 | 生命週期 |
|---|---|---|
| 基礎設施（database、cache、broker） | Orbit（`orbit up --infra`） | 長駐，與日常開發共用 |
| 測試資料（專屬的 database 或 schema） | 測試 harness | 可拋棄，scenario 之間重置 |
| 受測系統（app runtime） | 測試 harness | 每次執行以測試 profile 啟動 |

開發者平常跑的基礎設施就是測試套件用的基礎設施。套件不自己起一份副本；它在
共用的伺服器上建自己的 database、讓自己的 app runtime 指過去，並透過應用程式
清理資料（app 內建的測試專用 reset endpoint，比任何基礎設施層的還原都快上
幾個數量級）。

## 驗證基底——永遠不要切換它

`orbit switch` 會停掉運行中的資源並重啟 daemon。它是機器層級的 provisioning
步驟：放在 pipeline 的 setup 階段或由人親手輸入是對的，作為跑測試的隱式副作
用是錯的。Orbit 會強制這件事——有資源運行時，`switch` 會要求確認，非互動的
caller 必須加 `--yes`。

harness 應該做的是開跑前先*驗證*：

1. 讀 `orbit env info --json`。它回報 env 的身分，以及每個資源帶出處的 port
   與 URL——`declared` 來自 environment 檔、`observed` 來自運行中的 daemon。
   daemon 服務的是別的 environment 時不給 observed 值，harness 永遠不會把別
   的 stack 的 port 誤認成自己的。含憑證的 environment 值需要
   `--show-secrets` 才輸出。
2. 確認套件需要的資源存在且 healthy（完整生命週期細節在
   `orbit status --json`）。
3. 不滿足時快速失敗，並給出可執行的訊息——列出缺少的資源，印出人類（或
   pipeline 的 provisioning 階段）該執行的 `orbit switch <env>` / `orbit up`
   指令。不要自己執行它。

失敗時 `orbit up --json` 自帶證據：`data.failed_resources` 列出每個沒有變
healthy 的資源，含狀態、原因與有上限的 log tail。CI log 不需要第二個指令就能
看到解釋。payload 形狀見 [agent CLI contract](agent-cli.zh-TW.md)。

## 在同一台機器跑第二個 stack

專屬 runner——或想小心地在 dev 環境旁邊跑第二個 stack——可以用三個環境變數
完全隔離，這正是 Orbit 自己的測試套件用的機制：

```bash
export ORBIT_HOME=/tmp/orbit-ci        # socket、狀態、env 選取
export ORBIT_NAMESPACE=ci-$RANDOM      # container 名稱 + label
export ORBIT_DASHBOARD_PORT=24500      # dashboard listener
```

帶著這些變數執行的每個指令都操作一個完全獨立的 Orbit：自己的 daemon、自己的
container、自己的狀態。一個限制：host port 仍然是 env 檔裡靜態宣告的，所以
*同一個* env 的兩個 stack 會在 port 上相撞——給第二個 stack 一份 host port
不同的 env 檔。
