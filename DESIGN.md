---
version: alpha
name: Orbit
description: Design system guidance for Orbit's local-dev dashboard and agent-built UI.
colors:
  primary: "#58a6ff"
  secondary: "#8b949e"
  tertiary: "#39c5cf"
  neutral: "#161b22"
  background: "#0d1117"
  surface: "#161b22"
  border: "#30363d"
  text: "#e6edf3"
  text-muted: "#8b949e"
  accent: "#58a6ff"
  success: "#3fb950"
  warning: "#d29922"
  danger: "#f85149"
  kind-frontend: "#a371f7"
  kind-backend: "#39c5cf"
  kind-infra: "#8b949e"
typography:
  body:
    fontFamily: "-apple-system, system-ui, sans-serif"
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.4
  mono:
    fontFamily: "ui-monospace, monospace"
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.4
  section-title:
    fontFamily: "-apple-system, system-ui, sans-serif"
    fontSize: 12px
    fontWeight: 600
    lineHeight: 1.3
rounded:
  sm: 4px
  md: 6px
  lg: 8px
  pill: 999px
spacing:
  1: 4px
  2: 8px
  3: 12px
  4: 16px
  5: 24px
  6: 32px
components:
  button:
    height: 32px
    padding: "6px 14px"
    rounded: "{rounded.md}"
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-muted}"
  icon-button:
    size: 30px
    rounded: "{rounded.md}"
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-muted}"
  card:
    rounded: "{rounded.lg}"
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
---

# Orbit Design System

## Overview

Orbit is a luminous local-dev control room. It is the path that holds your stack together: services, containers, databases, logs, envs, and agents moving through a dependency orbit.

The foundation is still a dense, developer-focused orchestration dashboard. It must stay fast to scan, status-first, and built for repeated use while services are starting, failing, rebuilding, and recovering. But Orbit should also have a recognizable celestial systems language: nodes as bodies, edges as paths, traffic as signal flow, rebuilds as energy moving through a pipeline.

Use the luminous layer to carry operational meaning. Glow, pulse, flowing dots, orbital motion, and sci-fi energy treatment are welcome when they reveal live state, dependency movement, build progress, or failure. They are not generic decoration.

Do not interpret "celestial" literally. Do not add planets, stars, galaxies, rockets, astronauts, space wallpaper, or illustrative cosmic scenes to the product UI. The metaphor should appear as precise system behavior: paths, flow, pulse, scan, selection, state, and topology.

Orbit is a Mystery House project: personal, agent-built, idiosyncratic, and fun. It may have taste. It should not feel committee-designed. The design goal is a precise engineering instrument with a bit of living machinery inside it.

When implementing UI, read this file together with `ui/src/app.css`, `.claude/rules/svelte-*.md`, and `docs/CODE_CONVENTIONS.md`.

## Colors

The canonical runtime tokens live in `ui/src/app.css`. Keep new UI aligned with those CSS variables rather than introducing local hex values.

- **Background (`#0d1117`)**: the page and graph canvas foundation.
- **Surface (`#161b22`)**: panels, drawers, controls, modals, repeated cards.
- **Border (`#30363d`)**: the primary depth system. Orbit uses borders before shadows.
- **Text (`#e6edf3`)**: primary readable content.
- **Muted text (`#8b949e`)**: metadata, secondary labels, inactive controls.
- **Accent (`#58a6ff`)**: focus rings, selected graph elements, links, active navigation.
- **State colors**: green means healthy/success, yellow means in progress or caution, red means degraded/danger.
- **Kind colors**: frontend purple, backend teal, infra gray. These identify service kind and must not replace state colors.

The luminous palette is built from these same tokens. Blue is the primary signal/flow color. Green, yellow, and red are reserved for state. Purple and teal identify service kind. Do not introduce unrelated neon palettes.

Avoid one-off decorative palettes, large marketing gradients, bokeh/orb wallpaper, and color treatments that compete with service state. If something glows, it should be because something is alive, flowing, selected, building, complete, or broken.

## Typography

Use the existing type scale from `ui/src/app.css`:

- `--text-xs`: 10px
- `--text-sm`: 11px
- `--text-md`: 12px
- `--text-base`: 13px
- `--text-lg`: 14px
- `--text-xl`: 16px

Use system sans-serif for normal UI and `ui-monospace` only for service names, env names, ports, versions, commands, logs, IDs, and machine-shaped values.

Do not introduce viewport-scaled type. Keep letter spacing at zero except for compact uppercase labels that already use a small positive value.

## Layout

Build on the 4px spacing grid:

- `--space-1`: 4px
- `--space-2`: 8px
- `--space-3`: 12px
- `--space-4`: 16px
- `--space-5`: 24px
- `--space-6`: 32px

Prefer compact, stable layouts with explicit dimensions for graph nodes, icon buttons, toolbars, progress rows, badges, and fixed-format controls. Dynamic content must not resize core controls or cause graph/dashboard layout jumps.

Page sections should be unframed layouts or full-width tool surfaces. Use cards for repeated entities, modals, drawers, and genuinely bounded tools. Do not put cards inside cards.

## Elevation & Depth

Orbit has two depth layers:

- **Operational chrome**: borders-first, compact, quiet. Use subtle surface shifts, 1px borders, selected outlines, and inset highlights before box shadows.
- **Luminous systems layer**: controlled glow for live state, flow, and high-value visualization. Use it on graph traffic, selected nodes, build/rebuild flows, health transitions, and modal overlays where focus needs to lift from the page.

Shadows are acceptable for floating UI that needs separation from the canvas, such as tooltips, popovers, and modals. Keep ordinary UI shadows functional and restrained. Save radiant shadow stacks and glow halos for operational visualization surfaces.

## Shapes

Use small radii:

- 4px for badges, tooltips, and tight tags.
- 6px for compact buttons and nav pills.
- 8px for graph nodes, panels, drawers, modals, and repeated cards.
- Full pill radius only for chips/tags where the shape carries the affordance.

Avoid oversized rounded rectangles. Orbit should feel precise, not soft.

## Components

### Buttons

Use icon buttons for compact actions when a familiar symbol exists. Use lucide icons unless the codebase already uses an established icon for that domain. Icon-only buttons need `aria-label` and a tooltip.

Text buttons are for clear commands, primary drawer actions, and destructive confirmations. Disable buttons while requests are in flight and expose busy/loading state accessibly.

### Graph Nodes

Graph nodes are operational bodies in the Orbit system, not decorative cards. They should keep a stable size, show state first, expose common actions inline, and reserve color intensity for status and kind.

State and kind must remain visually distinct:

- State lives on badges, dots, motion, opacity, and danger affordances.
- Kind lives in subtle node tint and infra icons.

Use motion sparingly but confidently. Starting/building nodes may breathe. Terminal transitions may flash. Healthy traffic may flow along edges. Degraded state should interrupt the system visually without turning the whole canvas into an error page.

Do not make graph nodes look like planets or decorative objects. They remain compact service cards with operational metadata. The "orbital" feeling should come from layout, edges, traffic, and state transitions, not from literal astronomy graphics.

### Drawers & Panels

Use drawers for details that depend on selected graph/service context. The drawer should have one dominant lifecycle action and secondary actions below it. Keep cards collapsible only when the user gains scanability from hiding detail.

### Logs & Console Output

Logs are dense text. Preserve wrapping and readability, use muted text by default, and surface errors through targeted panels or toasts. Never silently swallow fetch/action failures.

### Progress & Long-Running Tasks

Represent rebuilds, starts, health checks, and other long tasks with a clear state badge, progress when known, elapsed time when useful, and ordered steps. If ETA is unknown, avoid fake precision.

### Luminous Operations

Luminous operations are a first-class Orbit pattern. Use them when a workflow has topology, movement, or state change that a static table would flatten. The Services graph is the reference case: nodes as bodies, edges as paths, and the Live flow-dot tracing recent traffic along dependencies in one glance.

Keep these visuals bounded and operational:

- Use them for process comprehension, not decoration.
- Keep surrounding controls dense, token-based, and dashboard-like.
- Tie motion and color to real state such as idle, building, complete, or failed.
- Respect reduced motion and provide non-visual status through badges, text, progress steps, and console output.
- Use Orbit tokens for all colors; glow, scan lines, energy rails, pipes, particles, and halos are acceptable inside bounded operational surfaces when they explain flow.
- Let related systems share the language: graph edge flow, service startup, DB publish, OTel traces, seed runs, and health transitions may all use luminous state-carrying motion.
- Do not spread luminous treatment to normal settings, plain forms, dense lists, basic drawer cards, confirmation modals, or static documentation panels.
- Do not add literal sci-fi illustration or cosmic objects. The visualization should still read as a tool, not a poster.

### Tooltips & Popovers

Tooltips explain compact icon actions and status dots. They must be short, non-interactive, and positioned with the existing tooltip action. Popovers may contain controls, but should stay focused on the thing that opened them.

## Accessibility

Follow `.claude/rules/svelte-a11y.md` for every Svelte change.

- Interactive elements need visible text or `aria-label`.
- Modals and drawers need dialog semantics.
- Status indicators need screen-reader-accessible state.
- Keyboard focus uses the global `:focus-visible` ring.
- Respect reduced motion for transitions and repeated animation.

## Do's and Don'ts

Do:

- Reuse `ui/src/app.css` tokens.
- Keep controls compact and scan-friendly.
- Prefer predictable dashboard patterns over novelty.
- Make failure and degraded states obvious and actionable.
- Use icons, badges, chips, and terse labels to reduce scanning cost.
- Use luminous operational visuals for workflows where topology, flow, or motion carries real meaning.
- Let Orbit's identity show through graph paths, live traffic, pulses, rebuild flows, scans, and trace-like movement.
- Verify UI changes in a browser when layout or interaction changes.

Don't:

- Introduce a new visual theme without updating this file and `ui/src/app.css`.
- Use marketing-page hero layouts, unrelated decorative gradients, generic glassmorphism, or oversized typography.
- Apply glow, glass, pipes, particles, or halos to UI that is not communicating live operational state.
- Add planets, starfields, galaxies, rockets, astronauts, mascots, or other literal space illustrations.
- Add arbitrary colors, spacing values, or radius values for one component.
- Encode service health with kind colors or kind with state colors.
- Hide errors in console-only logs.
- Add new UI libraries for a small local pattern.
