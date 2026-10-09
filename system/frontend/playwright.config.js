import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

export default defineConfig(withBrowserTestIsolation('system', {
  testDir: './e2e',
  testMatch: '**/*.e2e.js',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  use: {
    headless: true,
    locale: 'zh-CN',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    viewport: { width: 1440, height: 900 }
  }
}))
