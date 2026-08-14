import { expect, test } from '@playwright/test'

test('keeps the primary action visible on a short phone', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('./')

  const primaryAction = page.getByRole('link', { name: 'Try with your agent', exact: true }).first()
  const actionBox = await primaryAction.boundingBox()
  const orbitBox = await page.locator('.hero-orbit canvas').boundingBox()
  expect(actionBox && actionBox.y + actionBox.height).toBeLessThanOrEqual(568)
  expect(orbitBox && orbitBox.width > 0 && orbitBox.height > 0).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('maintains one main landmark through client-side navigation', async ({ page }) => {
  await page.goto('./')
  await expect(page.locator('main, [role="main"]')).toHaveCount(1)
  await page.keyboard.press('Tab')
  const skipLink = page.getByRole('link', { name: 'Skip to content' })
  await expect(skipLink).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.locator('#VPContent')).toBeFocused()
  await page.getByRole('link', { name: 'Try with your agent', exact: true }).first().click()
  await expect(page).toHaveURL(/\/#try-orbit$/)
  await expect(page.locator('main, [role="main"]')).toHaveCount(1)
  await page.getByRole('link', { name: 'Orbit', exact: true }).click()
  await expect(page).toHaveURL(/\/orbit\/$/)
  await expect(page.locator('main, [role="main"]')).toHaveCount(1)
})

test('closes mobile navigation with Escape and restores focus', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 })
  await page.goto('./')
  const toggle = page.getByRole('button', { name: 'mobile navigation' })
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect.poll(() => page.locator('body').evaluate((body) => getComputedStyle(body).overflow)).toBe('hidden')
  await page.keyboard.press('Escape')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(toggle).toBeFocused()
  await expect.poll(() => page.locator('body').evaluate((body) => getComputedStyle(body).overflow)).not.toBe('hidden')
})

test('switches between distinct light and dark themes', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('vitepress-theme-appearance', 'light'))
  await page.goto('./')

  const appearance = page.locator('.VPSwitchAppearance').first()
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  const lightBackground = await page.locator('html').evaluate((root) => getComputedStyle(root).getPropertyValue('--vp-c-bg'))

  await appearance.click()
  await expect(page.locator('html')).toHaveClass(/dark/)
  const darkBackground = await page.locator('html').evaluate((root) => getComputedStyle(root).getPropertyValue('--vp-c-bg'))

  expect(lightBackground.trim()).toBe('#ffffff')
  expect(darkBackground.trim()).toBe('#0d1117')
})

test('uses dark mode on the first visit', async ({ page }) => {
  await page.goto('./')

  await expect(page.locator('html')).toHaveClass(/dark/)
  const background = await page.locator('html').evaluate((root) => getComputedStyle(root).getPropertyValue('--vp-c-bg'))
  expect(background.trim()).toBe('#0d1117')
})

test('renders a bounded animated hero and respects reduced motion', async ({ page }) => {
  await page.goto('./')
  const hero = page.locator('.hero-orbit')
  const canvas = hero.locator('canvas')
  await expect(hero).toHaveAttribute('data-motion', 'running')
  const dimensions = await canvas.evaluate((element) => {
    const bounds = element.getBoundingClientRect()
    return { cssWidth: bounds.width, cssHeight: bounds.height, width: element.width, height: element.height }
  })
  expect(dimensions.width).toBeGreaterThanOrEqual(dimensions.cssWidth)
  expect(dimensions.width).toBeLessThanOrEqual(dimensions.cssWidth * 2 + 1)
  expect(dimensions.height).toBeGreaterThanOrEqual(dimensions.cssHeight)
  expect(dimensions.height).toBeLessThanOrEqual(dimensions.cssHeight * 2 + 1)

  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(hero).toHaveAttribute('data-motion', 'reduced')
})

test('opens search on the first click and uses the active locale index', async ({ page }) => {
  await page.goto('./')
  await page.getByRole('button', { name: 'Search' }).click()
  const input = page.getByPlaceholder('Search')
  await expect(input).toBeVisible()
  await input.fill('instance')
  const firstEnglishResult = page.locator('.result').first()
  await expect(firstEnglishResult).not.toHaveAttribute('href', /\/zh-TW\//)
  await firstEnglishResult.focus()
  await page.keyboard.press('Enter')
  await expect(page).not.toHaveURL(/\/zh-TW\//)

  await page.goto('./zh-TW/')
  await page.getByRole('button', { name: '搜尋' }).click()
  await page.getByPlaceholder('搜尋').fill('instance')
  const firstChineseResult = page.locator('.result').first()
  await expect(firstChineseResult).toHaveAttribute('href', /\/zh-TW\//)
})

test('keeps the other language discoverable through the language switcher', async ({ page }) => {
  await page.goto('./docs/instances')
  await page.getByRole('button', { name: 'Change language' }).click()
  await page.getByRole('banner').getByRole('link', { name: '繁體中文' }).click()
  await expect(page).toHaveURL(/\/zh-TW\/docs\/instances$/)
  await page.getByRole('button', { name: '搜尋' }).click()
  await page.getByPlaceholder('搜尋').fill('instance')
  await expect(page.locator('.result').first()).toHaveAttribute('href', /\/zh-TW\//)
})

test('renders locale-owned Chinese navigation and metadata', async ({ page }) => {
  await page.goto('./zh-TW/docs/local-first')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-TW')
  await expect(page.getByRole('navigation', { name: 'Main Navigation' }).getByRole('link', { name: '開始使用' })).toHaveAttribute('href', /\/zh-TW\/docs\/local-first/)
  await expect(page.getByText('採用 MIT License 發布。')).toBeAttached()
  await expect(page.getByRole('link', { name: '在 GitHub 編輯此頁' })).toHaveAttribute('href', 'https://github.com/iml885203/orbit/edit/main/docs/local-first.zh-TW.md')
  const sidebarLinks = await page.locator('.VPSidebar a').evaluateAll((links) => links.map((link) => link.getAttribute('href')).filter(Boolean))
  expect(sidebarLinks.every((href) => href!.includes('/zh-TW/docs/'))).toBe(true)
})
