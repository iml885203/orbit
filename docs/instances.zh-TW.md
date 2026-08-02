# 隔離的 runtime instances

[English](./instances.md) · [繁體中文](./instances.zh-TW.md)

Orbit 的 default runtime 刻意保持向後相容：使用宣告的 host ports、預設 daemon
socket 與 dashboard address，以及舊有 Docker resource names。同一台機器上的多個
checkout、agent 或 CI job 需要獨立執行時，請使用 named instance。

## 使用 named instance

`--instance <name>` 會指定一個隔離 runtime，可套用於 lifecycle 與 diagnostic
commands：

```bash
orbit up --instance checkout-a --json
orbit status --instance checkout-a --json
orbit logs app --instance checkout-a --json
```

整個 workflow 都應維持相同的 instance target。JSON response 會包含 `instance`
field，recommended actions 也會保留該 target。

列出 named instances，即可取得 environment、state、dashboard 與解析後的 resource
endpoints：

```bash
orbit instance list --json
```

## 隔離與 ports

每個 named instance 都擁有 `~/.orbit/instances/` 下的獨立目錄，其中包含 daemon
socket、log、state、ownership record 與解析後的 ports；同時也擁有獨立的 Docker
namespace、network、containers 與 volumes。

Default runtime 會把宣告的 host ports 視為固定值。Named instance 則把它們視為
preferences：必要時 Orbit 會選擇可用 port、持久化結果以維持 restart 穩定、將解析
後的 port 注入 host services，並透過 `up`、`status` 與 `instance list` 回報實際
endpoints。

Caller 應使用這些回報的 endpoints，不要假設宣告 port，也不必自行協調
`ORBIT_HOME`、`ORBIT_NAMESPACE`、`ORBIT_DASHBOARD_PORT` 與 `ORBIT_SOCKET`。

## 清理

```bash
orbit instance clean checkout-a --json
```

Cleanup 會停止該 instance，並移除其 daemon state 與 Docker resources。這會刪除
該 instance 的本機 processes 與資料，但 ownership labels 會保護其他 named
instances 與 default runtime。只在確定不再需要該 instance 時執行。

底層 runtime environment variables 仍保留給相容性與 Orbit 隔離內部的測試，
不再是啟動第二個 stack 的一般方式；詳見
[設定](configuration.zh-TW.md#底層-runtime-覆寫)。
