import { defineConfig } from '@playwright/test'
import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
export default defineConfig(withBrowserTestIsolation('monitor', {
  testDir: './e2e', workers: 1, reporter: 'line',
  use: { headless: true, viewport: { width: 1280, height: 800 }, screenshot: 'only-on-failure' }
}))
