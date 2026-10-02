import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

const policy = { version: 1, failure_samples: 3, recovery_samples: 3, stale_seconds: 120, delay_ms: 15000, capacity_percent: 80, recovery_percent: 70 }
async function fixture(page) {
  await mockModuleQueryAPI(page, { pipelinePermission: true })
  const requests = []
  let failure = false, conflict = false, acknowledged = false
  await page.route('**/api/v1/monitor/platform/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const headers = { 'access-control-allow-origin': req.headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,DELETE,OPTIONS' }
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    const reply = (json, status = 200) => route.fulfill({ headers, json, status })
    const body = req.postDataJSON(); requests.push({ path, method: req.method(), body })
    if (path.endsWith('/log-pipeline')) {
      if (failure) return reply({ error: '观测不可用' }, 503)
      return reply({ configured: true, health: 'alert', notifications: 'unconfigured', node: { node: 'host-a', received_at: new Date().toISOString(), registry_valid: true, observation: { probe_delivered: true, probe_delay_ms: 100 } }, incidents: [{ id: 9, version: 3, signal: 'collector_dropped', severity: 'critical', status: acknowledged ? 'acknowledged' : 'open', opened_at: new Date().toISOString(), instance_id: '' }] })
    }
    if (path.endsWith('/policy')) return req.method() === 'GET' ? reply(policy) : conflict ? reply({ error: '规则版本冲突' }, 409) : reply({ ...body, version: 2 })
    if (path.endsWith('/acknowledge')) { acknowledged = true; return reply({ id: 9 }) }
    if (path.endsWith('/log-notification-deliveries')) return reply({ data: [], total: 0, page: 1, page_size: 20 })
    if (path.endsWith('/log-notification-destinations') && req.method() === 'GET') return reply([])
    if (path.endsWith('/log-notification-destinations')) return reply({ ...body, id: 1, version: 1 })
    if (path.endsWith('/credential')) return reply({ id: 1, version: 2, channel: 'webhook', enabled: false })
    if (path.endsWith('/log-notification-destinations/1')) return reply({ ...body, id: 1, version: 3 })
    throw new Error(`Unexpected platform request: ${req.method()} ${path}`)
  })
  await page.goto('/modules?tab=log-pipeline')
  await expect(page.getByTestId('pipeline-health')).toContainText('告警')
  return { requests, fail: () => { failure = true }, conflict: () => { conflict = true } }
}

test('platform permission hides pipeline entry', async ({ page }) => {
  await mockModuleQueryAPI(page)
  await page.goto('/modules')
  await expect(page.getByRole('tab', { name: '日志链路', exact: true })).toHaveCount(0)
})

test('alert acknowledgement uses current version and failed refresh becomes unknown', async ({ page }) => {
  const state = await fixture(page)
  await page.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page.getByTestId('pipeline-incidents')).toContainText('已确认')
  expect(state.requests.find(r => r.path.endsWith('/acknowledge')).body).toEqual({ version: 3 })
  state.fail()
  await page.getByTestId('log-pipeline').getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('pipeline-health')).toContainText('未知')
  await expect(page.getByText('观测不可用', { exact: true })).toBeVisible()
})

test('conflicting policy preserves edited fields', async ({ page }) => {
  const state = await fixture(page)
  await page.getByRole('button', { name: '检测规则', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('spinbutton').first().fill('5')
  state.conflict()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog.getByText('规则版本冲突', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('spinbutton').first()).toHaveValue('5')
})

test('webhook signing credential precedes enabling and stays out of target payload', async ({ page }) => {
  const state = await fixture(page)
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  await page.getByRole('button', { name: '新增通知目标', exact: true }).click()
  const dialog = page.getByRole('dialog').filter({ has: page.getByRole('textbox') })
  await dialog.getByRole('textbox').nth(0).fill('Local receiver')
  await dialog.getByRole('textbox').nth(1).fill('https://notify.example.test/hook')
  await dialog.locator('input[type=password]').fill('fixture-signing-secret')
  await dialog.locator('.el-switch').click()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).toBeHidden()
  const writes = state.requests.filter(r => r.method !== 'GET')
  expect(writes.map(r => r.method)).toEqual(['POST', 'PUT', 'PUT'])
  expect(writes[0].body.enabled).toBe(false)
  expect(writes[0].body.secret).toBeUndefined()
  expect(writes[1].body).toEqual({ version: 1, secret: 'fixture-signing-secret' })
  expect(writes[2].body.version).toBe(2)
  expect(writes[2].body.enabled).toBe(true)
  expect(writes[2].body.secret).toBeUndefined()
})
