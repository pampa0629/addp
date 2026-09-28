import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-workbench-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4190',
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    viewport: { width: 1280, height: 900 },
    actionTimeout: 10_000,
  },
  expect: { timeout: 10_000 },
  webServer: [{
    command: 'ADDP_E2E=1 VITE_ADDP_FRONTEND_PORTS=workbench:4190 VITE_ADDP_CONSOLE_PORT=4170 npm run dev -- --host 127.0.0.1 --port 4190 --strictPort',
    url: 'http://127.0.0.1:4190/module-ui/workbench/',
    reuseExistingServer: false,
    timeout: 30_000,
  }, {
    command: 'ADDP_E2E=1 WORKBENCH_FE_PORT=4190 npm --prefix ../../console/frontend run dev -- --host 127.0.0.1 --port 4170 --strictPort',
    url: 'http://127.0.0.1:4170/data-apps/fixture',
    reuseExistingServer: false,
    timeout: 30_000,
  }],
})
