// Renders ui/public/orbit-social-card.png (the og:image) from the homepage
// copy, so the card can never advertise a headline the homepage no longer
// uses. Run on demand with `pnpm run social-card` after changing the headline.
import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'
import { homepageCopy } from '../.vitepress/homepage-copy.ts'

const output = fileURLToPath(new URL('../../ui/public/orbit-social-card.png', import.meta.url))
const logo = readFileSync(fileURLToPath(new URL('../../ui/public/orbit-logo.svg', import.meta.url)), 'utf8')
const copy = homepageCopy['en-US']

const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><style>
  html, body { margin: 0; width: 1200px; height: 630px; overflow: hidden; }
  body {
    position: relative; box-sizing: border-box; padding: 72px 80px;
    background: #0d1117; color: #e6edf3;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
  }
  .glow {
    position: absolute; right: -140px; top: -120px; width: 720px; height: 720px; border-radius: 50%;
    background: radial-gradient(circle at 50% 50%, rgba(88,166,255,.34) 0%, rgba(123,83,214,.16) 38%, rgba(13,17,23,0) 70%);
  }
  .orbit { position: absolute; border: 1.5px solid rgba(121,192,255,.3); border-radius: 50%; }
  .brand { position: relative; display: flex; align-items: center; gap: 16px; font-size: 30px; font-weight: 700; }
  .brand svg { width: 44px; height: 44px; }
  h1 { position: relative; margin: 110px 0 0; max-width: 1040px; font-size: 48px; line-height: 1.1; letter-spacing: -0.03em; font-weight: 800; }
  p { position: relative; margin: 26px 0 0; max-width: 820px; font-size: 26px; line-height: 1.4; color: #8b949e; }
  .url { position: absolute; left: 80px; bottom: 60px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 22px; color: #58a6ff; }
</style></head><body>
  <div class="glow"></div>
  <div class="orbit" style="right:-40px;top:40px;width:560px;height:220px;transform:rotate(-18deg)"></div>
  <div class="orbit" style="right:20px;top:120px;width:420px;height:150px;transform:rotate(14deg)"></div>
  <div class="brand">${logo}<span>Orbit</span></div>
  <h1>${copy.headline[0]}<br>${copy.headline[1]}</h1>
  <p>${copy.subhead}</p>
  <div class="url">orbit.dotw.me</div>
</body></html>`

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 })
await page.setContent(html)
await page.evaluate(() => document.fonts.ready)
writeFileSync(output, await page.screenshot({ type: 'png' }))
await browser.close()
console.log(`Rendered ${output}`)
