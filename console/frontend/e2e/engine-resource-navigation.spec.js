import { expect, test } from '@playwright/test'
import { engineBackend, engineOwner } from '../../../monitor/frontend/e2e/engine-resources.fixture'

for (const locale of ['zh-cn', 'en']) test(`engine automatic filters retain one Console iframe and owner query (${locale})`, async ({ page }) => {
  await engineBackend(page, { locale })
  const queries = []
  await page.route('**/api/v1/system/platform/engine_deployments?**', route => {
    const query = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(query)
    return route.fulfill({ json: { data: [engineOwner], page: Number(query.page), page_size: 20, total: 21 } })
  })
  await page.clock.install()
  await page.goto('/monitor/engine-resources?page=2&refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('engine-list')).toContainText('1.00 GiB')
  const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin)
  const input = monitor.getByRole('textbox', { name: locale === 'en' ? 'Search deployment name' : '搜索部署名称', exact: true })
  await expect(monitor.getByRole('button', { name: locale === 'en' ? 'Search' : '搜索', exact: true })).toHaveCount(0)
  await input.fill('P'); await input.fill('PostgreSQL')
  expect(queries).toHaveLength(1)
  await page.clock.fastForward(301)
  await expect.poll(() => queries.length).toBe(2)
  await expect(page).toHaveURL('/monitor/engine-resources?search=PostgreSQL&refresh=off')
  expect(queries.at(-1)).toEqual({ page: '1', page_size: '20', search: 'PostgreSQL' })
  await input.hover(); await monitor.locator('form .el-input__clear').click()
  await expect.poll(() => queries.length).toBe(3)
  await expect(page).toHaveURL('/monitor/engine-resources?refresh=off')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
})
