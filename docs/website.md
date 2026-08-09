# Documentation website

Orbit's official website is [iml885203.github.io/orbit](https://iml885203.github.io/orbit/).
The repository README is its landing-page source, and files in `docs/` remain
the source for documentation pages. Do not copy documentation into `website/`.

## Local workflow

Use Node.js 22 and pnpm 11, then start the development server:

```bash
make docs-site-dev
```

Build and validate the production site before submitting a documentation
change:

```bash
make docs-site-check
```

The check fails for broken internal links or assets. It also verifies the
landing page, stable core-document routes, heading anchors, and the local
search index. Preview the generated production site with:

```bash
make docs-site-preview
```

## Content and navigation

Edit the existing Markdown owner for a product contract. The website reads
those files directly and adds navigation, responsive presentation, code
highlighting, stable heading links, and local full-site search. Update
`website/.vitepress/config.ts` only when a document needs to enter or leave the
top-level navigation.

Links to Markdown pages should stay relative so they work on GitHub and on the
website. Links to repository files that are not website pages should use their
full GitHub URL.

## Deployment

`.github/workflows/docs.yml` builds the same checked artifact and deploys it to
GitHub Pages after documentation changes reach `main`. The site uses `/orbit/`
as its base path. A future custom domain can change the base and canonical
hostname without changing routes below that base.

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
