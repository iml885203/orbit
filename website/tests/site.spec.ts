import { expect, test } from '@playwright/test'

test('keeps the primary action visible on a short phone', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('./')

  const primaryAction = page.getByRole('link', { name: 'Get started with your agent', exact: true }).first()
  const actionBox = await primaryAction.boundingBox()
  const orbitBox = await page.locator('.hero-orbit canvas').boundingBox()
  expect(actionBox && actionBox.y + actionBox.height).toBeLessThanOrEqual(568)
  expect(orbitBox && orbitBox.width > 0 && orbitBox.height > 0).toBe(true)
  expect(orbitBox && Math.abs(orbitBox.x + orbitBox.width / 2 - 160)).toBeLessThanOrEqual(1)
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
  await page.getByRole('link', { name: 'Get started with your agent', exact: true }).first().click()
  await expect(page).toHaveURL(/\/#get-started$/)
  await expect(page.locator('main, [role="main"]')).toHaveCount(1)
  await page.getByRole('link', { name: 'Orbit', exact: true }).click()
  await expect(page).toHaveURL(/:\d+\/$/)
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
  const showcaseFlow = page.locator('.homepage-showcase-flow')
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  const lightBackground = await page.locator('html').evaluate((root) => getComputedStyle(root).getPropertyValue('--vp-c-bg'))
  const lightShowcaseBackground = await showcaseFlow.evaluate((flow) => getComputedStyle(flow).backgroundColor)

  await appearance.click()
  await expect(page.locator('html')).toHaveClass(/dark/)
  const darkBackground = await page.locator('html').evaluate((root) => getComputedStyle(root).getPropertyValue('--vp-c-bg'))
  const darkShowcaseBackground = await showcaseFlow.evaluate((flow) => getComputedStyle(flow).backgroundColor)

  expect(lightBackground.trim()).toBe('#ffffff')
  expect(darkBackground.trim()).toBe('#0d1117')
  expect(lightShowcaseBackground).not.toBe(darkShowcaseBackground)
})

test('uses dark mode on the first visit', async ({ page }) => {
  await page.goto('./')

  await expect(page.locator('html')).toHaveClass(/dark/)
  const background = await page.locator('html').evaluate((root) => getComputedStyle(root).getPropertyValue('--vp-c-bg'))
  expect(background.trim()).toBe('#0d1117')
})

test('renders a bounded animated hero and respects reduced motion', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('./')
  const hero = page.locator('.hero-orbit')
  const canvas = hero.locator('canvas')
  await expect(hero).toHaveAttribute('data-motion', 'running')
  const containerBox = await page.locator('.VPHomeHero .image-container').boundingBox()
  const heroBox = await hero.boundingBox()
  expect(containerBox && heroBox && Math.abs(heroBox.x + heroBox.width / 2 - containerBox.x - containerBox.width / 2)).toBeLessThanOrEqual(1)
  expect(containerBox && heroBox && Math.abs(heroBox.y + heroBox.height / 2 - containerBox.y - containerBox.height / 2)).toBeLessThanOrEqual(1)
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

test('renders a semantic localized showcase only on homepages', async ({ page }) => {
  await page.goto('./')
  const englishShowcase = page.getByRole('region', { name: 'From one request to verified behavior' })
  await expect(englishShowcase).toBeVisible()
  await expect(englishShowcase.locator('ol > li')).toHaveCount(5)
  await expect(englishShowcase.getByRole('link', { name: 'See the configuration contract' })).toHaveAttribute('href', '/docs/configuration')
  await expect(englishShowcase.getByRole('link', { name: 'Explore logs and traces' })).toHaveAttribute('href', '/docs/tracing')
  await expect(page.locator('main, [role="main"]')).toHaveCount(1)

  await englishShowcase.getByRole('link', { name: 'See the configuration contract' }).click()
  await expect(page).toHaveURL(/\/docs\/configuration$/)
  await expect(page.locator('.homepage-showcase')).toHaveCount(0)
  await page.getByRole('link', { name: 'Orbit', exact: true }).click()
  await expect(page).toHaveURL(/:\d+\/$/)
  await expect(englishShowcase).toHaveAttribute('data-motion', 'paused')
  await englishShowcase.scrollIntoViewIfNeeded()
  await expect(englishShowcase).toHaveAttribute('data-motion', 'running')

  await page.goto('./zh-TW/')
  const chineseShowcase = page.getByRole('region', { name: '從一個需求到可驗證的行為' })
  await expect(chineseShowcase).toBeVisible()
  await expect(chineseShowcase.locator('ol > li')).toHaveCount(5)
  await expect(chineseShowcase.getByRole('link', { name: '查看設定契約' })).toHaveAttribute('href', '/zh-TW/docs/configuration')
  await chineseShowcase.getByRole('link', { name: '探索 logs 與 traces' }).click()
  await expect(page).toHaveURL(/\/zh-TW\/docs\/tracing$/)
  await expect(page.locator('.homepage-showcase')).toHaveCount(0)

  await page.goto('./')
  await expect(page.getByRole('region', { name: 'From one request to verified behavior' })).toBeVisible()
})

test('bounds showcase motion and keeps the phone layout accessible', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('./')
  const showcase = page.getByRole('region', { name: 'From one request to verified behavior' })
  await showcase.scrollIntoViewIfNeeded()
  await expect(showcase).toHaveAttribute('data-motion', 'running')
  const initialActiveStage = await showcase.locator('.homepage-showcase-stage.is-active').getAttribute('data-stage')
  await expect.poll(() => showcase.locator('.homepage-showcase-stage.is-active').getAttribute('data-stage')).not.toBe(initialActiveStage)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  const showcaseBox = await showcase.boundingBox()
  expect(showcaseBox && showcaseBox.x >= 0 && showcaseBox.x + showcaseBox.width <= 320).toBe(true)

  const firstLink = showcase.getByRole('link').first()
  await firstLink.focus()
  await expect(firstLink).toBeFocused()
  expect(await firstLink.evaluate((link) => getComputedStyle(link).outlineStyle)).not.toBe('none')

  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(showcase).toHaveAttribute('data-motion', 'paused')
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(showcase).toHaveAttribute('data-motion', 'running')

  await page.evaluate(() => window.scrollTo(0, 0))
  await expect(showcase).toHaveAttribute('data-motion', 'paused')
  await showcase.scrollIntoViewIfNeeded()
  await expect(showcase).toHaveAttribute('data-motion', 'running')

  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(showcase).toHaveAttribute('data-motion', 'reduced')
  await expect(showcase.locator('.homepage-showcase-rail span').first()).toHaveCSS('display', 'none')
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
