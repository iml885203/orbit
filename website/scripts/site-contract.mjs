import assert from 'node:assert/strict'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const outputDirectory = new URL('../.vitepress/dist/', import.meta.url)
const outputPath = fileURLToPath(outputDirectory)
const baseURL = new URL('https://orbit.dotw.me/')
const requiredPages = [
  'index.html',
  'zh-TW/index.html',
  'README.zh-TW.html',
  'docs/local-first.html',
  'zh-TW/docs/local-first.html',
  'docs/local-first.zh-TW.html',
  'docs/configuration.html',
  'docs/architecture.html',
  'docs/troubleshooting.html',
  'agent/SKILL.md',
  'agent/references/workflows.md',
  'agent/references/aspire-migration.md',
  '.well-known/agent-skills/index.json',
  '.well-known/ai-catalog.json',
  'orbit-social-card.png',
]

const sourceRoot = fileURLToPath(new URL('../../', import.meta.url))
const docsWorkflow = readFileSync(join(sourceRoot, '.github/workflows/docs.yml'), 'utf8')
assert.match(docsWorkflow, /- 'plugins\/orbit\/skills\/orbit\/\*\*'/, 'skill changes must publish the website mirror')
assert.match(docsWorkflow, /include-hidden-files: true/, 'Pages deployment must publish agent discovery files below .well-known')
const translatedSources = readdirSync(join(sourceRoot, 'docs'))
  .filter((name) => name.endsWith('.zh-TW.md'))
for (const source of translatedSources) {
  const page = source.replace('.zh-TW.md', '')
  const adapterPath = join(sourceRoot, 'zh-TW/docs', `${page}.md`)
  assert.ok(existsSync(adapterPath), `missing locale adapter for docs/${source}`)
  assert.equal(
    readFileSync(adapterPath, 'utf8').trim(),
    `<!--@include: ../../docs/${source}-->`,
    `locale adapter must include only docs/${source}`,
  )
}
const adapters = readdirSync(join(sourceRoot, 'zh-TW/docs')).filter((name) => name.endsWith('.md'))
assert.equal(adapters.length, translatedSources.length, 'every locale adapter must have one translated source')

for (const page of requiredPages) assert.ok(existsSync(join(outputPath, page)), `missing required page: ${page}`)

const home = readFileSync(join(outputPath, 'index.html'), 'utf8')
assert.match(home, /Build the product\. Let your agent run the project\./)
assert.match(home, /aria-label="Search"/)
assert.match(home, /href="\/#get-started"/)
assert.match(home, /<title>Local Dev Environments for Coding Agents \| Orbit<\/title>/)
assert.match(home, /<meta name="description" content="Orbit gives coding agents one consistent way to start, inspect, and verify complete local development environments through a CLI and visual dashboard\.">/)
assert.match(home, /property="og:title" content="Orbit — Local Dev Environments for Coding Agents"/)
assert.match(home, /property="og:description" content="Orbit gives coding agents one consistent way to start, inspect, and verify complete local development environments through a CLI and visual dashboard\.">/)
assert.ok(home.includes('Read https://orbit.dotw.me and help me get this project running with Orbit'), 'URL-first project onboarding prompt is missing')
assert.match(home, /rel="alternate" type="text\/markdown" href="\/agent\/SKILL\.md"/)
assert.match(home, /rel="alternate" hreflang="en" href="https:\/\/orbit\.dotw\.me\/"/)
assert.match(home, /rel="alternate" hreflang="x-default" href="https:\/\/orbit\.dotw\.me\/"/)
assert.match(home, /type="application\/ld\+json"/)
assert.match(home, /"@type":"SoftwareApplication"/)
assert.match(home, /property="og:image" content="https:\/\/orbit\.dotw\.me\/orbit-social-card\.png"/)
assert.match(home, /property="og:image:width" content="1200"/)
assert.match(home, /property="og:image:height" content="630"/)
assert.match(home, /rel="ai-catalog" href="\/\.well-known\/ai-catalog\.json"/)
assert.match(home, /<section class="homepage-showcase /)
assert.match(home, /Ask once\. See the whole environment come alive\./)
assert.match(home, /Read orbit\.dotw\.me and get this project running\./)
assert.match(home, /Orbit found the project environment\. Starting its dependencies now\./)
assert.match(home, /Environment ready · 6 nodes healthy/)
assert.match(home, /href="\/docs\/tracing"/)
const showcaseHtml = home.match(/<section class="homepage-showcase [\s\S]*?<\/section>/)?.[0] ?? ''
for (const node of ['web', 'api', 'worker', 'postgresql', 'redis', 'kafka']) assert.match(showcaseHtml, new RegExp(`>${node}<`))
for (const relationship of ['Web depends on API', 'API depends on PostgreSQL and Redis', 'Worker depends on PostgreSQL and Kafka']) {
  assert.ok(showcaseHtml.includes(relationship), `English showcase relationship is missing: ${relationship}`)
}
assert.ok((showcaseHtml.match(/>Healthy</g) ?? []).length >= 6, 'every SSR graph node must expose its healthy state')
assert.doesNotMatch(showcaseHtml, /<(?:input|button|img|video|canvas|iframe)\b/)
assert.doesNotMatch(showcaseHtml, /homepage-showcase-stage|data-stage=/)

const robots = readFileSync(join(outputPath, 'robots.txt'), 'utf8')
assert.match(robots, /Content-Signal: ai-train=no, search=yes, ai-input=yes/)

const skillIndex = JSON.parse(readFileSync(join(outputPath, '.well-known/agent-skills/index.json'), 'utf8'))
assert.equal(skillIndex.$schema, 'https://schemas.agentskills.io/discovery/0.2.0/schema.json')
assert.equal(skillIndex.skills[0].url, 'https://orbit.dotw.me/agent/SKILL.md')
assert.match(skillIndex.skills[0].digest, /^sha256:[a-f0-9]{64}$/)

const agentCatalog = JSON.parse(readFileSync(join(outputPath, '.well-known/ai-catalog.json'), 'utf8'))
assert.equal(agentCatalog.host.identifier, 'did:web:orbit.dotw.me')
assert.equal(agentCatalog.entries[0].url, 'https://orbit.dotw.me/agent/SKILL.md')

const notFound = readFileSync(join(outputPath, '404.html'), 'utf8')
assert.match(notFound, /<meta name="robots" content="noindex, nofollow">/)

const chineseHome = readFileSync(join(outputPath, 'zh-TW/index.html'), 'utf8')
assert.match(chineseHome, /<html lang="zh-TW"/)
assert.match(chineseHome, /href="\/zh-TW\/docs\/local-first"/)
assert.ok(chineseHome.includes('閱讀 https://orbit.dotw.me，幫我用 Orbit 把這個專案跑起來'), 'Traditional Chinese URL-first project onboarding prompt is missing')
assert.match(chineseHome, /<section class="homepage-showcase /)
assert.match(chineseHome, /問一次，看見整個環境依序啟動。/)
assert.match(chineseHome, /閱讀 orbit\.dotw\.me，幫我把這個專案跑起來。/)
assert.match(chineseHome, /環境已就緒 · 6 個 nodes 健康/)
assert.match(chineseHome, /href="\/zh-TW\/docs\/tracing"/)
const chineseShowcaseHtml = chineseHome.match(/<section class="homepage-showcase [\s\S]*?<\/section>/)?.[0] ?? ''
for (const node of ['web', 'api', 'worker', 'postgresql', 'redis', 'kafka']) assert.match(chineseShowcaseHtml, new RegExp(`>${node}<`))
for (const relationship of ['Web 依賴 API', 'API 依賴 PostgreSQL 與 Redis', 'Worker 依賴 PostgreSQL 與 Kafka']) {
  assert.ok(chineseShowcaseHtml.includes(relationship), `Traditional Chinese showcase relationship is missing: ${relationship}`)
}
assert.ok((chineseShowcaseHtml.match(/>Healthy</g) ?? []).length >= 6, 'every Traditional Chinese SSR graph node must expose its healthy state')

const skillSource = readFileSync(join(sourceRoot, 'plugins/orbit/skills/orbit/SKILL.md'))
assert.deepEqual(readFileSync(join(outputPath, 'agent/SKILL.md')), skillSource, 'published agent skill must exactly mirror its source')
const skillDirectory = join(sourceRoot, 'plugins/orbit/skills/orbit')
const skillFiles = filesBelow(skillDirectory)
for (const source of skillFiles) {
  const path = relative(skillDirectory, source).split(sep).join('/')
  assert.deepEqual(readFileSync(join(outputPath, 'agent', path)), readFileSync(source), `published skill does not match source: ${path}`)
}
const mirroredSkillFiles = filesBelow(join(outputPath, 'agent'))
assert.equal(mirroredSkillFiles.length, skillFiles.length, 'published skill contains files outside the canonical source')

const chineseGuide = readFileSync(join(outputPath, 'zh-TW/docs/local-first.html'), 'utf8')
for (const route of ['sql-workflow', 'architecture', 'agent-cli', 'versioning', 'CODE_CONVENTIONS']) {
  assert.match(chineseGuide, new RegExp(`/zh-TW/docs/${route}`))
}

for (const [page, language, counterpart] of [
  ['docs/local-first.html', 'en-US', '/zh-TW/docs/local-first'],
  ['zh-TW/docs/local-first.html', 'zh-TW', '/docs/local-first'],
]) {
  const html = readFileSync(join(outputPath, page), 'utf8')
  assert.doesNotMatch(html, /homepage-showcase/, 'showcase must render on homepages only')
  assert.match(html, new RegExp(`<html lang="${language}"`))
  assert.match(html, language === 'zh-TW'
    ? /<meta name="description" content="Orbit 讓 coding agents 透過一致的 CLI 與視覺化 dashboard，啟動、檢查並驗證完整的本機開發環境。">/
    : /<meta name="description" content="Orbit gives coding agents one consistent way to start, inspect, and verify complete local development environments through a CLI and visual dashboard.">/)
  assert.match(html, /rel="canonical"/)
  assert.match(html, /property="og:title"/)
  assert.match(html, /property="og:site_name" content="Orbit"/)
  assert.match(html, /name="twitter:card"/)
  assert.match(html, /property="og:image" content="https:\/\/orbit\.dotw\.me\/orbit-social-card\.png"/)
  assert.match(html, new RegExp(`rel="alternate"[^>]+href="https://orbit.dotw.me${counterpart}"`))
  assert.match(html, /rel="alternate" hreflang="x-default" href="https:\/\/orbit\.dotw\.me\/"/)
}
assert.match(chineseGuide, /href="https:\/\/github.com\/iml885203\/orbit\/edit\/main\/docs\/local-first\.zh-TW\.md"/)

const searchIndexName = readdirSync(join(outputPath, 'assets/chunks'))
  .find((name) => name.startsWith('@localSearchIndexroot.') && name.endsWith('.js'))
assert.ok(searchIndexName, 'missing local search index')
const indexedContent = readFileSync(join(outputPath, 'assets/chunks', searchIndexName), 'utf8')
for (const term of ['low-level-runtime-overrides', 'Component map', 'data loss might occur']) {
  assert.ok(indexedContent.includes(term), `local search index is missing: ${term}`)
}
assert.ok(!indexedContent.includes('Orbit 1.0 test matrix'), 'release audit must not appear in local search')
assert.ok(!indexedContent.includes('Documentation website'), 'website maintenance must not appear in local search')

function filesBelow(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? filesBelow(path) : [path]
  })
}

function publicURLFor(file) {
  const outputRelativePath = relative(outputPath, file).split(sep).join('/')
  if (outputRelativePath === 'index.html') return baseURL
  return new URL(outputRelativePath.replace(/\.html$/, ''), baseURL)
}

function outputFileFor(url) {
  const relativePath = decodeURIComponent(url.pathname.slice(baseURL.pathname.length))
  if (!relativePath) return join(outputPath, 'index.html')
  if (relativePath.endsWith('/')) return join(outputPath, relativePath, 'index.html')
  return /\.(?:css|gif|gz|ico|jpe?g|js|json|md|png|svg|txt|webp|woff2?|xml)$/.test(relativePath)
    ? join(outputPath, relativePath)
    : join(outputPath, `${relativePath}.html`)
}

for (const htmlFile of filesBelow(outputPath).filter((file) => file.endsWith('.html'))) {
  const html = readFileSync(htmlFile, 'utf8')
  for (const [, value] of html.matchAll(/\b(?:href|src)="([^"]+)"/g)) {
    if (/^(?:mailto:|data:|javascript:)/.test(value)) continue
    const target = new URL(value, publicURLFor(htmlFile))
    if (target.origin !== baseURL.origin) continue
    assert.ok(target.pathname.startsWith(baseURL.pathname), `${relative(outputPath, htmlFile)} escapes site base: ${value}`)
    const targetFile = outputFileFor(target)
    assert.ok(existsSync(targetFile), `${relative(outputPath, htmlFile)} has unresolved link: ${value}`)
    if (target.hash) {
      const targetHTML = readFileSync(targetFile, 'utf8')
      assert.ok(targetHTML.includes(`id="${decodeURIComponent(target.hash.slice(1))}"`), `${relative(outputPath, htmlFile)} has unresolved fragment: ${value}`)
    }
  }
}

console.log('documentation routes, locales, metadata, search index, and links are valid')
