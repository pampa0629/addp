import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

export default defineConfig(withBrowserTestIsolation('inference', {
  testDir: './e2e',
  testMatch: '**/*.e2e.js',
  reporter: 'line',
  workers: 1,
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure', viewport: { width: 1440, height: 1000 } }
}))
