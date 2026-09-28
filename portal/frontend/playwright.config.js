import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-portal-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4191',
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 1280, height: 800 }
  },
  webServer: {
    command: 'PORTAL_FE_PORT=4191 npm run dev -- --host 127.0.0.1 --port 4191 --strictPort',
    url: 'http://127.0.0.1:4191/portal/',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
