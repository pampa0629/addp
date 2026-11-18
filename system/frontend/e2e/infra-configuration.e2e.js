import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { mockRaster } from './raster-policy.fixture'

test('platform Infra panel separates a Redis runtime update from persistence', async ({ page }) => {
  await mockRaster(page, { permissions: ['system.infra_configuration.read', 'system.infra_configuration.update'] })
  let value = '128'
  const reads = []
  const writes = []
  const parameter = () => ({ name: 'slowlog-max-len', value, unit: 'entries', source: 'redis_runtime', activation: 'online', editable: true, pending_restart: false })
  await page.route(/\/api\/v1\/system\/platform\/infra_configuration(?:\/.*)?$/, async route => {
    const method = route.request().method()
    const headers = {
      'access-control-allow-origin': route.request().headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true',
      'access-control-allow-methods': 'GET,POST,OPTIONS',
      'access-control-allow-headers': 'authorization,content-type'
    }
    if (method === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (method === 'GET') {
      reads.push(value)
      return route.fulfill({ headers, json: [
        { id: 'database', provider: 'postgres', status: 'unbound', parameters: [] },
        { id: 'redis', provider: 'redis', status: 'available', parameters: [
          parameter(),
          { name: 'maxmemory', value: '0', unit: 'bytes', source: 'redis_runtime', activation: 'native', editable: false, pending_restart: false }
        ] }
      ] })
    }
    writes.push(route.request().postDataJSON())
    value = writes.at(-1).value
    return route.fulfill({ headers, json: {
      application: 'confirmed', persistence: 'unconfirmed', failure_stage: 'persist', audit_recorded: true, current: parameter()
    } })
  })
  await page.goto('/configuration')
  await expect(page.getByText('此组件尚未配置管理凭据')).toBeVisible()
  await page.getByTestId('infra-component').click()
  await page.getByRole('option', { name: 'Redis', exact: true }).click()
  await expect(page.getByTestId('edit-maxmemory')).toHaveCount(0)
  await page.getByTestId('edit-slowlog-max-len').click()
  await page.getByRole('dialog').getByRole('textbox').fill('256')
  await page.getByTestId('infra-apply').click()
  const result = page.getByTestId('infra-outcome')
  await expect(result).toContainText('已核验生效')
  await expect(result).toContainText('未确认持久化')
  await expect(page.getByRole('row').filter({ hasText: 'slowlog-max-len' })).toContainText('256')
  expect(writes).toEqual([{ value: '256' }])
  expect(reads).toEqual(['128', '256'])
})

test('Tenant configuration has no Infra tab', async ({ page }) => {
  await mockRaster(page, { scope: 'tenant', permissions: ['system.engine_raster_policy.read', 'system.infra_configuration.read'] })
  await page.goto('/configuration?tab=infra-configuration')
  await expect(page.getByRole('tab', { name: '引擎配置' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '基础设施' })).toHaveCount(0)
})
