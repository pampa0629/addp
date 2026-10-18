import { test, expect } from '@playwright/test'
import { expectVisibleGraphLoop } from '../../../common-frontend/graph/tests/loopBrowserAssertions.mjs'

for (const view of ['schema', 'result']) {
  test(`${view} keeps self relationships visible through theme changes`, async ({ page }) => {
    await page.goto(`/e2e/fixtures/graph-schema-theme.html?view=${view}&self=1`)
    const root = page.locator(view === 'result' ? '.graph-result-view' : '.graph-schema-view')
    await expect(root.locator('canvas')).toBeVisible()
    for (const theme of ['dark', 'light', 'blue', 'purple']) {
      await page.getByLabel('主题', { exact: true }).selectOption(theme)
      await expectVisibleGraphLoop(root, 'SELF_LINK', expect)
    }
  })
}
