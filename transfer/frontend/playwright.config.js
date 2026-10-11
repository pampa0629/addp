import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

export default defineConfig(withBrowserTestIsolation('transfer', {
  testDir: './e2e',
  testMatch: '**/*.spec.js',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  use: {
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    viewport: { width: 1280, height: 900 }
  }
}))
