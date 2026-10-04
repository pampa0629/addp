import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
export default defineConfig({
  testDir: './e2e', workers: 1, reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-monitor-playwright-results'),
  use: { baseURL: 'http://127.0.0.1:4179', headless: true, viewport: { width: 1280, height: 800 }, screenshot: 'only-on-failure' },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4179 --strictPort --base /',
    url: 'http://127.0.0.1:4179', reuseExistingServer: false, gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 }, timeout: 30000
  }
})
