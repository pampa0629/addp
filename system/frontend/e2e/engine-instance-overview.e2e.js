import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const metric = (key, scope, value, unit = 'count', semantics = 'gauge') => ({ key, scope, value, unit, semantics, status: 'available', ...(scope === 'database' ? { scope_name: 'example' } : {}) })
const profiles = {
  pg: [metric('database_size_bytes', 'database', '9007199254740993', 'bytes'), metric('transactions_committed', 'database', '12', 'count', 'counter')],
  redis: [metric('database_keys', 'database', '0'), { key: 'server_memory_bytes', scope: 'server', unit: 'bytes', semantics: 'gauge', status: 'permission_denied' }],
  minio: [metric('account_buckets', 'account', '2')]
}

async function fixture(page, { language = 'zh-cn', metrics = profiles.pg, supported = true } = {}) {
  const state = { requests: [], reads: 0, fail: false, version: 1, pending: null, delay: false }
  const engine = { id: '71', name: 'Native overview fixture', version: 1, engine_type: 'future_native', engine_origin: 'general',
    lifecycle_state: 'active', connection_status: 'online', connection_info: {},
    capabilities: { schema_version: 'engine.capabilities/v1', engine_family: 'tabular', storage: {}, ...(supported ? { instance_overview: true } : {}) } }
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    state.requests.push(`${request.method()} ${path}`)
    if (path.endsWith('/refresh')) return reply({ access_token: 'overview-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Overview reader' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' },
      context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: ['system.engine.read'] }] } })
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines')) return reply([engine])
    if (path.endsWith('/engines/71')) return reply(engine)
    if (path.endsWith('/engines/71/overview')) {
      state.reads++
      const response = { engine_id: '71', version: state.version, queried_at: '2026-11-08T12:00:00Z', metrics }
      if (state.delay) { state.pending = { route, headers, response }; state.delay = false; return }
      if (state.fail) return reply({ error: 'private-source-error' }, 503)
      return reply(response)
    }
    throw new Error(`Unexpected overview fixture operation: ${request.method()} ${path}`)
  })
  await page.goto('/engines')
  await page.locator('.el-table__row').filter({ hasText: engine.name }).getByRole('button', { name: language === 'en' ? 'Detail' : '详情', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  return state
}
const openOverview = page => page.getByRole('tab', { name: '实例概况', exact: true }).click()

for (const profile of Object.keys(profiles)) {
  test(`${profile} overview renders exact values, scope and independent permission denial without native side effects`, async ({ page }) => {
    const state = await fixture(page, { metrics: profiles[profile] })
    expect(state.reads).toBe(0)
    await openOverview(page)
    const panel = page.getByTestId('engine-instance-overview')
    await expect(panel.getByTestId('overview-metrics')).toBeVisible()
    if (profile === 'pg') { await expect(panel).toContainText('9,007,199,254,740,993'); await expect(panel).toContainText('整个数据库: example'); await expect(panel).toContainText('累计值') }
    if (profile === 'redis') { await expect(panel).toContainText('0 个'); await expect(panel).toContainText('源账号权限不足'); await expect(panel).toContainText('整个服务') }
    if (profile === 'minio') { await expect(panel).toContainText('2 个'); await expect(panel).toContainText('当前连接账号的桶列表'); await expect(panel).not.toContainText('数据库占用空间') }
    expect(state.reads).toBe(1)
    expect(state.requests.some(request => /\/catalog|\/test|\/access_grants|\/monitor/.test(request))).toBe(false)
    expect(state.requests.filter(request => !request.startsWith('GET ') && !request.endsWith('/refresh'))).toEqual([])
    await page.setViewportSize({ width: 1280, height: 800 })
    const dialog = page.getByRole('dialog')
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  })
}

test('manual refresh clears old values on failure and never reveals the source error', async ({ page }) => {
  const state = await fixture(page)
  await openOverview(page)
  const panel = page.getByTestId('engine-instance-overview')
  await expect(panel.getByTestId('overview-metrics')).toBeVisible()
  state.fail = true
  await panel.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(panel.getByTestId('overview-error')).toBeVisible()
  await expect(panel.getByTestId('overview-metrics')).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('private-source-error')
  expect(state.reads).toBe(2)
})

test('closing the overview discards a late response and a changed configuration cannot be relabelled', async ({ page }) => {
  const state = await fixture(page)
  state.delay = true
  await openOverview(page)
  await expect.poll(() => state.pending !== null).toBe(true)
  await page.getByRole('tab', { name: '基本信息', exact: true }).click()
  const pending = state.pending
  await pending.route.fulfill({ json: pending.response, headers: pending.headers })
  await expect(page.getByTestId('engine-instance-overview')).toHaveCount(0)
  state.version = 2
  await openOverview(page)
  await expect(page.getByTestId('overview-error')).toContainText('引擎配置已变化')
  await expect(page.getByTestId('overview-metrics')).toHaveCount(0)
})

test('uses English labels and omits the tab when the capability is absent', async ({ page }) => {
  await fixture(page, { language: 'en', metrics: profiles.minio })
  await page.getByRole('tab', { name: 'Instance overview', exact: true }).click()
  await expect(page.getByTestId('overview-metrics')).toContainText('Buckets listed for the account')
  await expect(page.getByTestId('overview-metrics')).toContainText('Registered account bucket list')
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
  await page.unrouteAll({ behavior: 'wait' })
  await fixture(page, { supported: false })
  await expect(page.getByRole('tab', { name: '实例概况', exact: true })).toHaveCount(0)
})

test('restores the overview from the existing engine detail URL', async ({ page }) => {
  const state = await fixture(page, { metrics: profiles.minio })
  await page.goto('/engines/71?tab=overview')
  await expect(page.getByTestId('overview-metrics')).toContainText('账号可列出的桶数')
  await expect(page.getByRole('tab', { name: '实例概况', exact: true })).toHaveAttribute('aria-selected', 'true')
  expect(state.reads).toBe(1)
})
