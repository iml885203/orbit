import { mkdirSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const repositoryRoot = fileURLToPath(new URL('../../', import.meta.url))
const outputRoot = fileURLToPath(new URL('../.vitepress/dist/', import.meta.url))
const translatedPages = readdirSync(join(repositoryRoot, 'docs'))
  .filter((name) => name.endsWith('.zh-TW.md'))
  .map((name) => name.replace('.zh-TW.md', ''))

function redirectDocument(target) {
  const escapedTarget = JSON.stringify(target)
  return `<!doctype html><html lang="zh-TW"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${target}"><link rel="canonical" href="https://orbit.dotw.me${target}"><title>Orbit</title></head><body><script>location.replace(${escapedTarget}+location.hash)</script><a href="${target}">前往新版頁面</a></body></html>`
}

mkdirSync(join(outputRoot, 'docs'), { recursive: true })
for (const page of translatedPages) {
  const target = `/zh-TW/docs/${page}`
  writeFileSync(join(outputRoot, 'docs', `${page}.zh-TW.html`), redirectDocument(target))
}
writeFileSync(join(outputRoot, 'README.zh-TW.html'), redirectDocument('/zh-TW/'))

console.log('legacy Traditional Chinese routes redirect to locale routes')
