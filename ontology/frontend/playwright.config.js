import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
export default defineConfig({
  testDir: './e2e',
  workers: 1,
  timeout: 45_000,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-ontology-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4192',
    headless: true,
    viewport: { width: 1280, height: 900 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    actionTimeout: 10_000
  },
  webServer: {
    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4192',
    url: 'http://127.0.0.1:4192/ontology/',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
