import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { checkLabeledCheckbox } from './online/transfer-browser-support.js'

for (const locale of ['en', 'zh-cn']) {
  for (const checked of [false, true]) {
    test(`real Meta dialog checks the visible label and submits once (${locale}, initially ${checked})`, async ({ page }) => {
      const apiCalls = []
      await page.route(url => url.pathname.startsWith('/api/'), async route => {
        apiCalls.push(route.request().url())
        await route.abort()
      })
      await page.goto(`${browserTestOrigin('meta')}/module-ui/meta/e2e/manual-scan.html?lang=${locale}&checked=${checked}`)
      const dialog = page.getByRole('dialog', { name: /^(确认扫描范围与深度|Confirm scan scope and depth)$/i })
      await expect(dialog).toBeVisible()
      const checkbox = dialog.getByRole('checkbox')
      // This is Element Plus's real styled control, not an artificial hidden-input stub.
      await expect(checkbox).not.toBeVisible()
      expect(await checkbox.isChecked()).toBe(checked)
      await expect(checkbox.locator('xpath=ancestor::label[1]')).toBeVisible()
      await checkLabeledCheckbox(checkbox)
      await expect(checkbox).toBeChecked()
      await expect(page.getByTestId('manual-scan-submissions')).toHaveText('[]')
      await dialog.getByRole('button', { name: /^(确认并开始扫描|Confirm and start scan)$/i }).click()
      await expect.poll(async () => JSON.parse(await page.getByTestId('manual-scan-submissions').textContent())).toEqual([{
        engine_id: 33, scan_depth: 'basic', force: true, trigger_type: 'manual', source: 'meta', catalog_paths: ['samples']
      }])
      expect(apiCalls).toEqual([])
    })
  }
}
