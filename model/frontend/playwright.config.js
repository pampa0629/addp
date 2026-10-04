import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  reporter: 'line',
  outputDir: resolve(process.env.RUNNER_TEMP || tmpdir(), 'addp-model-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4182',
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    viewport: { width: 900, height: 760 },
    actionTimeout: 10_000
  },
  expect: { timeout: 10_000 },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4182 --strictPort --base /',
    url: 'http://127.0.0.1:4182',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
    timeout: 30_000
  }
})
