import { test, expect } from '@playwright/test'
import { cdcTaskBackend } from './cdc-tasks.fixture'

const alerts = ['cdc_source_recovery_lost', 'cdc_buffer_recovery_lost'].map((code, index) => ({
  id: index + 1, module: 'transfer', task_type: 'sync', source_task_id: '7',
  execution_id: '10000000-0000-4000-8000-000000000001', signal_code: code,
  severity: 'critical', status: 'open', opened_at: '2026-11-13T03:00:00Z',
  last_observed_at: '2026-11-13T03:01:00Z', details: { generation: '3', reason: index ? 'buffer_position_lost' : 'wal_lost' }
}))

for (const locale of ['zh-cn', 'en']) test(`CDC risk alerts show task generation and link to the current task (${locale})`, async ({ page }) => {
  const state = await cdcTaskBackend(page, { locale, alerts, permissions: ['monitor.alert_rule.read', 'monitor.alert_incident.read', 'monitor.execution.read', 'transfer.task.read'] })
  await page.goto('/alerts')
  await expect(page.getByTestId('cdc-alert-reason').first()).toContainText(locale === 'en' ? 'Capture generation 3' : '捕获代次 3')
  await expect(page.getByTestId('cdc-alert-reason').first()).toContainText(locale === 'en' ? 'WAL required by this task has been lost' : '任务所需 WAL 已丢失')
  await expect(page.getByTestId('cdc-alert-reason').last()).toContainText(locale === 'en' ? 'precedes message retention' : '消息保留范围')
  await expect(page.getByTestId('cdc-alert-open-task')).toHaveCount(2)
  await page.getByTestId('cdc-alert-open-task').first().click()
  await expect(page).toHaveURL(/cdc-resources\?view=tasks&task_id=7/)
  await expect(page.getByTestId('cdc-task-source')).toContainText('public.orders_7')
  expect(state.calls.some(row => row.path.includes('/runtime/cdc-risk-observations'))).toBe(false)
})

test('CDC task actions are absent without task and execution permissions', async ({ page }) => {
  await cdcTaskBackend(page, { alerts, permissions: ['monitor.alert_rule.read', 'monitor.alert_incident.read'] })
  await page.goto('/alerts')
  await expect(page.getByTestId('cdc-alert-reason')).toHaveCount(2)
  await expect(page.getByTestId('cdc-alert-open-task')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '确认', exact: true })).toHaveCount(0)
})

for (const locale of ['zh-cn', 'en']) test(`Capture failure warning uses current task permissions (${locale})`, async ({ page }) => {
  const warning = { ...alerts[0], signal_code: 'cdc_capture_failed', severity: 'warning',
    details: { generation: '3', reason: 'source_disconnected', trigger_seconds: 60 } }
  await cdcTaskBackend(page, { locale, alerts: [warning], permissions: ['monitor.alert_rule.read', 'monitor.alert_incident.read', 'monitor.execution.read', 'transfer.task.read'] })
  await page.goto('/alerts')
  await expect(page.getByText(locale === 'en' ? 'CDC capture has persistently failed' : 'CDC 捕获持续失败', { exact: true })).toBeVisible()
  await expect(page.getByTestId('cdc-alert-reason')).toContainText(locale === 'en' ? 'Capture is disconnected from the source' : '捕获组件与源端已断连')
  await expect(page.getByTestId('cdc-capture-failure-duration')).toContainText('60')
  await expect(page.getByTestId('cdc-alert-open-task')).toHaveCount(1)
})


for (const locale of ['zh-cn', 'en']) test(`CDC performance alerts retain pause and task permissions (${locale})`, async ({ page }) => {
  const warning = { ...alerts[0], signal_code: 'cdc_consumer_stalled', severity: 'warning',
    details: { generation: '3', evaluation_state: 'paused', trigger_seconds: 180, lag_offsets: '3' } }
  await cdcTaskBackend(page, { locale, alerts: [warning], permissions: ['monitor.alert_rule.read', 'monitor.alert_incident.read', 'monitor.execution.read', 'transfer.task.read'] })
  await page.goto('/alerts')
  await expect(page.getByText(locale === 'en' ? 'CDC target consumption stalled' : 'CDC 目标消费停滞', { exact: true })).toBeVisible()
  await expect(page.getByTestId('cdc-performance-evaluation')).toContainText(locale === 'en' ? 'Task paused' : '任务暂停')
  await expect(page.getByTestId('cdc-alert-open-task')).toHaveCount(1)
})

for (const locale of ['zh-cn', 'en']) test(`CDC rule form selects only current target policies (${locale})`, async ({ page }) => {
  const state=await cdcTaskBackend(page, { locale, targets: [{module:'transfer',task_type:'sync',source_task_id:'7',source_task_name:'Orders CDC',cdc:true,capture_delay_available:true}],
    permissions: ['monitor.alert_rule.read','monitor.alert_rule.create','monitor.notification_destination.read','monitor.alert_incident.read'] })
  await page.goto('/alerts?tab=rules')
  await page.getByRole('button', { name: locale === 'en' ? 'Add Rule' : '新增规则', exact:true }).click()
  const dialog=page.getByRole('dialog')
  await dialog.locator('input').first().fill('Backlog Acceptance')
  await dialog.locator('.el-select').first().click()
  await page.getByRole('option', {name:'Orders CDC (transfer / sync / 7)',exact:true}).click()
  await dialog.locator('.el-select').nth(1).click()
  await expect(page.getByRole('option', {name:locale==='en'?'CDC target consumption stalled':'CDC 目标消费停滞',exact:true})).toBeVisible()
  await page.getByRole('option', {name:locale==='en'?'CDC backlog persistently high':'CDC 积压持续偏高',exact:true}).click()
  const threshold=dialog.locator('input[inputmode="numeric"]')
  await expect(threshold).toHaveValue('10000')
  await threshold.fill('9007199254740993')
  await expect(threshold).toHaveValue('9007199254740993')
  await expect(dialog.getByText(locale==='en'?'Critical':'严重',{exact:true})).toHaveCount(0)
  await dialog.getByRole('button',{name:locale==='en'?'Save':'保存',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  const saved=state.calls.find(row=>row.method==='POST' && row.path.endsWith('/monitor/alert-rules')).body
  expect(saved.backlog_threshold).toBe('9007199254740993')
  expect(saved.rule_type).toBe('cdc_backlog_high')
  expect(saved.severity).toBe('warning')
})
