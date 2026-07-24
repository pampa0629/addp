import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-develop-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4178',
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 1280, height: 800 },
    actionTimeout: 10_000
  },
  expect: { timeout: 10_000 },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4178 --strictPort',
    url: 'http://127.0.0.1:4178',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
