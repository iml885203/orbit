import { expect, test, type Page } from '@playwright/test'

// The hero sets data-motion only after the client mounts, so waiting for it
// keeps a click from landing on server-rendered markup before hydration.
async function openHydrated(page: Page, path: string) {
  await page.goto(path)
  await expect(page.locator('.hero-orbit')).toHaveAttribute('data-motion', /running|reduced|paused/)
}

test('stops the film when the visitor switches language', async ({ page }) => {
  await openHydrated(page, '/#demo')
  await page.getByRole('button', { name: 'Play the 34-second demo', exact: true }).click()
  const film = page.locator('#demo video')
  await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(0)
  await page.getByRole('contentinfo').getByRole('link', { name: '繁體中文' }).click()
  await expect(page).toHaveURL(/\/zh-TW\/$/)
  await expect.poll(() => film.evaluate(v => ({ paused: v.paused, time: v.currentTime }))).toEqual({ paused: true, time: 0 })
  await expect(page.getByRole('button', { name: '播放 34 秒示範', exact: true })).toBeVisible()
})

for (const [path, playLabel] of [['/', 'Play the 34-second demo'], ['/zh-TW/', '播放 34 秒示範']] as const) {
  test(`plays the film only when asked: ${path}`, async ({ page }) => {
    await openHydrated(page, `${path}#demo`)
    const film = page.locator('#demo video')
    await expect(film).toBeVisible()
    await expect(page.locator('video')).toHaveCount(1)
    await expect(page.locator('a[href$=".mp4"]')).toHaveCount(0)
    // Enough data to play must be present and a moment must pass before
    // "never autoplayed" means anything; an observer-driven autoplay would
    // show up as played time within that window.
    await expect.poll(() => film.evaluate(v => v.readyState)).toBeGreaterThanOrEqual(2)
    await page.waitForTimeout(600)
    expect(await film.evaluate(v => ({ paused: v.paused, time: v.currentTime, played: v.played.length, controls: v.controls }))).toEqual({ paused: true, time: 0, played: 0, controls: false })

    await page.getByRole('button', { name: playLabel, exact: true }).click()
    await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(0)
    expect(await film.evaluate(v => ({ controls: v.controls, inline: v.playsInline }))).toEqual({ controls: true, inline: true })
    await expect(page.getByRole('button', { name: playLabel, exact: true })).toHaveCount(0)

    const beforeScroll = await film.evaluate(v => v.currentTime)
    await page.evaluate(() => window.scrollTo(0, 0))
    await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(beforeScroll)

    await page.goto('/docs/local-first')
    await expect(page.locator('#demo')).toHaveCount(0)
  })
}

test('reduced motion keeps the poster and plays from the keyboard on a small phone', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 320, height: 568 })
  await openHydrated(page, '/#demo')
  const film = page.locator('#demo video')
  await expect(film).toBeVisible()
  expect(await film.evaluate(v => ({ paused: v.paused, time: v.currentTime }))).toEqual({ paused: true, time: 0 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  await page.getByRole('button', { name: 'Play the 34-second demo' }).focus()
  await page.keyboard.press('Enter')
  await expect.poll(() => film.evaluate(v => v.currentTime)).toBeGreaterThan(0)
})

test('failed media exposes a recovery action', async ({ page }) => {
  await page.route('**/media/orbit-launch.*', route => route.abort())
  await openHydrated(page, '/#demo')
  await expect(page.getByRole('alert')).toContainText('The video could not load')
  await expect(page.getByRole('button', { name: 'Retry video' })).toBeVisible()
  await page.unroute('**/media/orbit-launch.*')
  await page.getByRole('button', { name: 'Retry video' }).click()
  await expect.poll(() => page.locator('#demo video').evaluate(v => v.currentTime)).toBeGreaterThan(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
})
