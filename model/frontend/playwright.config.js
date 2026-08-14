import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-model-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4182',
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 900, height: 760 },
    actionTimeout: 10_000
  },
  expect: { timeout: 10_000 },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4182 --strictPort',
    url: 'http://127.0.0.1:4182',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
