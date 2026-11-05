# 整合

[English](./integrations.md) · [繁體中文](./integrations.zh-TW.md)

建立在 Orbit [JSON CLI contract](./agent-cli.zh-TW.md) 上的工具。

## Orbit for Orca

在 [Orca](https://www.onorca.dev) sidebar 加入唯讀的 Orbit 面板：instances、其
containers 與 services、SQL 同步狀態，並可在 terminal 分頁開啟 logs。

![Orca 右側 sidebar 中的 Orbit 面板](./assets/integrations/orca-orbit-panel.png)

安裝：在 Orca 開啟 Settings → Plugins → Add → "From a Git URL…"，輸入
`https://github.com/iml885203/orca-orbit`。需要 Orbit 0.16.10+ 與 Orca 1.4.247+。
詳細內容見 [plugin repository](https://github.com/iml885203/orca-orbit)。

## Orbit for Herdr app

在 [Herdr app](https://herdr.dev) 裡管理本機的 Orbit 環境：services、logs、
traces 與 SQL。

![Herdr app 中的 Orbit 視窗](./assets/integrations/herdr-app-orbit-panel.png)

安裝：把這段貼給 Herdr app 裡的 agent。

```text
Read ~/.config/herdr-app/AGENTS.md, then add this mod: https://github.com/iml885203/orbit/tree/main/integrations/herdr-app
```

agent 會先讀過每個檔案，把 `mods/orbit/` 複製到 `~/.config/herdr-app/mods/`，
並合併 `keys.json` 的 `cmd-o`；之後按 `cmd-o` 就能開啟視窗。需要 Orbit
0.17.0 以上。建置方式見 [RECIPE.md](https://github.com/iml885203/orbit/blob/main/integrations/herdr-app/RECIPE.md)。

更新：再貼一次安裝 prompt 會多裝一份，因為 Herdr 不會覆蓋已安裝的 mod。請改用
[RECIPE.md › Updating](https://github.com/iml885203/orbit/blob/main/integrations/herdr-app/RECIPE.md#updating)
裡的取代 prompt。
