import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from './module-query.fixture'

const policy = { version: 1, failure_samples: 3, recovery_samples: 3, stale_seconds: 120, delay_ms: 15000, capacity_percent: 80, recovery_percent: 70 }
async function fixture(page, { management = true } = {}) {
  await mockModuleQueryAPI(page, { pipelinePermission: true, pipelineManagement: management })
  const requests = []
  let failure = false, conflict = false, deploymentState = 'enabled'
  await page.route('**/api/v1/monitor/platform/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const headers = { 'access-control-allow-origin': req.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,DELETE,OPTIONS' }
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    const reply = (json, status = 200) => route.fulfill({ headers, json, status })
    const body = req.postDataJSON(); requests.push({ path, query: Object.fromEntries(new URL(req.url()).searchParams), method: req.method(), body })
    if (path.endsWith('/log-pipeline')) {
      if (failure) return reply({ error: '观测不可用' }, 503)
      return reply({ deployment_state: deploymentState, configured: true, health: deploymentState === 'enabled' ? 'alert' : 'unknown', notifications: 'unconfigured', node: { node: 'host-a', received_at: new Date().toISOString(), registry_valid: true, observation: { probe_delivered: true, probe_delay_ms: 100 } }, active_incidents: 1 })
    }
    if (path.endsWith('/policy')) return req.method() === 'GET' ? reply(policy) : conflict ? reply({ error: '规则版本冲突' }, 409) : reply({ ...body, version: 2 })
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

test('diagnostics expose only count and link, with no duplicate management requests', async ({ page }) => {
 const state = await fixture(page)
 await expect(page.getByTestId('log-pipeline')).toContainText('未恢复告警：1 项')
 await expect(page.getByRole('button', { name: '确认', exact: true })).toHaveCount(0)
 await expect(page.getByRole('button', { name: '通知管理', exact: true })).toHaveCount(0)
 await expect(page.getByRole('button', { name: '查看平台告警', exact: true })).toBeVisible()
 expect(state.requests.some(item => item.path.includes('notification') || item.path.includes('/alerts'))).toBe(false)
})

for (const [state, label] of [['disabled', '已关闭'], ['unconfigured', '部署配置无效']]) {
  test(`paused logging shows ${state} while preserving incidents`, async ({ page }) => {
    const fixtureState = await fixture(page)
    fixtureState.deployment(state)
    await page.getByTestId('log-pipeline').getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('pipeline-health')).toContainText(label)
    await expect(page.getByTestId('pipeline-paused')).toContainText('链路检测已暂停')
    await expect(page.getByTestId('log-pipeline')).toContainText('未恢复告警：1 项')
    await expect(page.getByText('探针成功', { exact: true })).toHaveCount(0)
  })
}
