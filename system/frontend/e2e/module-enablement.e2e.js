import { expect, test } from '@playwright/test'

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: {
      'access-control-allow-origin': 'http://127.0.0.1:4173',
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
      { instance_id: 'manager-worker', role: 'worker', status: 'down', module_url: '', process_started_at: startedAt,
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
        (!params.registered_host || instance.registered_host === params.registered_host))
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
  await page.getByPlaceholder('登记主机名或 IP').fill('manager.local')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect.poll(() => instanceQueries.at(-1)?.registered_host).toBe('manager.local')
  await expect(page.locator('.module-instances .el-table__body tr')).toHaveCount(1)
  await page.locator('.module-instances .el-select').nth(3).click()
  await page.getByRole('option', { name: '近 15 分钟' }).click()
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect.poll(() => instanceQueries.at(-1)?.registered_from).toMatch(/^\d{4}-\d{2}-\d{2}T/)
  await expect.poll(() => instanceQueries.at(-1)?.registered_to).toMatch(/^\d{4}-\d{2}-\d{2}T/)
  await page.getByRole('button', { name: '重置' }).click()
  await expect.poll(() => instanceQueries.at(-1)?.module_name).toBeUndefined()
  const workerRow = page.locator('.module-instances .el-table__body tr').filter({ hasText: 'manager-worker' })
  await workerRow.locator('.el-table__expand-icon').click()
  const diagnostics = page.locator('.module-instances .el-table__expanded-cell')
  await expect(diagnostics).toContainText('最近心跳')
  await expect(diagnostics).toContainText('租约到期')
  await expect(diagnostics).toContainText('下线判定时间')
  await expect(diagnostics).toContainText('租约超时，疑似异常退出')
  await expect(diagnostics).toContainText('租约超时仅表示实例失联，不能据此断定进程已经退出。')
  await expect(diagnostics).toContainText(/\d{4}\/\d{1,2}\/\d{1,2} \d{1,2}:\d{2}:\d{2}/)
})
