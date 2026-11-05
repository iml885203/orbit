# Integrations

[English](./integrations.md) · [繁體中文](./integrations.zh-TW.md)

Tools built on Orbit's [JSON CLI contract](./agent-cli.md).

## Orbit for Orca

A read-only Orbit panel in [Orca](https://www.onorca.dev)'s sidebar: instances,
their containers and services, SQL sync status, and logs in a terminal tab.

![The Orbit panel in Orca's right sidebar](./assets/integrations/orca-orbit-panel.png)

Install: in Orca, open Settings → Plugins → Add → "From a Git URL…" and enter
`https://github.com/iml885203/orca-orbit`. Needs Orbit 0.16.10+ and Orca
1.4.247+. Details are in the [plugin repository](https://github.com/iml885203/orca-orbit).

## Orbit for Herdr app

Control your local Orbit environments from the [Herdr app](https://herdr.dev):
services, logs, traces and SQL.

![The Orbit window in the Herdr app](./assets/integrations/herdr-app-orbit-panel.png)

Install: paste this into the agent running in the Herdr app.

```text
Read ~/.config/herdr-app/AGENTS.md, then add this mod: https://github.com/iml885203/orbit/tree/main/integrations/herdr-app
```

The agent reads every file first, copies `mods/orbit/` into
`~/.config/herdr-app/mods/` and merges `cmd-o` from `keys.json`; `cmd-o` then
opens the window. Needs Orbit 0.17.0 or later. How it is built is in
[RECIPE.md](https://github.com/iml885203/orbit/blob/main/integrations/herdr-app/RECIPE.md).

Update: running the install prompt again adds a second copy, because Herdr
never overwrites an installed mod. Use the replace prompt in
[RECIPE.md › Updating](https://github.com/iml885203/orbit/blob/main/integrations/herdr-app/RECIPE.md#updating)
instead.
