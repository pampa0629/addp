import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-orchestrator-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4177',
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 1280, height: 800 }
  },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4177 --strictPort',
    url: 'http://127.0.0.1:4177',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
