import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-quality-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4183',
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 1100, height: 780 }
  },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4183 --strictPort',
    url: 'http://127.0.0.1:4183',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
