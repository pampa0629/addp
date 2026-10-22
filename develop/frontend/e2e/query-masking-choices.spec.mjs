import { expect, test } from '@playwright/test'
import { developAuthContext } from './developAuthContext.js'

const locator = 'addp://engine/11/path/public/customers?type=table'
const field = { target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'fixture-fingerprint' }, component_key: 'phone', policy_id: 'policy-uuid', assessment_revision: 1, policy_revision: 1, options: [{ id: 'hash-uuid', name: '统计哈希' }, { id: 'mask-uuid', name: '业务掩码' }] }
const engine = { id: 11, name: 'PostgreSQL Demo', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online', capabilities: { compute: { query: { supported: true, languages: ['sql'], default_language: 'sql', result_kinds: ['table'] } } } }
const reply = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

for (const locale of ['zh-cn', 'en']) test(`saved masking choice survives reload and is reused without a per-query prompt (${locale})`, async ({ page }, testInfo) => {
  const updates = [], executions = [], observations = [], errors = []
  page.on('pageerror', error => errors.push(error.message))
  let revision = 1
  let task = { id: 17, name: 'role-choice-query', dev_type: 'query', timeout: 300, content: { query: 'SELECT phone FROM public.customers', query_type: 'sql', target_locator: locator, query_parameters: [], protection_choices: [] }, execution_config: { engine_id: 11 } }
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), locale)
  await page.route('**/api/v1/**', route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    if (path === '/api/v1/system/auth/context') return reply(route, developAuthContext)
    if (path === '/api/v1/system/users/me') return reply(route, { id: 1, username: 'choice-user' })
    if (path === '/api/v1/system/refresh') return reply(route, { access_token: 'fixture-token', expires_in: 3600 })
    if (path === '/api/v1/develop/engines' || path === '/api/v1/meta/engines') return reply(route, [engine])
    if (path === '/api/v1/develop/task-definitions/17') {
      if (req.method() === 'PUT') {
        const body = req.postDataJSON(); updates.push(body)
        if (body.content.protection_choices.some(choice => choice.selection.policy_revision !== revision)) return reply(route, { error: '所选脱敏方式已变更，请重新选择。', error_code: 'query_protection_choice_invalid' }, 409)
        task = { ...task, ...body }
      }
      return reply(route, task)
    }
    if (path === '/api/v1/develop/query-protection-options') {
      observations.push(Object.fromEntries(url.searchParams))
      return reply(route, { fields: [{ ...field, policy_revision: revision }] })
    }
    if (path === '/api/v1/develop/query-preflight') return reply(route, { allowed: true, effect: 'read', statement: 'select', schema_coverage: 'complete', diagnostics: [], risk_level: 'low', requires_confirmation: false })
    if (path === '/api/v1/develop/executions' && req.method() === 'POST') { executions.push(req.postDataJSON()); return reply(route, { execution_id: 'role-choice-execution' }) }
    if (path.startsWith('/api/v1/develop/executions/')) return reply(route, { execution_id: 'role-choice-execution', module: 'develop', task_type: 'query', status: 'success', progress: 100, metadata: { result: { result_kind: 'table', columns: ['phone'], fields: [{ name: 'phone', type: 'string' }], rows_count: 1, summary: { preview_rows: [{ phone: 'fixture-hash' }] } } } })
    if (path.startsWith('/api/v1/meta/resource-tree/')) return reply(route, { id: 'engine', label: engine.name, type: 'database', locator: 'addp://engine/11/path?type=server', children: [] })
    return reply(route, {})
  })
  const labels = locale === 'zh-cn' ? { methods: '脱敏方式', save: '保存查询', execute: '执行', refresh: '刷新方式', defaults: '默认方式' } : { methods: 'Masking methods', save: 'Save Query', execute: 'Execute', refresh: 'Refresh methods', defaults: 'Default method' }
  await page.goto('/sql?action=edit&id=17')
  await expect(page.locator('.query-task-name')).toHaveText('role-choice-query')
  await page.getByRole('button', { name: labels.methods, exact: true }).click()
  const drawer = page.getByRole('dialog', { name: labels.methods, exact: true })
  await expect(drawer.getByText('phone', { exact: true })).toBeVisible()
  await drawer.locator('.el-select').click()
  await page.getByRole('option', { name: '统计哈希', exact: true }).click()
  await drawer.getByRole('button', { name: /close|关闭/i }).click()
  await page.getByRole('button', { name: labels.save, exact: true }).click()
  await expect.poll(() => updates.length).toBe(1)
  const expected = [{ target: field.target, component_key: 'phone', selection: { policy_id: field.policy_id, assessment_revision: 1, policy_revision: 1, option_id: 'hash-uuid' } }]
  expect(updates[0].content.protection_choices).toEqual(expected)
  await page.reload()
  await expect(page.locator('.query-task-name')).toHaveText('role-choice-query')
  await page.getByRole('button', { name: labels.execute, exact: true }).click()
  await expect.poll(() => executions.length).toBe(1)
  expect(executions[0].content.protection_choices).toEqual(expected)
  expect(JSON.stringify(executions[0])).not.toContain('role_keys')
  revision = 2
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: /^(脱敏方式|Masking methods)/ }).click()
  await expect(drawer.locator('.el-select')).toContainText(locale === 'zh-cn' ? '已保存的选择当前不可用' : 'A saved choice is unavailable')
  expect(task.content.protection_choices).toEqual(expected)
  expect(observations.every(item => item.engine_id === '11' && item.target_locator === locator && Object.keys(item).length === 2)).toBe(true)
  const bounds = await drawer.boundingBox(); expect(Math.round(bounds.width)).toBeLessThanOrEqual(390)
  expect(await drawer.evaluate(element => element.clientWidth <= window.innerWidth)).toBe(true)
  await expect.poll(async () => { const box = await drawer.boundingBox(); return Math.round(box.x) >= 0 && Math.round(box.x + box.width) <= 390 }).toBe(true)
  await expect(page.locator('.el-message--error')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath(`masking-choice-stale-${locale}.png`) })
  await drawer.locator('.el-select').click()
  await page.getByRole('option', { name: labels.defaults, exact: true }).click()
  await drawer.getByRole('button', { name: /close|关闭/i }).click()
  await page.getByRole('button', { name: labels.save, exact: true }).click()
  await expect.poll(() => updates.length).toBe(2)
  expect(updates[1].content.protection_choices).toEqual([])
  expect(errors).toEqual([])
})
