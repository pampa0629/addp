import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: {
      'access-control-allow-origin': browserTestOrigin('system'),
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,PUT,POST,OPTIONS'
    },
    body: JSON.stringify(body)
  })
}

test('System and Gateway stay enabled while a business module can be disabled', async ({ page }) => {
  const startedAt = new Date(Date.now() - 65000).toISOString()
  const leaseExpiresAt = new Date(Date.now() + 60000).toISOString()
  const modules = [
    { id: 1, module_name: 'system', route_prefix: '/system', enabled: true, version: 1, instances: [] },
    { id: 2, module_name: 'manager', route_prefix: '/manager', enabled: true, version: 1, instances: [
      { instance_id: 'manager-backend', role: 'backend', status: 'up', module_url: 'http://manager.local:8081', process_started_at: startedAt, lease_expires_at: leaseExpiresAt },
      { instance_id: 'manager-worker', host_node_name: 'host-a', runtime_hostname: 'container-a', host_node_ips: ['192.0.2.7', '2001:db8::1'], role: 'worker', status: 'down', module_url: '', process_started_at: startedAt,
        last_heartbeat: startedAt, lease_expires_at: startedAt, stopped_at: startedAt, stop_reason: 'lease_expired' }
    ] },
    { id: 3, module_name: 'gateway', route_prefix: '', enabled: true, version: 1, instances: [] }
  ]
  const writes = []
  const instanceQueries = []
  await page.route('**/api/v1/system/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (method === 'OPTIONS') return fulfillJSON(route, 204, {})
    if (path.endsWith('/refresh')) return fulfillJSON(route, 401, { error: 'authentication_required' })
    if (path.endsWith('/login') && method === 'POST') {
      return fulfillJSON(route, 200, {
        next_action: 'session_issued',
        session: { access_token: 'e2e-module-token', expires_in: 300 }
      })
    }
    if (path.endsWith('/users/me')) {
      return fulfillJSON(route, 200, { id: '1', display_name: 'E2E Administrator' })
    }
    if (path.endsWith('/auth/context')) {
      return fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'platform' },
        authentication: { assurance_level: 'aal2' },
        authorization: {
          role_assignments: [{
            role_key: 'platform.system_administrator', scope: { type: 'platform' },
            permissions: ['platform.module.read', 'platform.module.update']
          }]
        }
      })
    }
    if (path.endsWith('/platform/modules') && method === 'GET') {
      return fulfillJSON(route, 200, { modules })
    }
    if (path.endsWith('/platform/module-instances') && method === 'GET') {
      const params = Object.fromEntries(new URL(request.url()).searchParams)
      instanceQueries.push(params)
      const all = modules[1].instances.map((instance, index) => ({
        ...instance, id: index + 1, module_name: 'manager', registered_host: index === 0 ? 'manager.local' : '',
        registered_at: startedAt
      }))
      const data = all.filter(instance => (!params.module_name || instance.module_name === params.module_name) &&
        (!params.registered_host || instance.registered_host === params.registered_host) &&
        (!params.node_name || [instance.host_node_name, instance.runtime_hostname].some(name => name?.toLowerCase() === params.node_name.toLowerCase())) &&
        (!params.node_ip || instance.host_node_ips?.includes(params.node_ip)) &&
        (!params.role || instance.role === params.role) && (!params.status || instance.status === params.status))
      return fulfillJSON(route, 200, { data, total: data.length, page: Number(params.page), page_size: Number(params.page_size), total_pages: 1 })
    }
    if (path.endsWith('/platform/modules/manager') && method === 'PUT') {
      writes.push(request.postDataJSON())
      modules[1] = { ...modules[1], enabled: false, version: 2 }
      return fulfillJSON(route, 200, modules[1])
    }
    throw new Error(`unexpected module management E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=%2Fmodules')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('e2e-password')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/modules$/)
  const moduleRow = name => page.getByRole('row').filter({
    has: page.locator('.module-technical-name').filter({ hasText: new RegExp(`^${name}$`) })
  })
  const systemRow = moduleRow('system')
  const gatewayRow = moduleRow('gateway')
  const managerRow = moduleRow('manager')
  await expect(systemRow.getByText('始终启用')).toBeVisible()
  await expect(systemRow.getByRole('switch')).toHaveCount(0)
  await expect(gatewayRow.getByText('始终启用')).toBeVisible()
  await expect(gatewayRow.getByRole('switch')).toHaveCount(0)
  await expect(managerRow.getByText('UP · 在线')).toBeVisible()
  await expect(managerRow.getByText('DOWN · 离线')).toBeVisible()
  await expect(managerRow.getByText('Backend · manager.local:8081')).toBeVisible()
  await expect(managerRow.getByText('Worker · 无监听端点')).toBeVisible()
  await expect(managerRow.locator('.instance-observation').filter({ hasText: 'UP · 在线' }).locator('.instance-observation-uptime')).toContainText('1 分钟')
  await managerRow.locator('.el-table__expand-icon').click()
  await expect(page.getByRole('columnheader', { name: '实例主机/IP:端口' })).toBeVisible()
  await managerRow.locator('.el-switch').click()
  await expect.poll(() => writes).toEqual([{ enabled: false, version: 1 }])
  await page.getByRole('button', { name: '查询该模块实例' }).click()
  await expect(page.getByRole('tab', { name: '服务实例' })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(() => instanceQueries.at(-1)?.module_name).toBe('manager')
  await expect.poll(() => instanceQueries.at(-1)?.status).toBe('up')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await expect(page.locator('.module-instances').getByText('manager-worker', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查询', exact: true })).toHaveCount(0)
  await page.getByPlaceholder('登记主机名或 IP').fill('manager')
  await page.getByPlaceholder('登记主机名或 IP').fill('manager.local')
  await expect.poll(() => instanceQueries.at(-1)?.registered_host).toBe('manager.local')
  expect(instanceQueries.some(query => query.registered_host === 'manager')).toBe(false)
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await page.locator('.module-instances .el-select').filter({ has: page.getByRole('combobox', { name: '时间范围', exact: true }) }).click()
  await page.getByRole('option', { name: '近 15 分钟' }).click()
  await expect.poll(() => instanceQueries.at(-1)?.time_from).toMatch(/^\d{4}-\d{2}-\d{2}T/)
  await expect.poll(() => instanceQueries.at(-1)?.time_to).toMatch(/^\d{4}-\d{2}-\d{2}T/)
  await page.locator('.module-instances .el-select').nth(1).click()
  await page.getByRole('option', { name: 'Backend', exact: true }).click()
  await expect.poll(() => instanceQueries.at(-1)?.role).toBe('backend')
  await page.locator('.module-instances .el-select').nth(2).click()
  await page.getByRole('option', { name: 'UP · 在线' }).click()
  await expect.poll(() => instanceQueries.at(-1)?.status).toBe('up')
  await page.locator('.module-instances .el-select').first().click()
  await page.getByRole('option', { name: '系统管理', exact: true }).click()
  await expect.poll(() => instanceQueries.at(-1)?.module_name).toBe('system')
  await expect(page.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  const queriesBeforeCustomRange = instanceQueries.length
  await page.locator('.module-instances .el-select').filter({ has: page.getByRole('combobox', { name: '时间范围', exact: true }) }).click()
  await page.getByRole('option', { name: '自定义时间' }).click()
  await expect(page.getByText('请选择完整的起止时间', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeDisabled()
  expect(instanceQueries).toHaveLength(queriesBeforeCustomRange)
  const customRange = page.locator('.module-instances .query-range')
  await customRange.getByPlaceholder('起始时间').fill('2020-01-01 00:00:00')
  await customRange.getByPlaceholder('结束时间').fill('2030-01-01 00:00:00')
  await customRange.getByPlaceholder('结束时间').press('Enter')
  await expect.poll(() => instanceQueries.at(-1)?.time_from).toMatch(/^2019-12-31T|^2020-01-01T/)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '重置' }).click()
  await expect.poll(() => instanceQueries.at(-1)).toEqual({ page: '1', page_size: '10', status: 'up' })
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await page.locator('.module-instances .el-select').nth(2).click()
  await page.getByRole('option', { name: '全部状态', exact: true }).click()
  await expect.poll(() => instanceQueries.at(-1)?.status).toBeUndefined()
  await expect(page.locator('.module-instances .el-select').nth(2)).toContainText('全部状态')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(2)
  await page.locator('.module-instances .el-select').nth(2).click()
  await page.getByRole('option', { name: 'DOWN · 离线', exact: true }).click()
  await expect.poll(() => instanceQueries.at(-1)?.status).toBe('down')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await page.getByPlaceholder('宿主节点或运行环境主机名').fill('HOST-A')
  await expect.poll(() => instanceQueries.at(-1)?.node_name).toBe('HOST-A')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await expect(page.locator('.module-instances').getByText('container-a', { exact: false })).toBeVisible()
  await page.getByPlaceholder('宿主节点或运行环境主机名').fill('container-a')
  await expect.poll(() => instanceQueries.at(-1)?.node_name).toBe('container-a')
  await page.getByPlaceholder('宿主节点 IP（精确匹配）').fill('2001:db8::1')
  await expect.poll(() => instanceQueries.at(-1)?.node_ip).toBe('2001:db8::1')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await expect(page).toHaveURL(/node_ip=2001(?::|%3A)db8/)
  await expect(page.locator('.module-instances .instance-node')).toContainText('192.0.2.7, 2001:db8::1')
  const workerRow = page.locator('.module-instances .el-table__body tr').filter({ hasText: 'manager-worker' })
  await workerRow.locator('.el-table__expand-icon').click()
  const diagnostics = page.locator('.module-instances .el-table__expanded-cell')
  await expect(diagnostics).toContainText('宿主节点 IP')
  await expect(diagnostics).toContainText('192.0.2.7, 2001:db8::1')
  await expect(diagnostics).toContainText('最近心跳')
  await expect(diagnostics).toContainText('租约到期')
  await expect(diagnostics).toContainText('离线判定时间')
  await expect(diagnostics).toContainText('租约超时，疑似异常退出')
  await expect(diagnostics).toContainText('租约超时仅表示实例失联，不能据此断定进程已经退出。')
  await expect(diagnostics).toContainText(/\d{4}\/\d{1,2}\/\d{1,2} \d{1,2}:\d{2}:\d{2}/)
  await page.getByRole('button', { name: '重置' }).click()
  await expect.poll(() => instanceQueries.at(-1)).toEqual({ page: '1', page_size: '10', status: 'up' })
  await expect(page.getByPlaceholder('宿主节点或运行环境主机名')).toHaveValue('')
  await expect(page.getByPlaceholder('宿主节点 IP（精确匹配）')).toHaveValue('')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
})

test('instance queries renew leases without losing filters or accepting an older refresh', async ({ page }) => {
  const started = Date.now()
  await page.clock.install({ time: new Date(started) })
  let leaseExpiresAt = new Date(started + 20_000).toISOString()
  const instanceQueries = []
  let holdNextQuery = false
  let releaseRefresh
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() === 'OPTIONS') return fulfillJSON(route, 204, {})
    if (url.pathname.endsWith('/refresh')) return fulfillJSON(route, 401, {})
    if (url.pathname.endsWith('/login')) return fulfillJSON(route, 200, {
      next_action: 'session_issued', session: { access_token: 'instance-refresh-test', expires_in: 3600 }
    })
    if (url.pathname.endsWith('/users/me')) return fulfillJSON(route, 200, { id: '1', display_name: 'Test reader' })
    if (url.pathname.endsWith('/auth/context')) return fulfillJSON(route, 200, {
      principal: { id: '1', principal_type: 'user' }, context: { type: 'platform' },
      authorization: { role_assignments: [{ scope: { type: 'platform' }, permissions: ['platform.module.read'] }] }
    })
    if (url.pathname.endsWith('/platform/modules')) return fulfillJSON(route, 200, { modules: [
      { module_name: 'manager', enabled: true, instances: [] }
    ] })
    if (url.pathname.endsWith('/platform/module-instances')) {
      const params = Object.fromEntries(url.searchParams)
      instanceQueries.push(params)
      const data = Array.from({ length: 10 }, (_, index) => ({
        id: index + 1, instance_id: `${params.status}-${params.page}-${index}`, module_name: 'manager',
        role: 'backend', module_url: 'http://manager.local:8081',
        process_started_at: new Date(started - 65_000).toISOString(),
        status: params.status,
        lease_expires_at: params.status === 'down' ? new Date(started).toISOString() : leaseExpiresAt,
        ...(params.status === 'down' ? { stop_reason: 'lease_expired' } : {})
      }))
      if (holdNextQuery) {
        holdNextQuery = false
        await new Promise(resolve => { releaseRefresh = resolve })
      }
      return fulfillJSON(route, 200, { data, total: 25, page: Number(params.page), page_size: 10 })
    }
    throw new Error(`Unexpected request: ${url.pathname}`)
  })
  await page.goto('/login?redirect=%2Fmodules')
  await page.locator('input[autocomplete="username"]').fill('test-reader')
  await page.locator('input[autocomplete="current-password"]').fill('fixture-only')
  await page.locator('button.auth-login-primary').click()
  await page.getByRole('tab', { name: '服务实例' }).click()
  const query = page.locator('.module-instances')
  await query.getByPlaceholder('登记主机名或 IP').fill('manager.local')
  await query.getByPlaceholder('登记主机名或 IP').press('Enter')
  await expect.poll(() => instanceQueries.at(-1)?.registered_host).toBe('manager.local')
  await query.locator('.el-pagination .btn-next').click()
  await expect.poll(() => instanceQueries.at(-1)?.page).toBe('2')
  await expect(query.getByText('up-2-0', { exact: true })).toBeVisible()
  const applied = { ...instanceQueries.at(-1) }
  const before = instanceQueries.length
  leaseExpiresAt = new Date(started + 300_000).toISOString()
  await page.clock.fastForward(30_000)
  await expect.poll(() => instanceQueries.length).toBeGreaterThan(before)
  await expect(query.locator('.el-tag').filter({ hasText: 'UP · 在线' })).toHaveCount(10)
  await expect(query.getByText('up-2-0', { exact: true })).toBeVisible()
  expect(instanceQueries.at(-1)).toEqual(applied)
  await expect(query.locator('.el-table__body tr').first()).toContainText('1 分钟')

  holdNextQuery = true
  const pendingStart = instanceQueries.length
  await page.clock.fastForward(10_000)
  await expect.poll(() => instanceQueries.length).toBe(pendingStart + 1)
  await expect(query.locator('.el-loading-mask')).toHaveCount(0)
  await page.clock.fastForward(20_000)
  expect(instanceQueries.length).toBe(pendingStart + 1)
  await query.locator('.el-select').filter({
    has: page.getByRole('combobox', { name: '运行状态', exact: true })
  }).locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: 'DOWN · 离线', exact: true }).click()
  await expect(query.getByText('down-1-0', { exact: true })).toBeVisible()
  const oldResponse = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname.endsWith('/platform/module-instances') && url.searchParams.get('status') === 'up'
  })
  const latestFreshness = await query.getByTestId('instance-query-freshness').innerText()
  await page.clock.fastForward(1000)
  releaseRefresh()
  await (await oldResponse).finished()
  await expect(query.getByText('down-1-0', { exact: true })).toBeVisible()
  await expect(query.getByText('up-2-0', { exact: true })).toHaveCount(0)
  await expect(query.getByTestId('instance-query-freshness')).toHaveText(latestFreshness)

  await page.getByRole('tab', { name: '模块概览' }).click()
  const afterLeave = instanceQueries.length
  await page.clock.fastForward(20_000)
  expect(instanceQueries.length).toBe(afterLeave)
})

test('one instance expires and recovers while automatic refresh preserves filters and current status membership', async ({ page }) => {
  const started = Date.now()
  await page.clock.install({ time: new Date(started) })
  await mockModuleQueryAPI(page)
  const queries = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  let instance = {
    id: 1, instance_id: 'manager-lease-probe', module_name: 'manager', role: 'backend',
    module_url: 'http://manager.local:8081', health_check_url: 'http://manager.local:8081/health/ready',
    host_node_name: 'host-a', runtime_hostname: 'container-a', status: 'up',
    process_started_at: new Date(started - 65_000).toISOString(),
    registered_at: new Date(started - 65_000).toISOString(),
    last_heartbeat: new Date(started).toISOString(),
    lease_expires_at: new Date(started + 15_000).toISOString()
  }
  await page.route('**/api/v1/system/platform/module-instances?*', route => {
    if (route.request().method() === 'OPTIONS') return fulfillJSON(route, 204, {})
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(params)
    const data = !params.status || params.status === instance.status ? [instance] : []
    return fulfillJSON(route, 200, { data, total: data.length, page: 1, page_size: 10 })
  })
  await page.goto('/modules?tab=instances&module_name=manager&registered_host=manager.local&node_name=host-a&role=backend&status=all')
  const list = page.locator('.module-instances')
  await expect(list.locator('.el-select').filter({
    has: page.getByRole('combobox', { name: '运行状态', exact: true })
  })).toContainText('全部状态')
  const row = () => list.locator('.el-table__row').filter({ hasText: instance.instance_id })
  const selectStatus = async label => {
    await list.locator('.el-select').filter({
      has: page.getByRole('combobox', { name: '运行状态', exact: true })
    }).locator('.el-select__wrapper').click()
    await page.getByRole('option', { name: label, exact: true }).click()
  }
  await expect(row().locator('.el-tag').filter({ hasText: 'UP · 在线' })).toHaveClass(/el-tag--success/)
  await expect(row()).toContainText('manager.local:8081')
  await expect(row()).toContainText('1 分钟')
  const allQuery = { ...queries.at(-1) }
  const allURL = page.url()

  instance = {
    ...instance, status: 'down', stop_reason: 'lease_expired', stopped_at: instance.lease_expires_at
  }
  const beforeExpiry = queries.length
  await page.clock.fastForward(20_000)
  await expect.poll(() => queries.length).toBeGreaterThan(beforeExpiry)
  await expect(row().locator('.el-tag').filter({ hasText: 'DOWN · 离线' })).toHaveClass(/el-tag--danger/)
  expect(queries.at(-1)).toEqual(allQuery)
  await expect(page).toHaveURL(allURL)
  await expect(list.locator('.el-loading-mask')).toHaveCount(0)
  await expect(row()).toContainText('租约超时，疑似异常退出')
  const uptimeColumn = await list.getByRole('columnheader', { name: '持续运行时长', exact: true }).evaluate(el => el.cellIndex)
  await expect(row().locator('td').nth(uptimeColumn)).toHaveText('—')
  await row().locator('.el-table__expand-icon').click()
  await expect(list.getByText('租约超时仅表示实例失联，不能据此断定进程已经退出。', { exact: true })).toBeVisible()

  await selectStatus('UP · 在线')
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  await expect(row()).toHaveCount(0)
  await selectStatus('DOWN · 离线')
  await expect(row().getByText('DOWN · 离线', { exact: true })).toBeVisible()
  const downQuery = { ...queries.at(-1) }
  const downURL = page.url()

  instance = {
    ...instance, status: 'up', stop_reason: '', stopped_at: null,
    last_heartbeat: new Date(started + 20_000).toISOString(),
    lease_expires_at: new Date(started + 120_000).toISOString()
  }
  const beforeRecovery = queries.length
  await page.clock.fastForward(10_000)
  await expect.poll(() => queries.length).toBeGreaterThan(beforeRecovery)
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  await expect(row()).toHaveCount(0)
  expect(queries.at(-1)).toEqual(downQuery)
  await expect(page).toHaveURL(downURL)

  await selectStatus('UP · 在线')
  await expect(row().locator('.el-tag').filter({ hasText: 'UP · 在线' })).toHaveClass(/el-tag--success/)
  await expect(row().locator('td').nth(uptimeColumn)).toContainText('1 分钟')
  await expect(row()).not.toContainText('租约超时，疑似异常退出')
  await row().locator('.el-table__expand-icon').click()
  await expect(list.getByText('租约超时仅表示实例失联，不能据此断定进程已经退出。', { exact: true })).toHaveCount(0)
  await expect(list.getByPlaceholder('登记主机名或 IP')).toHaveValue('manager.local')
  await expect(list.getByPlaceholder('宿主节点或运行环境主机名')).toHaveValue('host-a')
  expect(queries.at(-1)).toEqual({ ...allQuery, status: 'up' })
  expect(errors).toEqual([])
})
