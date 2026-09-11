# Orbit product film

A 33.5-second, 1080p/60 fps Remotion film. The opening shows a user request,
an agent response, `orbit inspect --json`, its result summary, and the agent's
`orbit up --json` call. The persistent dependency graph moves
from scattered resources into startup order, exposes a failing dependency,
and recovers after a coding agent fixes configuration. The graph moves into
the same node positions in a sharp capture of the real dashboard; the five
resources then converge into the Orbit mark.

```sh
npm ci
node scripts/score.mjs
npm run check
npm run dev
npm run render
```

Output: `out/orbit-launch.mp4`. `npm run poster` creates a closing poster.
Both README languages use the static `../docs/assets/orbit-showcase.png`
cover, linking to the full film at `https://orbit.dotw.me/media/orbit-launch.mp4`.
After rendering, copy `out/poster.png` to that cover path and
`out/orbit-launch.mp4` to `../docs/assets/orbit-launch.mp4`.
The website build publishes the film at its stable media URL. There is no
looping GIF or mid-film excerpt in the README.
All motion and synthesized audio are deterministic. This is an illustrated
workflow, not a recording or a startup-time benchmark. Orbit reports state
and diagnostics; the coding agent owns configuration fixes. The example uses
fictional resource names. Product claims follow `../docs/why-orbit.md` and
`../plugins/orbit/skills/orbit/SKILL.md`.

The bundled dashboard PNG is captured at 2× pixel density from Orbit's actual
Svelte application using synthetic API fixtures, never a live environment.
To refresh both the image and its measured node coordinates:

```sh
cd ../ui
ORBIT_UI_OUTDIR=../launch-video/out/dashboard-build npm run build
cd ../launch-video
npx playwright install chromium
npm run capture
```

The capture script serves the built UI on an ephemeral loopback port, checks
browser errors and missing fixture endpoints, and closes its browser and
server when done. `public/dashboard-layout.json` drives the matched reveal.

Timeline: agent conversation and tool calls (0–7.2 s), stack assembly
(7.2–10.9 s), ordered readiness (10.9–15.5 s), dependency failure
(15.5–20.4 s), agent recovery (20.4–24.5 s), matched dashboard reveal
(24.5–29.5 s), node convergence and brand close (29.5–33.5 s).

`PREVIEW_HOST=<LAN-IP> npm run preview` serves the film on port 8765, with
byte-range support and caching disabled so reloading shows the latest render.

Original soundtrack is synthesized by `scripts/score.mjs`, with impacts on
scene boundaries. Fonts are bundled via Fontsource; their packages carry
the SIL Open Font License. Orbit assets and this composition follow the
repository license; Remotion has its own licensing terms.
