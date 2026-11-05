# Orbit

Control your local Orbit environments from Herdr: services, logs, traces and SQL.

![Orbit](preview.png)

A control panel for the Orbit local dev environment in the Herdr app (`herdr-app`, the desktop app, not the Herdr terminal TUI) that you open over your work with `cmd-o`. It floats as a window covering 75% of the width and 80% of the height. Across the top you see every Orbit runtime on the machine (the default one and each `orbit --instance <name>`) as pills, with health chips next to them. Below are four tabs: the resource table, live logs, recent traces with a span waterfall, and SQL Server sync status. You can restart or stop a resource, stop a whole runtime, or send logs or a trace to a Claude session for diagnosis. The window can dock beside your terminal, become a tab, float again, or close.

## What you see

The mod is one pane with the theme's `t.chrome` background, `t.foreground` text, the Nerd Font "JetBrainsMono Nerd Font" if installed (else the settings font, else "JetBrains Mono"), 12.5px base size. Top to bottom:

1. **Header row** (padding 10/8, `t.surface`, 1px `t.border` bottom line):
   - Orbit glyph in `t.accent`, bold "Orbit" (13px).
   - **Instance pills**, one per runtime: 24px tall, fully rounded, 1px border. The first is `default`, then each named instance. The pill holds a 7px dot, the name, and a dim count `up/total` (resources not stopped / all), or `off` if the daemon is not running. The current pill has a border in its own color (cycle `t.ansi6, t.ansi5, t.ansi4, t.ansi3, t.ansi2, t.ansi1` by index), `t.raised` fill, semibold name. Tooltip: environment name and dashboard URL. Before the list loads, the environment name shows as plain dim text.
   - **Health chips** (18px, tinted, 10.5px semibold): `N/M healthy` in `t.ansi2`; `N starting` in `t.ansi3` and `N unhealthy` in `t.ansi1` only when nonzero. When the daemon is not running, a neutral dim chip `stopped` (or `checking...` while loading) appears before them. If an environment was selected with `orbit switch` but is not the running one: outline chip `selected env: <name> (not running)` in `t.ansi3`.
   - Right side: dim `updated HH:MM:SS` (or `refreshing`), the **Stop daemon** button (only while the daemon runs), and the window buttons.
2. **Tab row** (1px bottom line): a segmented control (`t.surface`, rounded 7) with **Resources, Logs, Traces, SQL**, each 24px tall with an icon and its number 1-4. The active tab is filled `t.solid` with `t.solid_foreground` text. Then a spacer, a **Dashboard** button (only while the daemon runs; tooltip shows the URL), a **Refresh** button with an `r` hotkey badge, and a 220px **agent-target field**: a text input with a terminal glyph, placeholder `new session`, and a dim hint on the right (`-> <address> · <label>` or `not found: new session`).
3. **Stopped banner** (only when the daemon is stopped): a rounded card with `Stopped — <env>` and "Start it with `orbit up` (or `orbit --instance <name> up`) in a terminal. This panel picks it up within a few seconds." Neutral colors, not an error.
4. A warning line (`t.ansi3`) for the latest message, and a red error row (`t.ansi1` on a 14%-tinted background, with Retry) when status cannot be read.
5. **Body** for the active tab:
   - **Resources**: a rounded card "Resources" with the total count, a header row (24px) and 26px rows. Columns: 12px state dot, NAME 150, KIND 78, STATE 104 (`state · phase` while starting), PORTS 170, UPTIME 70, RESTARTS 66 (`t.ansi3` if above 0), then URL in `t.accent`. Services sort first, then other kinds, by name. State colors: healthy/running/ready `t.ansi2`; starting, building, pending, probing, restarting, stopping `t.ansi3`; unhealthy, degraded, failed, error, crashed, exited, port_conflict, blocked `t.ansi1`; anything else `t.dim`. Selected row `t.selected`. Clicking a row opens a **side panel**, 400px wide, absolutely positioned over the right edge of the table (it does not narrow the table), `t.background` with a left border and large shadow. It shows kind, name, a state chip, a restarts chip, role/image/startup/health facts, the URL and sidecar URLs as `t.accent` links, and buttons Open `o`, Logs `l`, Restart `R`, Stop `S`.
   - **Logs**: a card "Logs · <resource>" with line count and updated time; a wrapped row of resource picker chips (22px, with a state dot); a tool row with a filter input (placeholder `filter lines  /`), **Following/Paused** (`f`), **Copy**, and primary **Ask agent** (`c`); then a virtualized list of 18px lines, 11.5px. A line with fail/error/crit/fatal/exception/unhandled/err is `t.ansi1`; warn/warning is `t.ansi3`.
   - **Traces**: card "Recent traces" with buttons **All traces / Errors only** (`e`) and **No health checks / With health checks** (`h`). Rows (26px): time, `ok` (`t.ansi2`) or `ERROR` (`t.ansi1`), root service (color hashed from the service name over `t.ansi4, t.ansi5, t.ansi6, t.ansi2, t.ansi3, t.ansi12, t.ansi13, t.ansi14`), root span name, duration, span count, services. Opening one shows a header card (Copy id, Ask agent `c`, Back `esc`, chips for status, duration, spans), a **Waterfall** card (22px rows: indented service and name in a 320px column, a 260px bar track with the span's offset and width, min 2px, error spans `t.ansi1`, duration, kind), and a **Logs with this trace id** card (last 400 lines).
   - **SQL**: card "SQL" with `N in sync` (`t.ansi2`) and `N changed` (`t.ansi3`) chips and a **Check** button. Rows: PROJECT 170, DATABASE 210, STATUS 110, PUBLISHED 100 (`2h ago`, or a dash), DETAIL. Status colors: in sync `t.ansi2`, changed `t.ansi3`, unknown `t.ansi1`, checking and not published `t.dim`.

Window buttons (24px tall, `t.raised`, each a Nerd Font icon then its text): while floating, **Split** (`\u{eb56}`), **Tab** (`\u{f2d2}`), and a 24px icon-only close (`\u{f00d}`). Once docked or a tab, a single **Float** button (`\u{f2d0}`) with a `cmd-enter` badge. The side panel's close is the same icon at 22px; **Stop daemon** carries `\u{f011}`.

## How it behaves

**Opening and placement.** `cmd-o` opens the floating window. **Split** (or `cmd-enter`) docks it to the right of the focused pane, 0.6 weight against 1 for the host, and closes the float. **Tab** (or `cmd-shift-t`) opens it as a tab named "Orbit" and selects it. **Float** (or `cmd-enter`) opens a new floating copy and closes the docked pane. `esc` closes the float when nothing else needs dismissing.

**Instances.** The pills come from the default runtime plus every named instance. Clicking a pill, or pressing `i` (cycles to the next), switches the whole mod to that runtime: resources, logs, traces, SQL, Dashboard link, Stop daemon, and the agent prompts all follow it. All state of the old instance is cleared at once and a forced refresh runs. The chosen instance is remembered.

**Keys** (ignored while an input has focus; `esc` returns focus to the panel):
- Everywhere: `1` `2` `3` `4` switch tab, `r` refresh, `i` next instance, `/` focus the log filter (Logs tab only).
- Resources: `j`/`down`, `k`/`up` move selection; `space` or `p` toggles the side panel; `o` or `enter` opens the resource URL; `l` jumps to its logs; `R` or `S` arm restart or stop.
- Logs: `f` follow, `c` ask agent, `[` and `]` previous/next resource.
- Traces: `e`, `h`, `c` as above; `j`/`k` move the cursor, `enter` or `o` opens the trace.
- `esc` order: leave an input, cancel an armed confirm, close the side panel, close the trace, clear the log filter, then close the floating window.

**Two-press confirmations.** Restart, Stop (resource) and Stop daemon first turn red and read "Confirm ..." (a red strip with Confirm and Cancel appears for resources). A second press of the same action on the same target within 6 seconds runs it; otherwise it disarms. Restart and stop need a running daemon. **Stop daemon** runs `orbit down`, then `orbit daemon stop` for the shown instance, then refreshes; the status line reports the result.

**Refresh and polling.** Every 3 seconds the shown instance's `orbit status --json` is read (one CLI process). The instance list (two CLI processes) is re-read every 15 seconds, on a forced refresh, and on every tick while the shown instance has no dashboard, which is how a start is noticed. While Logs is open and following, the log is re-read every 3 seconds (last 1000 lines, scrolled to the bottom when new output arrives). The trace list is re-read every 5 seconds while Traces is on the list view (limit 100); an open trace is not polled. SQL is checked when the tab opens, on refresh, and again if older than 60 seconds or the daemon state changed. Nothing polls while a docked copy's tab is not the active one; a floating window always counts as visible. Filter typing is debounced 200 ms.

**Daemon stopped.** `orbit status --json` reports `daemon.running: false`; while it stays stopped the status is re-read at most every 15 seconds. All resources show as `stopped`, the neutral "Stopped" banner appears, and the header shows the `stopped` chip. Logs and Traces show "Orbit is stopped, so there are no live logs/traces", SQL shows "Orbit is stopped, so SQL status is not checked." No logs, traces or SQL calls are made. Restart/Stop explain that they need the daemon. When the daemon comes back, the next tick picks it up.

**SQL.** Loading takes two steps. First `orbit sqlserver list` (one quick call) gives every database of the environment's SQL projects and Orbit's publish record for each, and the rows show at once with the last publish time. A database with no publish record shows `not published` and is not diffed. One with a record reads `checking` until its `orbit sqlserver diff` returns, then `in sync`, or `changed` with `N files changed since publish` or `+created ~altered -dropped`. At most 4 diffs run at once, and each row updates as its own diff finishes. An environment without SQL Server shows "This environment has no SQL Server workflow."

**Ask agent.** In Logs (`c`) it sends the visible (filtered) lines; in an open trace it sends the summary, waterfall, error span attributes and trace-id logs. Each section is clipped to 12000 characters. The text is written to `ask-<timestamp>.md` in the Herdr app data folder under `data/orbit/` (newest 20 kept) and the agent is told to read it; short single-line text is sent inline. The target is the name typed in the agent-target field, matched against terminal names, addresses, Claude session names and tab names (exact first, then case-insensitive), and must be running `claude`. Otherwise a new tab opens running `claude` with the text as its task, in the focused folder, and a toast says why. After sending, the Orbit mod docks beside that agent's pane and the float closes. The prompts tell the agent not to restart, stop or reconfigure resources unless asked.

**What persists** (Herdr shared state): under `orbit.ui` the instance, tab, selected resource, log resource, side panel open, follow, errors-only and health-check toggles; under `orbit.agentTarget` the typed agent name. Unreadable saved state falls back to defaults with a message.

## Built from

Files in `mods/orbit/`, entry `main.js` (a `View` subclass wrapping `Panel`, `close()` forwards to it). Imports `gpui-kit`, `gpui-base`, `herdr`.

`mod.json`:

```json
{"place":"none","size":420,"title":"Orbit","focusable":true,"overlay":{"kind":"window","key":"orbit-panel","width":0.75,"height":0.8}}
```

`keys.json`: `{ "global": { "cmd-o": "mod.open:orbit" } }`

Modules: `panel.js` (state, timers, keys, rendering root), `actions.js` (loads, confirmations, asks), `api.js` (the `orbit` CLI), `model.js` (tones, line filters, waterfall layout), `target.js` (agent resolution and delivery), `ask.js` (prompt text and snapshot files), `dock.js` (split/tab/float), `ui-state.js` (shared state), `views/{shell,widgets,resources,logs,traces,sql}.js`.

Data sources. Every `orbit` call runs with `cwd` = `home()` so a project's `orbit.yaml` is never picked up, and carries `--instance <name>` (named instances) and `-c <config path>` (both kinds). If `orbit` is not on PATH it retries `~/.local/bin/orbit`.
- `orbit inspect --json` (default runtime: selected env, daemon running, dashboard URL, config path) and `orbit instance list --json` (named instances: name, environment, state, dashboard, config path, resource states).
- `orbit status --json`: resources (with `image` and `sidecars`), and `daemon.running`, `daemon.context` and `daemon.config_path` for the environment name. `daemon.running` decides whether the panel is live.
- `orbit logs <name> --lines 1000 --json` (`data.lines`); `logs_unavailable` shows as an empty log.
- `orbit trace --limit 100 [--include-health] --json` (`data.traces`), and `orbit trace <id> --logs --json` for an open trace: the trace's fields plus `stdout_lines` (`{service, line}`); a `stdout_lines_unavailable` notice shows as one note line.
- `orbit restart <name> --timeout 25s --json` and `orbit down <name> --timeout 25s --json` for Restart and Stop. Both return when Orbit has verified the result, so the "Restarting..." state lasts until then. Herdr's `exec` stops a program after 30 seconds, so a slower restart returns `timeout` and the mod says it is still in progress; the status picks up the result. The message names what Orbit reports, including dependents it stopped too.
- `orbit sqlserver list --json` (`projects` and `publish_records`, each record's `last_publish.at`) and `orbit sqlserver diff <db> --json`; `not_configured` means no SQL workflow.
- `orbit down --json` then `orbit daemon stop --json`.
- CLI responses are `{ok, data}` or `{ok:false, error:{code,message,hint}}`.

Herdr functions: `theme`, `settings`, `fonts`, `props`, `nextCommand`, `closeOverlay`, `spaces`, `panes`, `focused`, `terminals`, `run` (`pane.split` with `mod`, `layout.resize`, `pane.focus`, `tab.new` with `mod` or `argv: ["claude"]` and `task`, `tab.select`, `mod.open`, `pane.close`), `hc` (`get <addr> --json`, `send <addr> <text>`), `notify`, `shared`/`setShared`, `readTextFile`, `writeTextFile`, `appHome`, `home`, `exec`; `cx.timer.every/after`, `cx.spawn`, `cx.sleep`, `cx.open_url`, `cx.write_to_clipboard`. The agent session name is read from `~/.claude/sessions/<pid>.json`.

Widgets: `InputState`/`Input`, `VirtualListScrollHandle`, `v_virtual_list` plus `Scrollbar.vertical` for the logs, `overflow_y_scrollbar` for tables.

Art: Nerd Font glyphs only (`\u{f1b3}` orbit, `\u{f233}` resources, `\u{f15c}` logs, `\u{f0e8}` traces, `\u{f1c0}` sql, `\u{f120}` agent, `\u{f06a}` error, `\u{f071}` warning, `\u{f0b0}` filter, `\u{f08e}` link, `\u{eb56}` split, `\u{f2d2}` tab, `\u{f2d0}` float, `\u{f00d}` close, `\u{f011}` power). Icons go in the Nerd Font (JetBrainsMono Nerd Font when `fonts()` has it); never Unicode symbols. No images.

## Watch out

- **Floating copies must not loop on `nextCommand`.** A floating window is a new copy each time; a command loop keeps every closed copy alive in the QuickJS heap. Only docked panes and tabs run `cx.spawn(commandLoop)`, which handles `focus`, `escape`, `dock`, `float`, `tab`, `refresh` and the tab names. A float focuses itself with a 300 ms timer.
- **No `cx.timer` inside `cx.spawn`.** Timers are created in `init` or event handlers only. Inside a spawned task use `cx.sleep` (the log re-scroll waits 60 ms after layout this way). The one-shot helper `later()` is not called from tasks.
- **Cancel and release in `close()`.** Mark the copy closed, cancel every timer entry, drop the held state, and `release()` the scroll handle, both inputs and the focus handle. Every async result checks the closed flag before writing.
- **Generation guard.** Each instance switch bumps a counter; every load captures it and discards its result if it changed (A to B to A included). Two overlapping loads of the same kind are blocked by `loading` flags.
- **Always target the shown instance.** Forgetting `--instance` or `-c` makes `orbit` act on the default runtime. Never run from a project directory.
- **Stop daemon needs `down` first.** `daemon stop` alone leaves containers running with no supervisor.
- **Orbit version.** The mod needs Orbit 0.17.0 or later: `orbit status --json` carries `image` and `sidecars`, and `orbit trace <id> --logs --json` carries `stdout_lines`, from that release. On an older Orbit those fields are simply absent (no image, no sidecar links, no trace stdout lines). The SQL tab needs `publish_records` (0.16.10 or later); older versions show `unknown: Needs Orbit 0.16.10 or later` for each database. Right after an upgrade, a runtime still running the old daemon answers `orbit_update_pending` until `orbit daemon restart`; the SQL error row shows that message. A database with no publish record is not diffed because the diff would run a full SqlPackage comparison (seconds each).
- **Only the CLI.** The mod reads Orbit through `orbit ... --json`, which is Orbit's compatibility contract. Do not call the dashboard's `/api` routes: they are internal to Orbit and change without notice. If one status read fails while the daemon is up, the live view stays and shows the error; if status fails while it is stopped, the last resources show as stopped with the error. A daemon serving another environment (`daemon.context_mismatch`) is not treated as live.
- **Docked visibility.** A docked copy is considered visible only if its tab is the active one in the active space; otherwise it stops polling.
- **Side panel is absolute.** It overlays the table; the table's `relative()` parent is required.
- **Trust.** The mod runs programs (`orbit`, `hc`), writes files (`ask-*.md` under the Herdr app folder), reads `~/.claude/sessions/*.json`, and opens URLs in the browser. It makes no network requests of its own; the Dashboard button only opens the instance's local dashboard URL in the browser. Tell the person this before installing.
- **Float/dock handoff.** Split, Tab and Float close the copy they were pressed in once the new one appears; show any failure message only if that copy still exists.
- Controls that do nothing: `/` and `f` and `c` outside Logs, the side panel for a missing row ("Select a row"), Restart/Stop while the daemon is stopped.

## Done when

- `herdr-app check` passes for `mods/orbit`, and `cmd-o` opens a floating window about 75% wide and 80% tall with the header, tab row and Resources table.
- The pills list `default` plus every name from `orbit instance list`, each with `up/total` or `off`; hovering shows the environment and dashboard.
- Clicking a named pill (or pressing `i`) changes the table, health chips, Dashboard link and Logs to that instance's, and the choice survives closing and reopening.
- Clicking a resource row opens the 400px side panel over the table; `j`/`k` move the selection; `esc` closes it.
- `R` on a running resource turns the button red; a second press within 6 seconds runs `orbit restart <name> --json`; waiting out 6 seconds disarms it.
- Logs shows the picked resource's last lines, errors in `t.ansi1`, filter narrows them, `f` pauses polling, `[` and `]` change resource.
- Traces lists recent traces; opening one shows a waterfall and trace-id logs; `e` and `h` toggle the filters.
- SQL lists every database at once with its last publish time; published ones read `checking`, then turn in sync or changed one by one; never-published ones read not published; or the no-workflow message.
- Pressing Stop daemon twice runs `orbit down` then `orbit daemon stop`; afterwards the neutral `Stopped — <env>` banner shows, the resources read `stopped`, Logs/Traces/SQL show their stopped messages, and no logs, traces or SQL calls happen (check with a process listing).
- Running `orbit up` again brings it back within a few seconds without touching the panel.
- Ask agent with a running Claude's name in the target field sends a one-line pointer to an `ask-*.md` file in the Herdr app data folder; with an empty or unknown name it opens a new `claude` tab and says so.
- Split docks beside the focused pane, Tab opens an "Orbit" tab, Float returns to the window, and closing and reopening repeatedly does not leave extra command loops or timers behind.

## Updating

Herdr never overwrites a mod that is already installed: asking an agent to add this folder again installs a second copy under a new name. To update, ask for a replacement instead:

```text
Read ~/.config/herdr-app/AGENTS.md, then update my orbit mod: replace ~/.config/herdr-app/mods/orbit with the files from https://github.com/iml885203/orbit/tree/main/integrations/herdr-app/mods/orbit and run herdr-app check on it.
```

`keys.json` rarely changes; merge it again only if the commit history of `integrations/herdr-app/keys.json` shows a change. Saved state (`orbit.ui`, `orbit.agentTarget`) is kept. If the mod was renamed on install (for example `orbit-2`), name that folder instead.
