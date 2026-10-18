import { test, expect } from '@playwright/test'
import { expectVisibleGraphLoop } from '../../../common-frontend/graph/tests/loopBrowserAssertions.mjs'

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


test('ontology small graph stays at original scale after fitting and resizing while manual zoom remains available', async ({ page }) => {
  await page.goto('/ontology/e2e/fixtures/ontology-theme.html')
  const view = page.locator('.ontology-view')
  await expect(view.locator('canvas')).toBeVisible()
  const zoom = () => view.evaluate(element => element.__vueParentComponent.setupState.graphInstance.getZoom())
  await expect.poll(zoom).toBe(1)
  await view.getByRole('button', { name: '放大', exact: true }).click()
  expect(await zoom()).toBeGreaterThan(1)
  await view.getByRole('button', { name: '适应窗口', exact: true }).click()
  await expect.poll(zoom).toBe(1)
  await view.getByRole('button', { name: '放大', exact: true }).click()
  await page.setViewportSize({ width: 700, height: 600 })
  await expect.poll(zoom).toBeLessThanOrEqual(1)
  await expect.poll(() => view.locator('canvas').evaluate(element => Math.abs(parseFloat(element.style.width) - element.parentElement.clientWidth))).toBeLessThan(2)
})


test('ontology self relationships render outside their own node and retain themed arrows', async ({ page }) => {
  await page.goto('/ontology/e2e/fixtures/ontology-theme.html')
  const view = page.locator('.ontology-view')
  await expect(view.locator('canvas')).toBeVisible()
  await expectVisibleGraphLoop(view, 'Self reference', expect)
  await assertOntologyThemes(page)
  await expectVisibleGraphLoop(view, 'Self reference', expect)
})
