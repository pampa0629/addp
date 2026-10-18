import { test, expect } from '@playwright/test'

import { assertOntologyThemes } from './ontology-theme-assertions.js'

test('ontology custom colors, inheritance and readable labels follow live themes without losing the viewport', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/ontology/e2e/fixtures/ontology-theme.html')
  await assertOntologyThemes(page)
  await page.getByRole('button', { name: '切换画布' }).click()
  await expect(page.locator('canvas')).toHaveCount(0)
  await page.evaluate(() => { document.documentElement.className = 'dark' })
  await page.getByRole('button', { name: '切换画布' }).click()
  await expect(page.locator('canvas')).toBeVisible()
  expect(errors).toEqual([])
})
