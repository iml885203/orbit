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
]

const sourceRoot = fileURLToPath(new URL('../../', import.meta.url))
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
assert.match(home, /Build the product\. Let agents run the environment\./)
assert.match(home, /aria-label="Search"/)
assert.match(home, /href="\/#get-started"/)
assert.match(home, /href="\/docs\/development#install-orbit"/)
assert.match(home, /<title>Orbit<\/title>/)
assert.ok(home.includes('Help me get this project running with Orbit'), 'project onboarding prompt is missing')
assert.ok(home.includes('Help me try the public demo'), 'agent-first demo prompt is missing')
assert.ok(
  home.indexOf('Help me get this project running with Orbit') < home.indexOf('Help me try the public demo') &&
    home.indexOf('Help me try the public demo') < home.indexOf('Use Orbit regularly with your agent'),
  'first-time value must appear before plugin distribution details',
)

const chineseHome = readFileSync(join(outputPath, 'zh-TW/index.html'), 'utf8')
assert.match(chineseHome, /<html lang="zh-TW"/)
assert.match(chineseHome, /href="\/zh-TW\/docs\/local-first"/)
assert.ok(chineseHome.includes('幫我用 Orbit 跑起這個專案'), 'Traditional Chinese project onboarding prompt is missing')
assert.ok(chineseHome.includes('協助我試玩 public demo'), 'Traditional Chinese agent-first demo prompt is missing')
assert.ok(
  chineseHome.indexOf('幫我用 Orbit 跑起這個專案') < chineseHome.indexOf('協助我試玩 public demo') &&
    chineseHome.indexOf('協助我試玩 public demo') < chineseHome.indexOf('讓 Agent 在之後的 session 直接使用 Orbit'),
  'Traditional Chinese first-time value must appear before plugin distribution details',
)

const chineseGuide = readFileSync(join(outputPath, 'zh-TW/docs/local-first.html'), 'utf8')
for (const route of ['sql-workflow', 'architecture', 'agent-cli', 'versioning', 'CODE_CONVENTIONS']) {
  assert.match(chineseGuide, new RegExp(`/zh-TW/docs/${route}`))
}

for (const [page, language, counterpart] of [
  ['docs/local-first.html', 'en-US', '/zh-TW/docs/local-first'],
  ['zh-TW/docs/local-first.html', 'zh-TW', '/docs/local-first'],
]) {
  const html = readFileSync(join(outputPath, page), 'utf8')
  assert.match(html, new RegExp(`<html lang="${language}"`))
  assert.match(html, language === 'zh-TW'
    ? /<meta name="description" content="為 Agent 而生的本機開發環境編排工具。">/
    : /<meta name="description" content="Agent-native orchestration for local development.">/)
  assert.match(html, /rel="canonical"/)
  assert.match(html, /property="og:title"/)
  assert.match(html, /name="twitter:card"/)
  assert.match(html, /property="og:image" content="https:\/\/raw\.githubusercontent\.com\/iml885203\/orbit\/main\/docs\/assets\/orbit-demo-dashboard\.jpg"/)
  assert.match(html, new RegExp(`rel="alternate"[^>]+href="https://orbit.dotw.me${counterpart}"`))
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
  return /\.(?:css|gif|ico|jpe?g|js|json|png|svg|webp|woff2?|xml)$/.test(relativePath)
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
