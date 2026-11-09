# Documentation website

Edit, preview, and validate the documentation for
[orbit.ilogan.app](https://orbit.ilogan.app/).

Edit documentation in `docs/` and keep it there. The homepages are
`index.md` and `zh-TW/index.md` at the repository root. Both files contain
only frontmatter. The READMEs are for GitHub.

## Local workflow

Use Node.js 22 and pnpm 11, then start the development server:

```bash
make docs-site-dev
```

The full check installs website dependencies once and installs its pinned
headless Chromium build on first use. To prepare that browser separately, run
`make docs-site-browser-setup`. Callers that provide
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` keep using that browser instead.

Build and validate the production site before submitting a documentation
change:

```bash
make docs-site-check
```

The check fails for broken internal links or assets. It also verifies the
landing page, stable core-document routes, locale metadata, heading anchors,
and the local search index. A headless browser also verifies search, keyboard
navigation, landmarks, the short-phone layout, the prompt's Copy button, the
hero band's motion and reduced-motion behavior, and that the homepage film
plays only when the visitor asks. Preview the generated production site with:

```bash
make docs-site-preview
```

## Content and navigation

Edit the existing Markdown owner for a product contract. The website reads
those files directly and adds navigation, responsive presentation, code
highlighting, stable heading links, and local full-site search. Update
`website/.vitepress/config.ts` when a document needs to enter or leave the
top-level navigation. Homepage presentation copy for both locales (headline,
subhead, the three columns, the film caption) lives in
`website/.vitepress/homepage-copy.ts`; keep it concise. The prompt in that file
must match `docs/local-first.md` word for word: the site contract reads the
prompt from that owner and asserts that the READMEs and both homepages carry
it. The version in the hero's trust line is the newest `v*` tag at build time,
which is why the Pages workflow fetches tags; a tagless checkout omits the
segment. After changing the headline, run `pnpm --dir website run social-card`
(Node.js 22.18 or newer, which strips the copy module's types) to regenerate
the social preview image.

Links to Markdown pages should stay relative so they work on GitHub and on the
website. Links to repository files that are not website pages should use their
full GitHub URL.

English pages live at the site root and Traditional Chinese pages live below
`/zh-TW/`. Existing `.zh-TW` URLs remain redirect aliases. Pages without a
translation fall back to English; do not create a placeholder translation just
to fill a navigation slot.

## Deployment

`.github/workflows/docs.yml` builds the same checked artifact and deploys it to
GitHub Pages after documentation changes reach `main`, and again after the
Release workflow succeeds, so the version the homepage names never lags a
release. The custom domain serves the site from `/`, with English pages at the
root and Traditional Chinese pages below `/zh-TW/`.

VitePress was selected after evaluating Docusaurus first. Both support
Markdown documentation and stable routes, but Docusaurus needs an external
service or an additional plugin for local full-site search. Its documentation
version snapshots would also create another copy of Orbit's source content.
VitePress provides first-party local search and build-time link checking with a
smaller Vite-based toolchain.

The stable VitePress release still declares Vite 5, whose development server
has no release containing the current security patches. `website/package.json`
therefore pins the patched Vite 6.4.3 release. The production build, generated
link/search contract, and browser interaction checks are the compatibility
gate for that temporary major-version override. Remove it when a stable
VitePress release supports a patched Vite major.

## Website analytics

The public website uses PostHog Cloud (US) for pageviews, page departures and project-link clicks. Events include `product` and `site_domain` and share a project with Logan’s other public websites. URL queries and fragments are stripped before sending. Tracking runs only on the production website hostname and honors Do Not Track and Global Privacy Control. Link/button click autocapture, heatmaps and Web vitals are enabled. Session replay records 100% of eligible sessions with all inputs masked; console, network bodies/headers and network timing capture are disabled. No user identification is performed. It does not instrument the locally running application or CLI. The browser project token is public; analytics-reading credentials must never be added to the frontend.
