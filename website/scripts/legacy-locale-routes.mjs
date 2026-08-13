import { mkdirSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const repositoryRoot = new URL('../../', import.meta.url).pathname
const outputRoot = new URL('../.vitepress/dist/', import.meta.url).pathname
const translatedPages = readdirSync(join(repositoryRoot, 'docs'))
  .filter((name) => name.endsWith('.zh-TW.md'))
  .map((name) => name.replace('.zh-TW.md', ''))

function redirectDocument(target) {
  const escapedTarget = JSON.stringify(target)
  return `<!doctype html><html lang="zh-TW"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${target}"><link rel="canonical" href="https://iml885203.github.io${target}"><title>Orbit</title></head><body><script>location.replace(${escapedTarget}+location.hash)</script><a href="${target}">前往新版頁面</a></body></html>`
}

mkdirSync(join(outputRoot, 'docs'), { recursive: true })
for (const page of translatedPages) {
  const target = `/orbit/zh-TW/docs/${page}`
  writeFileSync(join(outputRoot, 'docs', `${page}.zh-TW.html`), redirectDocument(target))
}
writeFileSync(join(outputRoot, 'README.zh-TW.html'), redirectDocument('/orbit/zh-TW/'))

console.log('legacy Traditional Chinese routes redirect to locale routes')
