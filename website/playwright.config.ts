import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  retries: 0,
  outputDir: '/tmp/orbit-docs-playwright-results',
  use: {
    baseURL: 'http://127.0.0.1:4173/orbit/',
    browserName: 'chromium',
    launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH },
  },
  webServer: {
    command: 'pnpm run preview --host 127.0.0.1 --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173/orbit/',
    reuseExistingServer: false,
  },
})
