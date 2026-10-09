import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
export default defineConfig(withBrowserTestIsolation('ontology', {
  testDir: './e2e',
  workers: 1,
  timeout: 45_000,
  reporter: 'line',
  use: {
    headless: true,
    viewport: { width: 1280, height: 900 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    actionTimeout: 10_000
  }
}))
