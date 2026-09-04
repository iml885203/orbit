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
  await expect(englishShowcase.getByText('Read orbit.dotw.me and get this project running.')).toBeAttached()
  await expect(englishShowcase.getByText('Orbit found the project environment. Starting its dependencies now.')).toBeAttached()
  await expect(englishShowcase.locator('.showcase-node')).toHaveCount(6)
  await expect(englishShowcase.locator('.showcase-app-bar')).toContainText('Orbit')
  await expect(englishShowcase.locator('.showcase-connected')).toHaveText('Connected')
  await expect(englishShowcase.locator('.showcase-nav-active')).toHaveText('Services')
  await expect(englishShowcase.locator('.showcase-edge > path')).toHaveCount(5)
  await expect(englishShowcase.getByText('API depends on PostgreSQL and Redis')).toBeAttached()
  await expect(englishShowcase.getByRole('link', { name: 'Explore the dashboard workflow' })).toHaveAttribute('href', '/docs/tracing')
  await expect(englishShowcase.locator('input, button, img, video, canvas, iframe')).toHaveCount(0)
  await expect(englishShowcase.locator('a')).toHaveCount(1)
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
  const partialRequest = await showcase.getAttribute('data-typed-request')
  expect(partialRequest).toBeTruthy()
  expect('Read orbit.dotw.me and get this project running.'.startsWith(partialRequest!)).toBe(true)
  expect(partialRequest).not.toBe('Read orbit.dotw.me and get this project running.')
  await expect(showcase.locator('.showcase-dashboard-health')).toHaveCount(0)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(showcase).toHaveAttribute('data-motion', 'paused')
  const pausedRequest = await showcase.getAttribute('data-typed-request')
  await page.waitForTimeout(1200)
  await expect(showcase).toHaveAttribute('data-scene', '0')
  await expect(showcase).toHaveAttribute('data-typed-request', pausedRequest!)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(showcase).toHaveAttribute('data-scene', '1', { timeout: 2500 })
  await expect(showcase).toHaveAttribute('data-typed-request', 'Read orbit.dotw.me and get this project running.')
  await expect(showcase.locator('.showcase-message-user')).toHaveCSS('opacity', '0')
  await expect(showcase.locator('.showcase-dashboard')).toHaveCSS('opacity', '0')
  const initialConversationBox = await showcase.locator('.showcase-conversation').boundingBox()
  const initialDemoBox = await showcase.locator('.showcase-demo').boundingBox()
  const initialDashboardBox = await showcase.locator('.showcase-dashboard').boundingBox()
  expect(initialConversationBox && initialDemoBox && initialConversationBox.width >= initialDemoBox.width - 2).toBe(true)
  expect(initialConversationBox && initialDemoBox && initialConversationBox.height >= initialDemoBox.height - 4).toBe(true)
  expect(initialDemoBox?.height).toBeGreaterThanOrEqual(458)
  await expect(showcase.locator('.showcase-dashboard')).toHaveCSS('max-height', '0px')
  expect(initialDashboardBox?.height).toBeLessThanOrEqual(1)
  await expect(showcase.locator('.showcase-resource-count')).toBeHidden()
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  const sendIndicator = showcase.locator('.showcase-send-indicator')
  await expect(sendIndicator).toHaveCSS('animation-name', 'showcase-send')
  await expect(sendIndicator).toHaveCSS('animation-play-state', 'paused')
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
  }))
  const pausedSendTransform = await sendIndicator.evaluate((element) => getComputedStyle(element).transform)
  await page.waitForTimeout(300)
  await expect(sendIndicator).toHaveCSS('transform', pausedSendTransform)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(showcase).toHaveAttribute('data-scene', '2', { timeout: 2500 })
  await expect(showcase.locator('.showcase-message-user')).toHaveCSS('opacity', '1')
  await expect(showcase.locator('.showcase-message-user')).toHaveCSS('animation-name', 'showcase-reveal')
  await expect(showcase.locator('.showcase-message-agent')).toHaveCSS('opacity', '0')
  await expect(showcase).toHaveAttribute('data-scene', '3', { timeout: 2500 })
  await expect(showcase.locator('.showcase-message-agent')).toHaveCSS('opacity', '1')
  await expect(showcase.locator('.showcase-message-agent')).toHaveCSS('animation-name', 'showcase-reveal')
  const userBox = await showcase.locator('.showcase-message-user').boundingBox()
  const agentBox = await showcase.locator('.showcase-message-agent').boundingBox()
  expect(userBox && agentBox && userBox.x + userBox.width > agentBox.x + agentBox.width).toBe(true)
  expect(userBox && agentBox && agentBox.x < userBox.x).toBe(true)
  await expect(showcase).toHaveAttribute('data-scene', '4', { timeout: 2500 })
  await expect(showcase.locator('.showcase-dashboard')).toHaveCSS('opacity', '1')
  await expect(showcase.locator('.showcase-dashboard')).toHaveCSS('animation-name', 'none')
  const expandedDashboardBox = await showcase.locator('.showcase-dashboard').boundingBox()
  expect(expandedDashboardBox?.height).toBeGreaterThan(500)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  const showcaseBox = await showcase.boundingBox()
  expect(showcaseBox && showcaseBox.x >= 0 && showcaseBox.x + showcaseBox.width <= 320).toBe(true)

  const firstLink = showcase.getByRole('link').first()
  await firstLink.focus()
  await expect(firstLink).toBeFocused()
  expect(await firstLink.evaluate((link) => getComputedStyle(link).outlineStyle)).not.toBe('none')

  await page.evaluate(() => window.scrollTo(0, 0))
  await expect(showcase).toHaveAttribute('data-motion', 'paused')
  const pausedScene = await showcase.getAttribute('data-scene')
  await page.waitForTimeout(1400)
  await expect(showcase).toHaveAttribute('data-scene', pausedScene!)
  await showcase.scrollIntoViewIfNeeded()
  await expect(showcase).toHaveAttribute('data-motion', 'running')
  await expect(showcase).toHaveAttribute('data-scene', '5', { timeout: 2500 })
  await expect(showcase.locator('.showcase-node.kind-infra.is-healthy')).toHaveCount(3)
  await expect(showcase.locator('.showcase-node.kind-backend.is-healthy')).toHaveCount(0)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(0)
  const backendTint = await showcase.locator('.showcase-node.kind-backend').first().evaluate((element) => getComputedStyle(element).backgroundColor)
  await expect(showcase).toHaveAttribute('data-scene', '6', { timeout: 2500 })
  await expect(showcase.locator('.showcase-node.kind-backend.is-healthy')).toHaveCount(2)
  await expect(showcase.locator('.showcase-node.kind-frontend.is-healthy')).toHaveCount(0)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(4)
  await expect(showcase.locator('.showcase-node.kind-backend').first()).toHaveCSS('background-color', backendTint)
  await expect(showcase).toHaveAttribute('data-scene', '7', { timeout: 6000 })
  await expect(showcase).toHaveAttribute('data-motion', 'complete')
  await expect(showcase.locator('.showcase-node.is-healthy')).toHaveCount(6)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(5)
  await expect(showcase.locator('.showcase-dashboard-health')).toHaveText('Healthy')
  await expect(showcase.getByRole('status')).toHaveText('Environment ready · 6 nodes healthy')
  for (const node of await showcase.locator('.showcase-node').all()) {
    await expect(node.locator('strong')).toBeVisible()
    await expect(node.locator('.showcase-node-status')).toBeVisible()
    if (await node.locator('.showcase-node-kind').count()) await expect(node.locator('.showcase-node-kind')).toBeVisible()
    const nodeBox = await node.boundingBox()
    for (const content of [node.locator('strong'), node.locator('.showcase-node-status')]) {
      const contentBox = await content.boundingBox()
      expect(contentBox && nodeBox && contentBox.x >= nodeBox.x && contentBox.x + contentBox.width <= nodeBox.x + nodeBox.width).toBe(true)
    }
  }
  for (const relationship of await showcase.locator('.showcase-relationships li').all()) await expect(relationship).toBeVisible()
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(showcase.locator('.showcase-flow-dot').first()).toHaveCSS('animation-play-state', 'paused')
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await page.waitForTimeout(1400)
  await expect(showcase).toHaveAttribute('data-scene', '7')

  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(showcase).toHaveAttribute('data-motion', 'reduced')
  await expect(showcase).toHaveAttribute('data-scene', '7')
})

test('shows the complete static graph when reduced motion is set before entry', async ({ browser }) => {
  const page = await browser.newPage({ reducedMotion: 'reduce', viewport: { width: 320, height: 568 } })
  await page.goto('./')
  const showcase = page.getByRole('region', { name: 'Ask once. See the whole environment come alive.' })
  await showcase.scrollIntoViewIfNeeded()
  await expect(showcase).toHaveAttribute('data-motion', 'reduced')
  await expect(showcase).toHaveAttribute('data-scene', '7')
  await expect(showcase.locator('.showcase-node.is-healthy')).toHaveCount(6)
  await expect(showcase.locator('.showcase-edge.is-active')).toHaveCount(5)
  await expect(showcase.locator('.showcase-dashboard-health')).toHaveText('Healthy')
  await expect(showcase.locator('.showcase-composer')).toHaveCSS('opacity', '0')
  await expect(showcase.locator('.showcase-relationship-marker')).toHaveCount(3)
  await expect(showcase.locator('.showcase-relationship-marker').first()).toBeVisible()
  await page.close()
})

test('matches Orbit dashboard tokens and node geometry in both website themes', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('./')
  const showcase = page.locator('.homepage-showcase')
  const dashboard = showcase.locator('.showcase-dashboard')
  const node = showcase.locator('.showcase-node.kind-frontend').first()
  const desktopUserBox = await showcase.locator('.showcase-message-user').boundingBox()
  const desktopAgentBox = await showcase.locator('.showcase-message-agent').boundingBox()
  expect(desktopUserBox && desktopAgentBox && desktopUserBox.x + desktopUserBox.width > desktopAgentBox.x + desktopAgentBox.width).toBe(true)
  expect(desktopUserBox && desktopAgentBox && desktopAgentBox.x < desktopUserBox.x).toBe(true)
  const tokens = await dashboard.evaluate((element) => {
    const style = getComputedStyle(element)
    return Object.fromEntries([
      '--showcase-bg', '--showcase-card', '--showcase-border', '--showcase-fg', '--showcase-dim',
      '--showcase-green', '--showcase-yellow', '--showcase-blue', '--showcase-frontend', '--showcase-backend',
    ].map((token) => [token, style.getPropertyValue(token).trim()]))
  })
  expect(tokens).toEqual({
    '--showcase-bg': '#0d1117', '--showcase-card': '#161b22', '--showcase-border': '#30363d',
    '--showcase-fg': '#e6edf3', '--showcase-dim': '#8b949e', '--showcase-green': '#3fb950',
    '--showcase-yellow': '#d29922', '--showcase-blue': '#58a6ff', '--showcase-frontend': '#a371f7',
    '--showcase-backend': '#39c5cf',
  })
  await expect(dashboard).toHaveCSS('background-color', 'rgb(13, 17, 23)')
  await expect(dashboard).toHaveCSS('color', 'rgb(230, 237, 243)')
  await expect(node).toHaveCSS('width', '240px')
  await expect(node).toHaveCSS('min-height', '92px')
  await expect(node).toHaveCSS('border-radius', '8px')
  await expect(node).toHaveCSS('border-top-width', '1px')
  expect(await node.evaluate((element) => getComputedStyle(element).fontFamily)).toContain('ui-monospace')
  await expect(node.locator('.showcase-node-row')).toHaveCount(2)
  await expect(node.locator('.showcase-node-status')).toHaveCSS('color', 'rgb(63, 185, 80)')
  const cardColor = 'rgb(22, 27, 34)'
  const kindBackgrounds = await Promise.all(['frontend', 'backend', 'infra'].map((kind) =>
    showcase.locator(`.showcase-node.kind-${kind}`).first().evaluate((element) => getComputedStyle(element).backgroundColor)))
  expect(new Set(kindBackgrounds).size).toBe(3)
  for (const background of kindBackgrounds) expect(background).not.toBe(cardColor)
  await expect(showcase.locator('.showcase-nav-active')).toHaveCSS('color', 'rgb(88, 166, 255)')
  await expect(showcase.locator('.showcase-view-switch .is-selected')).toHaveCSS('color', 'rgb(88, 166, 255)')
  for (const kind of ['frontend', 'backend', 'infra']) {
    const ratios = await showcase.locator(`.showcase-node.kind-${kind}`).first().evaluate((element) => {
      const context = document.createElement('canvas').getContext('2d')!
      context.canvas.width = 1
      context.canvas.height = 1
      const rgb = (color: string) => {
        context.clearRect(0, 0, 1, 1)
        context.fillStyle = color
        context.fillRect(0, 0, 1, 1)
        return Array.from(context.getImageData(0, 0, 1, 1).data.slice(0, 3))
      }
      const luminance = (color: number[]) => {
        const linear = color.map((channel) => {
          const value = channel / 255
          return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
        })
        return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2]
      }
      const background = luminance(rgb(getComputedStyle(element).backgroundColor))
      const selectors = ['.showcase-node-status', '.showcase-node-meta']
      if (element.querySelector('.showcase-node-kind')) selectors.push('.showcase-node-kind')
      if (element.querySelector('.showcase-node-infra-icon')) selectors.push('.showcase-node-infra-icon')
      return selectors.map((selector) => {
        const foreground = luminance(rgb(getComputedStyle(element.querySelector(selector)!).color))
        return (Math.max(background, foreground) + 0.05) / (Math.min(background, foreground) + 0.05)
      })
    })
    for (const ratio of ratios) expect(ratio).toBeGreaterThanOrEqual(4.5)
  }
  const liveBox = await showcase.locator('.showcase-live').boundingBox()
  const webBox = await showcase.locator('.showcase-node.node-web').boundingBox()
  expect(liveBox && webBox && (liveBox.x + liveBox.width <= webBox.x || liveBox.x >= webBox.x + webBox.width || liveBox.y + liveBox.height <= webBox.y || liveBox.y >= webBox.y + webBox.height)).toBe(true)
  await expect(showcase.locator('.showcase-edge').first().locator('path')).toHaveCSS('stroke', 'rgb(139, 148, 158)')
  await expect(showcase.locator('.showcase-flow-dot')).toHaveCount(5)
  await expect(showcase.locator('.showcase-flow-dot').first()).toHaveCSS('animation-name', 'none')
  await expect(showcase.locator('.showcase-node-meta > span').first()).toHaveAttribute('aria-hidden', 'true')
  await expect(showcase.locator('.showcase-node-actions svg').first()).toHaveCSS('width', '15px')
  await expect(showcase.locator('.showcase-node-infra-icon')).toHaveCount(3)
  const lightBackground = await dashboard.evaluate((element) => getComputedStyle(element).backgroundColor)
  await page.getByRole('switch', { name: 'Switch to light theme' }).click()
  await expect(dashboard).toHaveCSS('background-color', lightBackground)
})

test('keeps the localized send and reply alignment in Traditional Chinese', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('./zh-TW/')
  const showcase = page.getByRole('region', { name: '問一次，看見整個環境依序啟動。' })
  await showcase.scrollIntoViewIfNeeded()
  const partialRequest = await showcase.getAttribute('data-typed-request')
  expect(partialRequest).toBeTruthy()
  expect('閱讀 orbit.dotw.me，幫我把這個專案跑起來。'.startsWith(partialRequest!)).toBe(true)
  await expect(showcase).toHaveAttribute('data-scene', '2', { timeout: 4500 })
  await expect(showcase.locator('.showcase-message-agent')).toHaveCSS('opacity', '0')
  await expect(showcase).toHaveAttribute('data-scene', '3', { timeout: 2500 })
  const userBox = await showcase.locator('.showcase-message-user').boundingBox()
  const agentBox = await showcase.locator('.showcase-message-agent').boundingBox()
  expect(userBox && agentBox && userBox.x + userBox.width > agentBox.x + agentBox.width).toBe(true)
  expect(userBox && agentBox && agentBox.x < userBox.x).toBe(true)
})

test('reflows the dashboard without clipping at an intermediate viewport', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 768, height: 900 })
  await page.goto('./')
  const graph = page.locator('.showcase-graph')
  const graphBox = await graph.boundingBox()
  for (const node of await graph.locator('.showcase-node').all()) {
    await expect(node).toBeVisible()
    const box = await node.boundingBox()
    expect(box && graphBox && box.x >= graphBox.x && box.x + box.width <= graphBox.x + graphBox.width).toBe(true)
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(768)

  await page.setViewportSize({ width: 1024, height: 900 })
  const dashboardWidths = await page.locator('.showcase-dashboard').evaluate((element) => ({
    client: element.clientWidth,
    scroll: element.scrollWidth,
  }))
  expect(dashboardWidths.scroll).toBeLessThanOrEqual(dashboardWidths.client)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1024)
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
