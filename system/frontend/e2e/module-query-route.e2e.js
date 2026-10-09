import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI, moduleQueryLink } from './module-query.fixture'

test('instance freshness tracks successful results, preserves stale data during retries and recovers', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-10-01T12:00:00Z') })
  await mockModuleQueryAPI(page)
  let mode = 'failure'
  let release
  const queries = []
  await page.route('**/api/v1/system/platform/module-instances?*', async route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(params)
    const requestedMode = mode
    if (requestedMode === 'pending') await new Promise(resolve => { release = resolve })
    const headers = {
      'access-control-allow-origin': route.request().headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true'
    }
    if (requestedMode === 'failure') return route.fulfill({ status: 503, headers, json: { error: '查询暂时不可用' } })
    const data = params.status === 'down' ? [] : [{
      instance_id: 'freshness-probe', module_name: 'manager', role: 'backend', status: 'up',
      module_url: 'http://manager.local:8081', lease_expires_at: '2026-10-01T13:00:00Z'
    }]
    return route.fulfill({ headers, json: { data, total: data.length, page: 1, page_size: 10 } })
  })
  await page.goto('/modules?tab=instances')
  const list = page.locator('.module-instances')
  const freshness = list.getByTestId('instance-query-freshness')
  const stale = list.getByText('数据已过期', { exact: true })
  const failure = list.getByText('查询暂时不可用', { exact: true })
  await expect(failure).toBeVisible()
  await expect(freshness).toHaveText('当前查询尚未成功获取数据')
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toHaveCount(0)
  await expect(stale).toHaveCount(0)

  mode = 'success'
  await list.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(list.getByText('freshness-probe', { exact: true })).toBeVisible()
  await expect(freshness).toContainText('最后成功刷新：')
  const firstSuccess = await freshness.innerText()
  const queryURL = page.url()
  mode = 'failure'
  await page.clock.fastForward(10000)
  await expect(stale).toBeVisible()
  await expect(failure).toBeVisible()
  await expect(freshness).toHaveText(firstSuccess)
  await expect(list.getByText('freshness-probe', { exact: true })).toBeVisible()
  await expect(page).toHaveURL(queryURL)

  mode = 'pending'
  await list.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => typeof release).toBe('function')
  await expect(stale).toBeVisible()
  await expect(failure).toBeVisible()
  await expect(freshness).toHaveText(firstSuccess)
  release()
  await expect(stale).toHaveCount(0)
  await expect(failure).toHaveCount(0)
  await expect(freshness).not.toHaveText(firstSuccess)

  const recoveredAt = await freshness.innerText()
  const queryCount = queries.length
  release = undefined
  await page.clock.fastForward(10000)
  await expect.poll(() => typeof release).toBe('function')
  await expect(stale).toHaveCount(0)
  await page.clock.fastForward(10000)
  await expect(stale).toBeVisible()
  await expect(failure).toHaveCount(0)
  await expect(freshness).toHaveText(recoveredAt)
  expect(queries).toHaveLength(queryCount + 1)
  await page.screenshot({ path: '/tmp/addp-instance-freshness-stale.png', fullPage: true })
  mode = 'success'
  release()
  await expect(stale).toHaveCount(0)
  await expect(freshness).not.toHaveText(recoveredAt)

  mode = 'failure'
  await list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '运行状态', exact: true }) }).click()
  await page.getByRole('option', { name: 'DOWN · 离线', exact: true }).click()
  await expect(failure).toBeVisible()
  await expect(freshness).toHaveText('当前查询尚未成功获取数据')
  await expect(list.getByText('freshness-probe', { exact: true })).toHaveCount(0)
  await expect(list.getByRole('button', { name: '下一页', exact: true })).toHaveCount(0)
  await expect(stale).toHaveCount(0)
  mode = 'success'
  await list.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(freshness).toContainText('最后成功刷新：')
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  await expect(failure).toHaveCount(0)
})

test('returning to a visible instance page checks stale data and refreshes without changing filters', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-10-01T12:00:00Z') })
  const queries = await mockModuleQueryAPI(page)
  await page.goto('/modules?tab=instances&module_name=manager&role=backend')
  const list = page.locator('.module-instances')
  const freshness = list.getByTestId('instance-query-freshness')
  await expect(freshness).toContainText('最后成功刷新：')
  const firstSuccess = await freshness.innerText()
  const url = page.url()
  const queryCount = queries.length
  await page.evaluate(() => {
    window.fixturePageHidden = true
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => window.fixturePageHidden })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await page.clock.fastForward(30000)
  expect(queries).toHaveLength(queryCount)
  let resume
  await page.route('**/api/v1/system/platform/module-instances?*', async route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    await new Promise(resolve => { resume = resolve })
    return route.fallback()
  })
  await page.evaluate(() => {
    window.fixturePageHidden = false
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => typeof resume).toBe('function')
  await expect(list.getByText('数据已过期', { exact: true })).toBeVisible()
  await expect(freshness).toHaveText(firstSuccess)
  resume()
  await expect.poll(() => queries.length).toBe(queryCount + 1)
  await expect(list.getByText('数据已过期', { exact: true })).toHaveCount(0)
  await expect(freshness).not.toHaveText(firstSuccess)
  await expect(page).toHaveURL(url)
  expect(queries.at(-1)).toEqual(queries[0])
})

test('standalone instance links restore filters, ranges and pagination after reload and history navigation', async ({ page }) => {
  const queries = await mockModuleQueryAPI(page)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto(`/modules${moduleQueryLink}`)
  const list = page.locator('.module-instances')
  await expect(page.getByRole('tab', { name: '服务实例' })).toHaveAttribute('aria-selected', 'true')
  await expect(list.getByText('page-2-0', { exact: true })).toBeVisible()
  await expect(list.getByPlaceholder('登记主机名或 IP')).toHaveValue('manager.local')
  await expect(list.getByPlaceholder('宿主节点或运行环境主机名')).toHaveValue('host-a')
  await expect.poll(() => queries.at(-1)).toEqual({
    module_name: 'manager', registered_host: 'manager.local', node_name: 'host-a', role: 'backend', time_basis: 'offline',
    time_from: '2026-10-01T00:00:00.000Z', time_to: '2026-10-02T00:00:00.000Z', page: '2', page_size: '20'
  })
  await page.reload()
  await expect(list.getByText('page-2-0', { exact: true })).toBeVisible()
  await expect(list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '时间依据', exact: true }) })).toContainText('离线判定时间')
  const historyLength = await page.evaluate(() => history.length)
  await list.getByPlaceholder('登记主机名或 IP').fill('host-b')
  await list.getByPlaceholder('登记主机名或 IP').press('Enter')
  await expect.poll(() => new URL(page.url()).searchParams.get('registered_host')).toBe('host-b')
  await expect(list.getByText('page-1-0', { exact: true })).toBeVisible()
  expect(new URL(page.url()).searchParams.has('page')).toBe(false)
  expect(await page.evaluate(() => history.length)).toBe(historyLength)
  const restoredURL = page.url()
  await page.goto('/modules')
  await expect(page.getByRole('tab', { name: '模块概览' })).toHaveAttribute('aria-selected', 'true')
  await page.goBack()
  await expect(page).toHaveURL(restoredURL)
  await expect(list.getByPlaceholder('登记主机名或 IP')).toHaveValue('host-b')
  await page.goForward()
  await expect(page.getByRole('tab', { name: '模块概览' })).toHaveAttribute('aria-selected', 'true')
  await page.goto('/modules?tab=instances&status=invalid&page=-1&time_period=custom&time_from=invalid&token=secret')
  await expect(page).toHaveURL(/\/modules\?tab=instances$/)
  await expect.poll(() => queries.at(-1)).toEqual({ page: '1', page_size: '10', status: 'up' })
  expect(errors).toEqual([])
})

test('offline time finds an old registration, queries immediately and removes it after recovery', async ({ page }) => {
  const now = new Date('2026-10-01T12:00:00Z')
  await page.clock.install({ time: now })
  await mockModuleQueryAPI(page)
  const queries = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  const rows = [
    { instance_id: 'old-registration-recent-offline', stopped_at: new Date(now.getTime() - 300000).toISOString() },
    { instance_id: 'old-offline', stopped_at: new Date(now.getTime() - 7200000).toISOString() }
  ].map(row => ({
    ...row, module_name: 'manager', role: 'backend', status: 'down',
    module_url: 'http://manager.local:8081', registered_host: 'manager.local', host_node_name: 'host-a',
    registered_at: new Date(now.getTime() - 7 * 86400000).toISOString(),
    lease_expires_at: row.stopped_at, stop_reason: 'lease_expired'
  }))
  await page.route('**/api/v1/system/platform/module-instances?*', async route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(params)
    const data = rows.filter(row => {
      if (params.status && row.status !== params.status) return false
      if (params.stop_reason && row.stop_reason !== params.stop_reason) return false
      const date = params.time_basis === 'offline' ? row.stopped_at : row.registered_at
      if (params.time_basis === 'offline' && !date) return false
      return (!params.time_from || date >= params.time_from) && (!params.time_to || date < params.time_to)
    })
    return route.fulfill({
      headers: {
        'access-control-allow-origin': route.request().headers().origin || browserTestOrigin('system'),
        'access-control-allow-credentials': 'true'
      },
      json: { data, total: data.length, page: Number(params.page), page_size: Number(params.page_size) }
    })
  })
  await page.goto('/modules?tab=instances&module_name=manager&registered_host=manager.local&node_name=host-a&role=backend&status=down&time_period=1h')
  const list = page.locator('.module-instances')
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  await list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '时间依据', exact: true }) }).click()
  await page.getByRole('option', { name: '离线判定时间', exact: true }).click()
  await expect.poll(() => queries.at(-1)?.time_basis).toBe('offline')
  await expect(list.getByText('old-registration-recent-offline', { exact: true })).toBeVisible()
  await expect(list.getByText('old-offline', { exact: true })).toHaveCount(0)
  await expect(list.getByRole('columnheader', { name: '离线判定时间', exact: true })).toBeVisible()
  const headers = await list.getByRole('columnheader').allTextContents()
  const statusIndex = headers.findIndex(header => header.trim() === '状态')
  expect(statusIndex).toBeGreaterThanOrEqual(0)
  expect(headers[statusIndex + 1].trim()).toBe('离线判定时间')
  await expect(list.getByText('租约超时，疑似异常退出', { exact: true })).toBeVisible()
  await list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '下线原因', exact: true }) }).click()
  await page.getByRole('option', { name: '正常退出', exact: true }).click()
  await expect.poll(() => queries.at(-1)?.stop_reason).toBe('graceful')
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  await list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '下线原因', exact: true }) }).click()
  await page.getByRole('option', { name: '租约超时，疑似异常退出', exact: true }).click()
  await expect.poll(() => queries.at(-1)?.stop_reason).toBe('lease_expired')
  await expect(list.getByText('old-registration-recent-offline', { exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/addp-offline-time-query-verified.png', fullPage: true })
  const url = page.url()
  expect(new URL(url).searchParams.get('time_period')).toBe('1h')
  expect(queries.at(-1)).toMatchObject({
    time_basis: 'offline', status: 'down', stop_reason: 'lease_expired', module_name: 'manager', registered_host: 'manager.local',
    node_name: 'host-a', role: 'backend', page: '1', page_size: '10'
  })
  await page.reload()
  await expect(list.getByText('old-registration-recent-offline', { exact: true })).toBeVisible()
  await expect(list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '时间依据', exact: true }) })).toContainText('离线判定时间')
  await expect(list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '下线原因', exact: true }) })).toContainText('租约超时，疑似异常退出')
  rows[0] = { ...rows[0], status: 'up', stopped_at: null, stop_reason: '', lease_expires_at: new Date(now.getTime() + 3600000).toISOString() }
  await page.clock.fastForward(11000)
  await expect(list.getByText('没有符合条件的服务实例', { exact: true })).toBeVisible()
  await expect(page).toHaveURL(url)
  await list.getByRole('button', { name: '重置', exact: true }).click()
  await expect.poll(() => queries.at(-1)).toEqual({ page: '1', page_size: '10', status: 'up' })
  await expect(list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '时间依据', exact: true }) })).toContainText('登记时间')
  await expect(list.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '下线原因', exact: true }) })).toContainText('全部下线原因')
  await expect(list.getByRole('columnheader', { name: '离线判定时间', exact: true })).toHaveCount(0)
  await expect(list.getByText('old-registration-recent-offline', { exact: true })).toBeVisible()
  expect(errors).toEqual([])
})
