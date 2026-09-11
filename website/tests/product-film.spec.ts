import { expect, test } from '@playwright/test'

for (const path of ['/', '/zh-TW/']) {
  test(`one inline film, no duplicate cover: ${path}`, async ({ page }) => {
    await page.goto(`${path}#demo`)
    const film = page.locator('#demo video')
    await expect(film).toBeVisible()
    await expect(page.locator('video')).toHaveCount(1)
    await expect(page.locator('.showcase-node, .showcase-composer')).toHaveCount(0)
    await expect(page.locator('a[href$=".mp4"]')).toHaveCount(0)
    await expect(page.locator('img[src*="orbit-showcase"]')).toHaveCount(0)
    await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(0)
    expect(await film.evaluate(v => ({ muted: v.muted, controls: v.controls, inline: v.playsInline }))).toEqual({ muted: true, controls: true, inline: true })
    await page.getByRole('button', { name: path === '/' ? 'Play from start' : '從頭播放', exact: true }).click()
    expect(await film.evaluate(v => v.currentTime)).toBeLessThan(2)
    await film.evaluate(v => v.pause())
    await expect.poll(() => film.evaluate(v => v.paused)).toBe(true)
    await page.evaluate(() => window.scrollTo(0, 0))
    await film.scrollIntoViewIfNeeded()
    expect(await film.evaluate(v => v.paused)).toBe(true)
    await page.goto('/docs/local-first')
    await expect(page.locator('#demo')).toHaveCount(0)
  })
}

test('reduced motion waits for explicit playback and fits mobile', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('/#demo')
  const film = page.locator('#demo video')
  await expect(film).toBeVisible()
  expect(await film.evaluate(v => ({ paused: v.paused, time: v.currentTime }))).toEqual({ paused: true, time: 0 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  await page.getByRole('button', { name: 'Play from start' }).focus()
  await page.keyboard.press('Enter')
  await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(0)
})

test('pauses outside the viewport and resumes without restarting', async ({ page }) => {
  await page.goto('/#demo')
  const film = page.locator('#demo video')
  await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(0.5)
  await page.evaluate(() => window.scrollTo(0, 0))
  await expect.poll(() => film.evaluate(v => v.paused)).toBe(true)
  const time = await film.evaluate(v => v.currentTime)
  await film.scrollIntoViewIfNeeded()
  await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(time)
})

test('failed media exposes a recovery action', async ({ page }) => {
  await page.route('**/media/orbit-launch.*', route => route.abort())
  await page.goto('/#demo')
  await expect(page.getByRole('alert')).toContainText('The video could not load')
  await expect(page.getByRole('button', { name: 'Retry video' })).toBeVisible()
  await page.unroute('**/media/orbit-launch.*')
  await page.getByRole('button', { name: 'Retry video' }).click()
  await expect.poll(() => page.locator('#demo video').evaluate(v => v.currentTime)).toBeGreaterThan(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
})
