import { expect, test } from '@playwright/test'
import { httpBackend, httpPermissions } from '../../../monitor/frontend/e2e/http-quality.fixture'
import { identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

test('HTTP quality keeps one Console iframe while selecting a template and restoring a deep link', async ({ page }) => {
  await httpBackend(page)
  await page.goto('/monitor/http-quality?refresh=off')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('http-instances')).toContainText('http-1')
  const origin = await frame.locator('body').evaluate(() => performance.timeOrigin)
  await frame.getByTestId('http-instances').getByRole('button', { name: '接口与趋势', exact: true }).first().click()
  await expect(page).toHaveURL(url => url.searchParams.get('instance_id') === '1')
  await frame.getByTestId('http-interfaces').getByRole('button', { name: 'GET /slow/:id', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('route') === '/slow/:id' && url.searchParams.get('method') === 'GET')
  await expect(frame.getByTestId('http-trend').locator('canvas')).toBeVisible()
  expect(await frame.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  const url = page.url(); await page.reload()
  await expect(frame.getByTestId('http-trend').locator('canvas')).toBeVisible()
  expect(page.url()).toBe(url)
})

test('Tenant HTTP permission strings cannot mount the platform quality iframe', async ({ page }) => {
  const state = await httpBackend(page, { identity: identity(httpPermissions, { type: 'tenant', tenant_id: 1 }) })
  await page.goto('/monitor/http-quality')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
  expect(state.calls).toEqual([])
})
