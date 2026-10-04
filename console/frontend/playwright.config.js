import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

export default defineConfig({
  testDir: './e2e',
  testIgnore: '**/online/**',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  outputDir: resolve(tmpdir(), 'addp-console-playwright-results'),
  use: {
    baseURL: 'http://127.0.0.1:4170',
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 1280, height: 800 }
  },
  webServer: [{
    command: 'ADDP_E2E=1 VITE_ADDP_CONSOLE_PORT=4170 VITE_ADDP_FRONTEND_PORTS=catalog:4190,system:4173 npm run dev -- --host 127.0.0.1 --port 4170 --strictPort',
    url: 'http://127.0.0.1:4170/e2e/fixtures/auth-fixture.html?role=health',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
    timeout: 30_000
  }, {
    command: 'ADDP_E2E=1 npm --prefix ../../service/frontend run dev -- --host 127.0.0.1 --port 4180 --strictPort --base /e2e/service-fixture/',
    url: 'http://127.0.0.1:4180/e2e/service-fixture/e2e/query-form.html',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
    timeout: 30_000
  }, {
    command: 'ADDP_E2E=1 VITE_ADDP_CONSOLE_PORT=4170 CATALOG_FE_PORT=4190 npm --prefix ../../catalog/frontend run dev -- --host 127.0.0.1 --port 4190 --strictPort',
    url: 'http://127.0.0.1:4190/module-ui/catalog/login',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
    timeout: 30_000
  }, {
    command: 'ADDP_E2E=1 VITE_ADDP_CONSOLE_PORT=4170 npm --prefix ../../system/frontend run dev -- --host 127.0.0.1 --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173/module-ui/system/login',
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
    timeout: 30_000
  }]
})
