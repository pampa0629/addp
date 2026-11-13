import { expect, test } from '@playwright/test'
import { assertCDCTaskMonitoring } from './online/transfer-browser-support'
import { cdcBackend } from '../../../monitor/frontend/e2e/cdc-resources.fixture'
import { cdcTaskBackend, task } from '../../../monitor/frontend/e2e/cdc-tasks.fixture'

test('Console CDC menu, history and language use one Monitor iframe without engine grants', async ({ page }) => {
  await cdcBackend(page, { permissions: ['monitor.resource_observation.read'] })
  await page.goto('/monitor/cdc-resources?refresh=off')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('cdc-value-failedTasks')).toHaveText('1')
  await expect(frame.getByTestId('cdc-card-postgresql')).toHaveCount(0)
  await expect(page.locator('.sidebar .el-menu-item').filter({ hasText: 'CDC 监控' })).toBeVisible()
  await expect(frame.getByRole('tab', { name: '任务链路', exact: true })).toHaveCount(0)
  await expect(frame.getByRole('tab', { name: '运行资源', exact: true })).toBeVisible()
  const origin = await frame.locator('body').evaluate(() => performance.timeOrigin)
  await frame.getByTestId('cdc-card-redpanda').getByRole('button', { name: '查看趋势', exact: true }).click()
  await expect(page).toHaveURL(/stage=redpanda/)
  await expect(frame.getByTestId('cdc-trend').locator('canvas')).toBeVisible()
  expect(await frame.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  await page.goBack()
  await expect(page).not.toHaveURL(/stage=/)
  await expect(frame.getByTestId('cdc-trend').locator('canvas')).toBeVisible()
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  const restoredOrigin = await frame.locator('body').evaluate(() => performance.timeOrigin)
  await page.getByRole('button', { name: 'CN', exact: true }).click()
  await page.getByRole('menuitem', { name: 'English', exact: true }).click()
  await expect(frame.getByRole('heading', { name: 'CDC Monitoring', exact: true })).toBeVisible()
  expect(await frame.locator('body').evaluate(() => performance.timeOrigin)).toBe(restoredOrigin)
  await page.reload()
  await expect(frame.getByRole('heading', { name: 'CDC Monitoring', exact: true })).toBeVisible()
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  await page.screenshot({ path: '/tmp/addp-cdc-console-page.png', fullPage: true })
})

for (const detail of [false, true]) test(`Tenant CDC opens the Transfer ${detail ? 'definition' : 'task list'} through Console`, async ({ page }) => {
  const state = await cdcTaskBackend(page)
  await page.route('**/module-ui/transfer/**', route => route.fulfill({ contentType: 'text/html', body: '<title>Transfer navigation fixture</title><h1>Transfer destination</h1>' }))
  await page.goto('/monitor/cdc-resources?view=tasks&task_id=7&refresh=off')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('cdc-task-lag')).toHaveText('3')
  await expect(frame.getByRole('tab', { name: '运行资源', exact: true })).toHaveCount(0)
  await expect(frame.getByRole('tab', { name: '任务链路', exact: true })).toBeVisible()
  await frame.getByRole('button', { name: detail ? '查看任务定义' : '前往同步任务', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/transfer/tasks${detail ? '/7/detail' : ''}$`))
  await expect(page.frameLocator('iframe[data-testid="module-iframe"]').getByRole('heading', { name: 'Transfer destination', exact: true })).toBeVisible()
  await page.goBack()
  await expect(page).toHaveURL(/\/monitor\/cdc-resources\?view=tasks&task_id=7&refresh=off$/)
  await expect(frame.getByTestId('cdc-task-lag')).toHaveText('3')
  expect(state.calls.some(call => call.path.includes('/platform/'))).toBe(false)
})

// A same-iframe history transition must not be rewritten by the leaving view.
test('Console CDC Platform history preserves a Tenant URL as a scope tip', async ({ page }) => {
  const state = await cdcBackend(page)
  await page.goto('/monitor/cdc-resources?view=tasks')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const tip = frame.getByText('任务链路属于租户，请切换到目标租户身份查看')
  await expect(tip).toBeVisible()
  expect(state.calls).toEqual([])
  await expect(frame.getByRole('tab', { name: '任务链路', exact: true })).toHaveCount(0)
  await frame.getByRole('tab', { name: '运行资源', exact: true }).click()
  await expect(frame.getByTestId('cdc-card-kafka-connect')).toBeVisible()
  await page.goBack()
  await expect(tip).toBeVisible()
  await expect(page).toHaveURL(/view=tasks$/)
  await expect(frame.getByTestId('cdc-card-kafka-connect')).toHaveCount(0)
  expect(state.calls.some(path => path.includes('/transfer/'))).toBe(false)
})

for (const phase of ['cdc_initial', 'cdc_growth', 'cdc_resumed']) test(`CDC Hosted page assertions read current task buffer (${phase})`, async ({ page }) => {
  await cdcTaskBackend(page)
  const paused = phase === 'cdc_growth'
  const binding = { task_id: 7, engine_id: 2, generation: 1, desired_state: paused ? 'paused' : 'running',
    committed_offset: phase === 'cdc_resumed' ? '9' : '6', latest_offset: phase === 'cdc_initial' ? '6' : '9', lag_offsets: paused ? '3' : '0' }
  let executionID
  await page.route('**/api/v1/transfer/task-definitions/7', async route => {
    const value = task(7), now = new Date().toISOString(), expires = new Date(Date.now() + 30000).toISOString()
    executionID = value.last_execution_id
    value.desired_state = binding.desired_state
    value.config.source.locator = 'addp://engine/2/path/public/continuous_source?type=table'
    value.config.target = { ...value.config.target, parent_locator: 'addp://engine/2/path/public?type=schema', name: 'continuous_cdc_target' }
    value.capture.postgresql_observation = { status: 'available', sampled_at: now, expires_at: expires, slot_active: true, wal_status: 'reserved', risk_state: 'reserved', retained_wal_bytes: '32768', unacknowledged_wal_bytes: '1024', safe_wal_size_bytes: null, wal_keep_unlimited: true }
    value.capture.delay_observation = { status: 'available', sampled_at: now, expires_at: expires, connected: true, delay_seconds: phase === 'cdc_initial' ? null : .141 }
    value.capture.buffer_observation = { status: 'available', sampled_at: now, expires_at: expires, earliest_offset: '0', committed_offset: binding.committed_offset, latest_offset: binding.latest_offset, lag_offsets: binding.lag_offsets, recovery_state: 'available' }
    await route.fulfill({ json: value })
  })
  await page.goto('/monitor/cdc-resources?view=tasks&task_id=7&refresh=off')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('cdc-task-progress')).toBeVisible()
  await assertCDCTaskMonitoring(page, frame, binding, phase, executionID)
  // The historical execution fixture has lag=3; it cannot stand in for current lag=0.
  await expect(frame.getByTestId('cdc-task-lag')).toHaveText('3')
})
