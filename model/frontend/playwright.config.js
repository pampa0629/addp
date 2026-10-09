import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

export default defineConfig(withBrowserTestIsolation('model', {
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  reporter: 'line',
  use: {
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    viewport: { width: 900, height: 760 },
    actionTimeout: 10_000
  },
  expect: { timeout: 10_000 }
}))
