import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

const policy = { version: 1, failure_samples: 3, recovery_samples: 3, stale_seconds: 120, delay_ms: 15000, capacity_percent: 80, recovery_percent: 70 }
const deliveryView = {
  id: '626089da-5030-4ce5-9ce9-13c791f562f4', event_id: '80ff1d2a-213b-4cec-99a1-15458c463bb9',
  destination_id: 7, destination_name: '平台值班接收端', destination_version: 3,
  channel: 'webhook', status: 'dead', attempt_count: 8, cycle_attempt_count: 8, manual_retry_count: 0,
  event_type: 'opened', occurred_at: '2026-10-01T08:00:00Z', incident_id: 9, incident_status: 'resolved',
  created_at: '2026-10-01T08:00:01Z', last_error: 'notification_attempt_limit'
}
async function fixture(page, { destinations = [], deliveries = [], deliveryTotal = deliveries.length, management = true, retryConflict = false } = {}) {
  await mockModuleQueryAPI(page, { pipelinePermission: true, pipelineManagement: management })
  const requests = []
  let failure = false, conflict = false, acknowledged = false, deploymentState = 'enabled'
  await page.route('**/api/v1/monitor/platform/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const headers = { 'access-control-allow-origin': req.headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,DELETE,OPTIONS' }
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    const reply = (json, status = 200) => route.fulfill({ headers, json, status })
    const body = req.postDataJSON(); requests.push({ path, query: Object.fromEntries(new URL(req.url()).searchParams), method: req.method(), body })
    if (path.endsWith('/log-pipeline')) {
      if (failure) return reply({ error: '观测不可用' }, 503)
      return reply({ deployment_state: deploymentState, configured: true, health: deploymentState === 'enabled' ? 'alert' : 'unknown', notifications: 'unconfigured', node: { node: 'host-a', received_at: new Date().toISOString(), registry_valid: true, observation: { probe_delivered: true, probe_delay_ms: 100 } }, incidents: [{ id: 9, version: 3, signal: 'collector_dropped', severity: 'critical', status: acknowledged ? 'acknowledged' : 'open', opened_at: new Date().toISOString(), instance_id: '' }] })
    }
    if (path.endsWith('/policy')) return req.method() === 'GET' ? reply(policy) : conflict ? reply({ error: '规则版本冲突' }, 409) : reply({ ...body, version: 2 })
    if (path.endsWith('/acknowledge')) { acknowledged = true; return reply({ id: 9 }) }
    if (path.endsWith('/retry')) {
      if (retryConflict) return reply({ error: '投递状态或通知目标已变化，请刷新后重新确认' }, 409)
      const row = deliveries.find(item => path.endsWith(`/${item.id}/retry`))
      Object.assign(row, { status: 'pending', manual_retry_count: row.manual_retry_count + 1, cycle_attempt_count: 0, last_error: '', next_attempt_at: new Date().toISOString() })
      return reply(row)
    }
    if (path.endsWith('/log-notification-deliveries')) return reply({ data: deliveries, total: deliveryTotal, page: Number(new URL(req.url()).searchParams.get('page')), page_size: 20 })
    if (path.endsWith('/log-notification-destinations') && req.method() === 'GET') return reply(destinations)
    if (path.endsWith('/log-notification-destinations')) return reply({ ...body, id: 1, version: 1 })
    if (path.endsWith('/credential')) return reply({ id: 1, version: 2, channel: requests.find(r => r.method === 'POST' && r.path.endsWith('/log-notification-destinations'))?.body.channel || 'webhook', enabled: false })
    if (path.endsWith('/log-notification-destinations/1')) return reply({ ...body, id: 1, version: 3 })
    throw new Error(`Unexpected platform request: ${req.method()} ${path}`)
  })
  await page.goto('/modules?tab=log-pipeline')
  await expect(page.getByTestId('pipeline-health')).toContainText('告警')
  return { requests, deployment: state => { deploymentState = state }, fail: () => { failure = true }, conflict: () => { conflict = true } }
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

test('delivery diagnostics identify targets and retry times without exposing unknown error text', async ({ page }) => {
  const nextAttempt = new Date(Date.now() + 60_000).toISOString()
  const deliveredAt = new Date(Date.now() - 60_000).toISOString()
  await fixture(page, {
    destinations: [{ id: 7, name: '平台值班接收端', channel: 'webhook', url: 'https://notify.example.test/hook', enabled: true }],
    deliveries: [
      { ...deliveryView, id: 'delivery-retry', event_id: 'event-opened', destination_id: 7, channel: 'webhook', status: 'pending', attempt_count: 2, last_error: 'notification_send_failed', next_attempt_at: nextAttempt },
      { ...deliveryView, destination_name: '', destination_version: 0, id: 'delivery-dead', event_id: 'event-expired', destination_id: 9, channel: 'webhook', status: 'dead', attempt_count: 8, last_error: 'notification_attempt_limit' },
      { ...deliveryView, id: 'delivery-unknown', event_id: 'event-other', destination_id: 7, channel: 'webhook', status: 'dead', attempt_count: 8, last_error: 'https://receiver.invalid/?secret=must-not-be-shown' },
      { ...deliveryView, id: 'delivery-success', event_id: 'event-resolved', destination_id: 7, channel: 'webhook', status: 'delivered', attempt_count: 1, last_error: '', delivered_at: deliveredAt }
    ]
  })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  const table = page.getByTestId('pipeline-deliveries')
  const rows = table.locator('tbody > tr.el-table__row')
  await expect(rows).toHaveCount(4)
  await expect(rows.nth(0)).toContainText('平台值班接收端')
  await expect(rows.nth(0)).toContainText('发送失败，请检查接收端和渠道配置')
  await expect(rows.nth(1)).toContainText('目标 ID 9（当前不可用）')
  await expect(rows.nth(1)).toContainText('尝试次数已达上限')
  await expect(rows.nth(2)).toContainText('投递失败，原因未识别')
  await expect(table).not.toContainText('must-not-be-shown')
  await rows.nth(0).locator('.el-table__expand-icon').click()
  const diagnostics = table.getByTestId('delivery-diagnostics')
  await expect(diagnostics).toContainText('event-opened')
  await expect(diagnostics).toContainText('下次尝试时间')
  const retryText = await page.evaluate(time => new Date(time).toLocaleString(), nextAttempt)
  await expect(diagnostics).toContainText(retryText)
  await rows.nth(0).locator('.el-table__expand-icon').click()
  await rows.nth(3).locator('.el-table__expand-icon').click()
  await expect(diagnostics).toContainText('event-resolved')
  const deliveredText = await page.evaluate(time => new Date(time).toLocaleString(), deliveredAt)
  await expect(diagnostics).toContainText(deliveredText)
})


test('manual retry confirms historical identity and enqueues only once with current versions', async ({ page }) => {
  const state = await fixture(page, { deliveries: [{ ...deliveryView }], deliveryTotal: 45 })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  const table = page.getByTestId('pipeline-deliveries')
  await expect(table).toContainText('本轮尝试次数')
  await expect(table).toContainText('人工重投次数')
  await page.locator('.el-drawer .el-pagination .btn-next').click()
  await expect.poll(() => state.requests.filter(r => r.path.endsWith('/log-notification-deliveries')).at(-1)?.query.page).toBe('2')
  await table.getByRole('button', { name: '重新投递', exact: true }).click()
  const confirm = page.getByRole('dialog', { name: '重新投递', exact: true })
  await expect(confirm).toContainText('历史事件，告警已恢复')
  await expect(confirm).toContainText('平台值班接收端')
  const eventTime = await page.evaluate(time => new Date(time).toLocaleString(), deliveryView.occurred_at)
  await expect(confirm).toContainText(eventTime)
  await confirm.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('已重新入队，送达结果请查看投递记录', { exact: true })).toBeVisible()
  await expect(table).toContainText('待投递')
  await expect(table.getByRole('button', { name: '重新投递', exact: true })).toHaveCount(0)
  const writes = state.requests.filter(r => r.path.endsWith('/retry'))
  expect(writes).toHaveLength(1)
  expect(writes[0].path).toContain(deliveryView.id)
  expect(writes[0].body).toEqual({ expected_manual_retry_count: 0, destination_version: 3 })
  expect(state.requests.filter(r => r.path.endsWith('/log-notification-deliveries')).at(-1).query.page).toBe('2')
})

test('manual retry conflict preserves failed row and exposes safe actionable feedback', async ({ page }) => {
  const state = await fixture(page, { deliveries: [{ ...deliveryView }], retryConflict: true })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  const table = page.getByTestId('pipeline-deliveries')
  await table.getByRole('button', { name: '重新投递', exact: true }).click()
  await page.getByRole('dialog', { name: '重新投递', exact: true }).getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('投递状态或通知目标已变化，请刷新后重新确认', { exact: true })).toBeVisible()
  await expect(table).toContainText('最终失败')
  await expect(table.getByRole('button', { name: '重新投递', exact: true })).toBeEnabled()
  await expect(page.getByText('已重新入队，送达结果请查看投递记录', { exact: true })).toHaveCount(0)
  expect(state.requests.filter(r => r.path.endsWith('/retry'))).toHaveLength(1)
})

test('read only notification permission hides mutation actions', async ({ page }) => {
  await fixture(page, { deliveries: [{ ...deliveryView }], management: false })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  await expect(page.getByTestId('pipeline-deliveries')).toContainText('最终失败')
  await expect(page.getByRole('button', { name: '重新投递', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '新增通知目标', exact: true })).toHaveCount(0)
})

test('completed historical delivery shows the immutable protocol event type without offering a subscription', async ({ page }) => {
  await fixture(page, { deliveries: [{ ...deliveryView, status: 'delivered', event_type: 'escalated' }] })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  const table = page.getByTestId('pipeline-deliveries')
  await table.locator('.el-table__expand-icon').click()
  await expect(table.getByTestId('delivery-diagnostics')).toContainText('escalated')
  await expect(table).not.toContainText('eventsLabel.')
  await expect(table.getByRole('button', { name: '重新投递', exact: true })).toHaveCount(0)
})

test('missing target and active suppression disable retry; pending delivery has no retry action', async ({ page }) => {
  await fixture(page, { deliveries: [
    { ...deliveryView, destination_name: '', destination_version: 0 },
    { ...deliveryView, id: '172b64ab-6256-4d83-b261-5a70dc84c279', suppressed_until: new Date(Date.now() + 60_000).toISOString(), incident_status: 'suppressed' },
    { ...deliveryView, id: '688834d8-5a62-46d7-9b6e-1669868b9b29', status: 'pending' }
  ] })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  const buttons = page.getByTestId('pipeline-deliveries').getByRole('button', { name: '重新投递', exact: true })
  await expect(buttons).toHaveCount(2)
  await expect(buttons.nth(0)).toBeDisabled()
  await expect(buttons.nth(1)).toBeDisabled()
})


test('WeCom URL is written only as a credential before enabling and stays blank on edit', async ({ page }) => {
  const baseURL = 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send'
  const robotURL = `${baseURL}?key=12345678-1234-1234-1234-123456789abc`
  const state = await fixture(page, { destinations: [{ id: 1, version: 3, name: '企微值班群', channel: 'wecom', url: baseURL, recipients: [], event_types: ['opened', 'resolved'], enabled: true, secret_configured: true }] })
  await page.getByRole('button', { name: '通知管理', exact: true }).click()
  await page.getByRole('button', { name: '新增通知目标', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '通知目标配置', exact: true })
  await expect(dialog.getByRole('checkbox', { name: '打开', exact: true })).toBeChecked()
  await expect(dialog.getByRole('checkbox', { name: '恢复', exact: true })).toBeChecked()
  await expect(dialog.getByRole('checkbox', { name: '升级', exact: true })).toHaveCount(0)
  await dialog.getByRole('textbox').first().fill('企微值班群')
  await dialog.locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: '企业微信', exact: true }).click()
  await expect(dialog.getByText('签名密钥（留空保持当前值）', { exact: true })).toHaveCount(0)
  await expect(dialog.getByText('收件地址（逗号分隔）', { exact: true })).toHaveCount(0)
  await dialog.locator('input[type=password]').fill(robotURL)
  await dialog.locator('.el-switch').click()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).toBeHidden()
  const writes = state.requests.filter(r => r.method !== 'GET')
  expect(writes.map(r => r.method)).toEqual(['POST', 'PUT', 'PUT'])
  expect(writes[0].body).toMatchObject({ channel: 'wecom', url: '', recipients: [], event_types: ['opened', 'resolved'], enabled: false })
  expect(writes[1].body).toEqual({ version: 1, secret: robotURL })
  expect(writes[2].body).toMatchObject({ channel: 'wecom', url: '', event_types: ['opened', 'resolved'], enabled: true, version: 2 })
  for (const request of [writes[0], writes[2]]) expect(JSON.stringify(request.body)).not.toContain('12345678-1234')
  await page.locator('.el-drawer').getByRole('button', { name: '编辑', exact: true }).click()
  await expect(dialog.locator('input[type=password]')).toHaveValue('')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).toBeHidden()
  expect(state.requests.filter(r => r.path.endsWith('/credential'))).toHaveLength(1)
  await expect(page.locator('.el-drawer')).not.toContainText('12345678-1234')
})

for (const [state, label] of [['disabled', '已关闭'], ['unconfigured', '部署配置无效']]) {
  test(`paused logging shows ${state} while preserving incidents`, async ({ page }) => {
    const fixtureState = await fixture(page)
    fixtureState.deployment(state)
    await page.getByTestId('log-pipeline').getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('pipeline-health')).toContainText(label)
    await expect(page.getByTestId('pipeline-paused')).toContainText('链路检测已暂停')
    await expect(page.getByTestId('pipeline-incidents')).toContainText('未处理')
    await expect(page.getByText('探针成功', { exact: true })).toHaveCount(0)
  })
}
