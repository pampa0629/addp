import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

test('unregistered sources use trusted capture times and the existing log drawer', async ({ page }) => {
  await mockModuleQueryAPI(page, { logPermission: true })
  const queries = [], logs = []
  const headers = { 'access-control-allow-origin': 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true' }
  let failure = false
  await page.route('**/api/v1/system/platform/module-log-sources?*', route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    const params = Object.fromEntries(new URL(route.request().url()).searchParams); queries.push(params)
    if (failure) return route.fulfill({ headers, status: 500, json: { error: '目录查询失败' } })
    return route.fulfill({ headers, json: { data: [{ module_name: 'copilot', instance_id: 'startup-1', role: 'backend', host_node_name: 'host-a', capture_started_at: '2026-10-01T11:59:00Z', observed_at: '2026-10-01T12:00:00Z' }], page: 1, page_size: 20, total: 1, total_pages: 1, discovery_state: 'unknown', discovery_issues: [{ code: 'metadata_missing', count: 4 }, { code: 'metadata_invalid', count: 2 }] } })
  })
  await page.route('**/api/v1/system/platform/modules/*/instances/*/logs?*', route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    logs.push(route.request().url())
    const q = new URL(route.request().url()).searchParams
    return route.fulfill({ headers, json: { entries: [{ entry_id: 'startup-1:1', timestamp: '2026-10-01T11:59:10Z', level: 'error', channel: 'stderr', message: 'startup diagnostic' }], returned: 1, has_more: false, from: q.get('from'), to: q.get('to'), collection_state: 'unknown' } })
  })
  await page.goto('/modules?tab=unregistered-logs')
  const table = page.getByTestId('module-log-sources')
  await expect(table).toContainText('startup-1')
  await expect(table).toContainText('未确认')
  await expect(page.getByText(/日志来源发现不完整或已失联/)).toBeVisible()
  await expect(page.getByTestId('source-discovery-issues')).toContainText('缺少必需采集元数据')
  await expect(page.getByTestId('source-discovery-issues')).toContainText('本次发现 4 处')
  await expect(page.getByTestId('source-discovery-issues')).toContainText('元数据无效或身份不一致')
  await expect(page.getByTestId('source-discovery-issues')).toContainText('本次发现 2 处')
  await expect(table.getByText('DOWN', { exact: false })).toHaveCount(0)
  await page.getByPlaceholder('宿主节点名（精确匹配）').fill('host-b')
  await expect.poll(() => queries.at(-1)?.node_name).toBe('host-b')
  await table.getByRole('button', { name: '查看日志', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByTestId('runtime-log-output')).toContainText('startup diagnostic')
  await expect(drawer.getByText('首次采集时间', { exact: true })).toBeVisible()
  await expect(drawer.getByText('进程启动时间', { exact: true })).toHaveCount(0)
  expect(logs[0]).toContain('/modules/copilot/instances/startup-1/logs?')
  await drawer.getByRole('button', { name: '关闭此对话框' }).click()
  await expect(drawer).toBeHidden()
  failure = true
  await page.getByRole('button', { name: '重置', exact: true }).click()
  await expect(page.getByText('目录查询失败', { exact: true })).toBeVisible()
  await expect(table).not.toContainText('startup-1')
})

test('module read permission does not expose unregistered log sources', async ({ page }) => {
  await mockModuleQueryAPI(page)
  await page.goto('/modules?tab=unregistered-logs')
  await expect(page.getByRole('tab', { name: '未登记实例的运行日志' })).toHaveCount(0)
  await expect(page.getByText('无权查看平台运行日志', { exact: true })).toBeVisible()
  await expect(page.getByTestId('module-log-sources')).toHaveCount(0)
})

test('source diagnostics distinguish stale observation and recovery', async ({ page }) => {
  await mockModuleQueryAPI(page, { logPermission: true })
  const headers = { 'access-control-allow-origin': 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true' }
  let code = 'observer_stale'
  await page.route('**/api/v1/system/platform/module-log-sources?*', route => {
    if (route.request().method() === 'OPTIONS') return route.fallback()
    return route.fulfill({ headers, json: { data: [], total: 0, page: 1, page_size: 20, total_pages: 0, discovery_state: code ? 'unknown' : 'observed', discovery_issues: code ? [{ code }] : [], observation: { sampled_at: '2026-10-01T12:00:00Z', scan_issues: [{ code: 'metadata_missing', count: 99 }] } } })
  })
  await page.goto('/modules?tab=unregistered-logs')
  const issues = page.getByTestId('source-discovery-issues')
  await expect(issues).toContainText('来源观测报告已超时')
  await expect(issues).toContainText('System 连通性和服务身份认证')
  await expect(issues).not.toContainText('99')
  for (const [value, title] of [['observer_unobserved', '尚未收到来源观测报告'], ['diagnostics_unavailable', '尚无来源扫描诊断证据']]) {
    code = value
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(issues).toContainText(title)
  }
  code = ''
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(issues).toHaveCount(0)
  await expect(page.getByText(/日志来源发现不完整或已失联/)).toHaveCount(0)
})
