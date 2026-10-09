import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

export default defineConfig(withBrowserTestIsolation('standard', {
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  use: {
    headless: true,
    screenshot: 'only-on-failure',
    viewport: { width: 900, height: 760 }
  }
}))
