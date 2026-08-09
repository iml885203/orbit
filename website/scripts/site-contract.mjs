import assert from 'node:assert/strict'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

const outputDirectory = new URL('../.vitepress/dist/', import.meta.url)
const outputPath = outputDirectory.pathname
const baseURL = new URL('https://iml885203.github.io/orbit/')

const requiredPages = [
  'index.html',
  'README.zh-TW.html',
  'docs/local-first.html',
  'docs/local-first.zh-TW.html',
  'docs/configuration.html',
  'docs/architecture.html',
  'docs/troubleshooting.html',
]

for (const page of requiredPages) {
  assert.ok(existsSync(join(outputPath, page)), `missing required page: ${page}`)
}

const home = readFileSync(join(outputPath, 'index.html'), 'utf8')
assert.match(home, /Your whole local stack, one observable environment\./)
assert.match(home, /aria-label="Search"/)
assert.match(home, /href="\/orbit\/docs\/local-first"/)
assert.match(home, /<title>Orbit<\/title>/)

const traditionalChineseHome = readFileSync(join(outputPath, 'README.zh-TW.html'), 'utf8')
assert.match(traditionalChineseHome, /<h1 id="orbit"[^>]*><img /)
assert.doesNotMatch(traditionalChineseHome, /&lt;img src=/)
assert.match(traditionalChineseHome, /href="\/orbit\/docs\/local-first.zh-TW"/)

const traditionalChineseGuide = readFileSync(join(outputPath, 'docs/local-first.zh-TW.html'), 'utf8')
for (const route of ['sql-workflow.zh-TW', 'architecture.zh-TW', 'agent-cli.zh-TW', 'versioning.zh-TW']) {
  assert.match(traditionalChineseGuide, new RegExp(`/orbit/docs/${route}`))
}

const configuration = readFileSync(join(outputPath, 'docs/configuration.html'), 'utf8')
assert.match(configuration, /class="header-anchor"/)

const searchIndex = readdirSync(join(outputPath, 'assets/chunks'))
  .find((name) => name.startsWith('@localSearchIndexroot.') && name.endsWith('.js'))
assert.ok(searchIndex, 'missing local search index')

const indexedContent = readFileSync(join(outputPath, 'assets/chunks', searchIndex), 'utf8')
for (const term of ['low-level-runtime-overrides', 'Component map', 'data loss might occur']) {
  assert.ok(indexedContent.includes(term), `local search index is missing: ${term}`)
}

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
  const attributes = html.matchAll(/\b(?:href|src)="([^"]+)"/g)
  for (const [, value] of attributes) {
    if (/^(?:mailto:|data:|javascript:)/.test(value)) continue

    const target = new URL(value, publicURLFor(htmlFile))
    if (target.origin !== baseURL.origin) continue
    assert.ok(
      target.pathname.startsWith(baseURL.pathname),
      `${relative(outputPath, htmlFile)} escapes the site base: ${value}`,
    )
    const targetFile = outputFileFor(target)
    assert.ok(
      existsSync(targetFile),
      `${relative(outputPath, htmlFile)} has an unresolved link: ${value}`,
    )
    if (target.hash) {
      const targetHTML = readFileSync(targetFile, 'utf8')
      const targetID = decodeURIComponent(target.hash.slice(1))
      assert.ok(
        targetHTML.includes(`id="${targetID}"`),
        `${relative(outputPath, htmlFile)} has an unresolved fragment: ${value}`,
      )
    }
  }
}

console.log('docs website pages, search index, deep links, and internal assets are valid')
