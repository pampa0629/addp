import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { engineBackend, engineOwner, enginePermissions, engineID } from '../../../monitor/frontend/e2e/engine-resources.fixture'
import { inventory } from '../../../system/frontend/e2e/engine-deployments.fixture'
import { identity } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

for (const locale of ['zh-cn', 'en']) test(`engine deployment opens the Monitor owner and browser Back restores System (${locale})`, async ({ page }) => {
  const owner = { ...engineOwner, inventory, declared_node_id: engineOwner.node_id }
  const { calls } = await engineBackend(page, { locale, owner })
  const writes = []
  page.on('request', request => { if (['POST', 'PUT', 'DELETE'].includes(request.method()) && request.url().includes('/api/v1/')) writes.push({ method: request.method(), path: new URL(request.url()).pathname }) })
  await page.goto('/system/engine-deployments?search=Infra')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const row = frame.getByRole('row').filter({ hasText: owner.display_name })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: locale === 'en' ? 'View monitoring' : '查看监控', exact: true }).click()
  await expect(page).toHaveURL(`${browserTestOrigin('console')}/monitor/engine-resources?deployment_id=${engineID}`)
  await expect(frame.getByRole('heading', { name: owner.display_name, exact: true })).toBeVisible()
  await expect(frame.getByRole('heading', { name: locale === 'en' ? 'Total connections' : '总连接数', exact: true })).toBeVisible()
  expect(calls.some(path => path.endsWith('/engine_resource_observations'))).toBe(true)
  await page.goBack()
  await expect(page).toHaveURL(`${browserTestOrigin('console')}/system/engine-deployments?search=Infra`)
  await expect(frame.getByRole('textbox', { name: locale === 'en' ? 'Search deployment names' : '搜索部署名称', exact: true })).toHaveValue('Infra')
  await expect(frame.getByRole('cell', { name: owner.display_name, exact: true })).toBeVisible()
  expect(writes).toEqual([{ method: 'POST', path: '/api/v1/system/refresh' }])
})

test('engine deployment without observation permission has no monitoring entry', async ({ page }) => {
  await engineBackend(page, { owner: { ...engineOwner, inventory, declared_node_id: engineOwner.node_id }, auth: identity(enginePermissions.filter(permission => permission !== 'monitor.resource_observation.read')) })
  await page.goto('/system/engine-deployments')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByRole('cell', { name: engineOwner.display_name, exact: true })).toBeVisible()
  await expect(frame.getByRole('button', { name: '查看监控', exact: true })).toHaveCount(0)
})
