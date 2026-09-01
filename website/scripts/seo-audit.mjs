import assert from 'node:assert/strict'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn } from 'node:child_process'
import { chromium } from '@playwright/test'

const websiteRoot = dirname(dirname(fileURLToPath(import.meta.url)))
const auditURL = 'http://127.0.0.1:4174/'
const temporaryDirectory = await mkdtemp(join(tmpdir(), 'orbit-seo-'))
const reportPath = join(temporaryDirectory, 'lighthouse.json')
const preview = spawn(process.execPath, [
  join(websiteRoot, 'node_modules/vitepress/bin/vitepress.js'),
  'preview',
  '.',
  '--host',
  '127.0.0.1',
  '--port',
  '4174',
], { cwd: websiteRoot, stdio: 'pipe' })

try {
  await waitForPreview()
  await runLighthouse()
  const report = JSON.parse(await readFile(reportPath, 'utf8'))
  const seo = report.categories.seo
  const failedAudits = seo.auditRefs
    .map(({ id }) => report.audits[id])
    .filter((audit) => audit.scoreDisplayMode !== 'notApplicable' && audit.score !== null && audit.score < 1)
    .map((audit) => `${audit.title}${audit.displayValue ? `: ${audit.displayValue}` : ''}`)

  assert.equal(seo.score, 1, `Lighthouse SEO must be 100/100:\n${failedAudits.join('\n')}`)
  console.log(`Lighthouse ${report.lighthouseVersion} SEO: ${Math.round(seo.score * 100)}/100`)
} finally {
  preview.kill('SIGTERM')
  await rm(temporaryDirectory, { recursive: true, force: true })
}

async function waitForPreview() {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (preview.exitCode !== null) throw new Error(`VitePress preview exited with code ${preview.exitCode}`)
    try {
      const response = await fetch(auditURL)
      if (response.ok) return
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 100))
    }
  }
  throw new Error(`VitePress preview did not become ready at ${auditURL}`)
}

async function runLighthouse() {
  const lighthouse = spawn(process.execPath, [
    join(websiteRoot, 'node_modules/lighthouse/cli/index.js'),
    auditURL,
    '--only-categories=seo',
    '--output=json',
    `--output-path=${reportPath}`,
    '--chrome-flags=--headless --no-sandbox',
    '--quiet',
  ], {
    cwd: websiteRoot,
    env: { ...process.env, CHROME_PATH: chromium.executablePath() },
    stdio: 'inherit',
  })
  const exitCode = await new Promise((resolve, reject) => {
    lighthouse.once('error', reject)
    lighthouse.once('exit', resolve)
  })
  assert.equal(exitCode, 0, `Lighthouse exited with code ${exitCode}`)
}
