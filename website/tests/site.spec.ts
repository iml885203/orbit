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
  const showcaseFlow = page.locator('.showcase-demo')
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

test('renders a semantic localized agent-to-dashboard showcase only on homepages', async ({ page }) => {
  await page.addInitScript(() => {
    const NativeIntersectionObserver = window.IntersectionObserver
    const callbacks: IntersectionObserverCallback[] = []
    Object.assign(window, { __showcaseObserverCallbacks: callbacks })
    window.IntersectionObserver = class extends NativeIntersectionObserver {
      constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
        super(callback, options)
        callbacks.push(callback)
      }
    }
  })
  await page.goto('./')
  const englishShowcase = page.getByRole('region', { name: 'Ask once. See the whole environment come alive.' })
  await expect(englishShowcase).toBeVisible()
  await expect(englishShowcase.getByText('Read orbit.dotw.me and get this project running.')).toBeVisible()
  await expect(englishShowcase.getByText('Orbit found the project environment. Starting its dependencies now.')).toBeAttached()
  await expect(englishShowcase.locator('.showcase-node')).toHaveCount(6)
  await expect(englishShowcase.getByText('API depends on PostgreSQL and Redis')).toBeAttached()
  await expect(englishShowcase.getByRole('link', { name: 'Explore the dashboard workflow' })).toHaveAttribute('href', '/docs/tracing')
  await expect(englishShowcase.locator('input, button, img, video, canvas, iframe')).toHaveCount(0)
  await expect(page.locator('main, [role="main"]')).toHaveCount(1)
  await page.evaluate(() => {
    const state = window as typeof window & {
      __oldShowcase?: Element
      __showcaseObserverCallback?: IntersectionObserverCallback
      __showcaseObserverCallbacks?: IntersectionObserverCallback[]
    }
    state.__oldShowcase = document.querySelector('.homepage-showcase') ?? undefined
    state.__showcaseObserverCallback = state.__showcaseObserverCallbacks?.at(-1)
  })

  await englishShowcase.getByRole('link', { name: 'Explore the dashboard workflow' }).click()
  await expect(page).toHaveURL(/\/docs\/tracing$/)
  await expect(page.locator('.homepage-showcase')).toHaveCount(0)
  await page.getByRole('link', { name: 'Orbit', exact: true }).click()
  await expect(page).toHaveURL(/:\d+\/$/)
  await expect(englishShowcase).toHaveAttribute('data-motion', 'paused')
  await page.evaluate(() => {
    const state = window as typeof window & {
      __oldShowcase?: Element
      __showcaseObserverCallback?: IntersectionObserverCallback
    }
    if (state.__oldShowcase && state.__showcaseObserverCallback) {
      state.__showcaseObserverCallback([
        { target: state.__oldShowcase, isIntersecting: true } as IntersectionObserverEntry,
      ], {} as IntersectionObserver)
    }
  })
  await expect(englishShowcase).toHaveAttribute('data-motion', 'paused')
  await englishShowcase.scrollIntoViewIfNeeded()
  await expect(englishShowcase).toHaveAttribute('data-motion', 'running')
  await expect(englishShowcase).toHaveAttribute('data-scene', '0')

  await page.getByRole('button', { name: 'Change language' }).click()
  await page.getByRole('banner').getByRole('link', { name: '繁體中文' }).click()
  await expect(page).toHaveURL(/\/zh-TW\/$/)
  const chineseShowcase = page.getByRole('region', { name: '問一次，看見整個環境依序啟動。' })
  await expect(chineseShowcase).toBeVisible()
  await expect(chineseShowcase).toHaveAttribute('data-scene', '0')
  await expect(chineseShowcase.getByText('閱讀 orbit.dotw.me，幫我把這個專案跑起來。')).toBeVisible()
  await expect(chineseShowcase.getByText('API 依賴 PostgreSQL 與 Redis')).toBeAttached()
  await chineseShowcase.getByRole('link', { name: '探索 dashboard workflow' }).click()
  await expect(page).toHaveURL(/\/zh-TW\/docs\/tracing$/)
  await expect(page.locator('.homepage-showcase')).toHaveCount(0)

  await page.goto('./')
  await expect(page.getByRole('region', { name: 'Ask once. See the whole environment come alive.' })).toBeVisible()
})

test('plays the dashboard sequence once and keeps the phone layout accessible', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('./')
  const showcase = page.getByRole('region', { name: 'Ask once. See the whole environment come alive.' })
  await showcase.scrollIntoViewIfNeeded()
  await expect(showcase).toHaveAttribute('data-motion', 'running')
  await expect(showcase).toHaveAttribute('data-scene', '0')
  await expect(showcase).toHaveAttribute('data-scene', '1', { timeout: 2500 })
  await expect(showcase.locator('.showcase-message-agent')).toHaveCSS('opacity', '1')
  await expect(showcase.locator('.showcase-dashboard')).toHaveCSS('opacity', '0')
  await expect(showcase).toHaveAttribute('data-scene', '2', { timeout: 2500 })
  await expect(showcase.locator('.showcase-dashboard')).toHaveCSS('opacity', '1')
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
  const pausedScene = await showcase.getAttribute('data-scene')
  await page.waitForTimeout(1400)
  await expect(showcase).toHaveAttribute('data-scene', pausedScene!)
  await showcase.scrollIntoViewIfNeeded()
  await expect(showcase).toHaveAttribute('data-motion', 'running')
  await expect(showcase).toHaveAttribute('data-scene', '3', { timeout: 2500 })
  await expect(showcase.locator('.showcase-node.kind-infra.is-healthy')).toHaveCount(3)
  await expect(showcase.locator('.showcase-node.kind-backend.is-healthy')).toHaveCount(0)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(0)
  await expect(showcase).toHaveAttribute('data-scene', '4', { timeout: 2500 })
  await expect(showcase.locator('.showcase-node.kind-backend.is-healthy')).toHaveCount(2)
  await expect(showcase.locator('.showcase-node.kind-frontend.is-healthy')).toHaveCount(0)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(4)
  await expect(showcase).toHaveAttribute('data-scene', '5', { timeout: 6000 })
  await expect(showcase).toHaveAttribute('data-motion', 'complete')
  await expect(showcase.locator('.showcase-node.is-healthy')).toHaveCount(6)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(5)
  await expect(showcase.getByRole('status')).toHaveText('Environment ready · 6 nodes healthy')
  await page.waitForTimeout(1400)
  await expect(showcase).toHaveAttribute('data-scene', '5')

  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(showcase).toHaveAttribute('data-motion', 'reduced')
  await expect(showcase).toHaveAttribute('data-scene', '5')
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
