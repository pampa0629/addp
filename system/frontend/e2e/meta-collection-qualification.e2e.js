import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const permissions = ['system.engine.read', ...['read', 'approve', 'revoke'].map(action => `system.meta_collection_qualification.${action}`)]
const facts = { schema_version: 'meta.scan_task_facts/v1', engine_id: '2', task_id: '42', scope_kind: 'engine', scan_depth: 'basic', force: false, read_options: {}, task_definition_digest: 'a'.repeat(64) }
const qualification = { id: '9810b9cd-a6b5-4dcf-a044-4a8b5388c322', engine_id: '2', task_id: '42', task_definition_digest: facts.task_definition_digest,
  engine_version: '7', scan_depth: 'basic', scope_kind: 'engine', purpose: 'Routine governance', status: 'active', version: '9007199254740993', expires_at: '2099-01-01T00:00:00Z' }

async function fixture(page, { allowed = permissions, approveFailure = 0, revokeConflict = false, taskFacts = facts, engineType = 'postgresql', language = 'zh-cn' } = {}) {
  const reads = [], writes = [], enginePolls = []
  let row = { ...qualification }, approvals = 0, revocations = 0
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,DELETE,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'collection-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: allowed }] } })
    if (path.includes('/meta_collection_task_facts/')) { reads.push(path); return reply(taskFacts) }
    if (path.includes('/meta_collection_qualifications')) {
      if (request.method() === 'GET') { reads.push(request.url()); return reply({ data: [row], total: 11, page: Number(new URL(request.url()).searchParams.get('page')), page_size: 10, total_pages: 2 }) }
      const body = request.postDataJSON(); writes.push({ method: request.method(), path, body })
      if (request.method() === 'POST') {
        approvals++
        if (approvals === 1 && approveFailure) return reply({ error: 'Task approval not confirmed', error_code: 'meta_collection_configuration_changed' }, approveFailure)
        row = { ...row, id: body.command_id, task_id: body.task_id, purpose: body.purpose, expires_at: body.expires_at, task_definition_digest: body.expected_task_definition_digest }
        return reply(row, 201)
      }
      if (request.method() === 'DELETE') {
        revocations++
        if (revocations === 1 && revokeConflict) { row = { ...row, version: '9007199254740994' }; return reply({ error: 'Qualification version changed' }, 409) }
        row = { ...row, status: 'revoked', revoked_reason: body.reason }; return reply(row)
      }
    }
    const engine = { id: '2', name: 'PG governance engine', engine_type: engineType, engine_origin: 'general', lifecycle_state: 'active', is_builtin: false,
      capabilities: { schema_version: 'engine.capabilities/v1', storage: {} } }
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines/2')) return reply(engine)
    if (path.endsWith('/engines')) { enginePolls.push(path); return reply([engine]) }
    throw new Error(`Unexpected collection fixture request: ${request.method()} ${path}`)
  })
  return { reads, writes, enginePolls }
}

async function openNew(page) {
  await page.goto('/engines/2?tab=collection-qualifications')
  await page.getByTestId('collection-workspace-tabs').getByText('新增批准', { exact: true }).click()
  await expect(page.getByTestId('collection-approve')).toBeDisabled()
  await page.getByTestId('collection-task').fill('42')
  await page.getByTestId('collection-inspect').click()
}
async function fillApproval(page) {
  await expect(page.getByTestId('collection-facts')).toContainText('整个引擎')
  await page.getByTestId('collection-purpose').fill('Routine governance')
  const date = page.getByTestId('meta-collection-qualifications').locator('.el-date-editor input')
  await date.fill('2098-01-01 00:00:00'); await date.press('Enter')
}

test('collection-only permission opens a separate read-only workspace and navigation never writes or scans', async ({ page }) => {
  const { reads, writes } = await fixture(page)
  await page.goto('/engines'); await page.getByTestId('engine-authorization-open').click()
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2?tab=collection-qualifications`)
  await expect(page.getByTestId('collection-records')).toContainText('Routine governance')
  await expect(page.getByRole('tab', { name: '数据授权', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '委托授权', exact: true })).toHaveCount(0)
  await page.getByTestId('collection-workspace-tabs').getByText('新增批准', { exact: true }).click()
  await expect(page.getByTestId('collection-records')).toHaveCount(0)
  await page.getByTestId('collection-workspace-tabs').getByText('资格记录', { exact: true }).click()
  await page.getByTestId('meta-collection-qualifications').getByLabel('下一页').click()
  await expect.poll(() => reads.some(url => url.includes('page=2'))).toBe(true)
  expect(writes).toEqual([])
})

test('approval sends only inspected task identity and an explicit purpose and expiry', async ({ page }) => {
  const { writes } = await fixture(page)
  await openNew(page); await fillApproval(page)
  await page.getByTestId('collection-approve').click()
  await expect(page.getByTestId('collection-records')).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0]).toEqual({ method: 'POST', path: '/api/v1/system/engines/2/meta_collection_qualifications', body: {
    command_id: expect.any(String), task_id: '42', expected_task_definition_digest: facts.task_definition_digest,
    purpose: 'Routine governance', expires_at: new Date(2098, 0, 1).toISOString()
  } })
  await expect(page.getByTestId('meta-collection-qualifications')).toContainText('未续期或授予普通数据读取权')
})

test('an unknown approval outcome requires an explicit identical command recovery', async ({ page }) => {
  const { writes } = await fixture(page, { approveFailure: 503 })
  await openNew(page); await fillApproval(page); await page.getByTestId('collection-approve').click()
  await expect(page.getByTestId('collection-retry')).toBeVisible()
  await expect(page.getByTestId('collection-task')).toBeDisabled()
  await page.getByTestId('collection-workspace-tabs').getByText('资格记录', { exact: true }).click()
  await page.getByTestId('collection-workspace-tabs').getByText('新增批准', { exact: true }).click()
  expect(writes).toHaveLength(1)
  await page.getByTestId('collection-retry').click()
  await expect(page.getByTestId('collection-records')).toBeVisible()
  expect(writes).toHaveLength(2)
  expect(writes[1]).toEqual(writes[0])
})

test('a configuration conflict preserves the draft but requires a fresh task inspection', async ({ page }) => {
  const { writes } = await fixture(page, { approveFailure: 409 })
  await openNew(page); await fillApproval(page); await page.getByTestId('collection-approve').click()
  await expect(page.getByTestId('collection-approve')).toBeDisabled()
  await expect(page.getByTestId('collection-purpose')).toHaveValue('Routine governance')
  await expect(page.getByTestId('collection-retry')).toHaveCount(0)
  await page.getByTestId('collection-inspect').click()
  await expect(page.getByTestId('collection-approve')).toBeEnabled()
  expect(writes).toHaveLength(1)
})

for (const change of [{ scan_depth: 'deep' }, { engine_id: '3' }, { read_options: { has_header: false } }]) {
  test(`unsupported or mismatched facts cannot be approved: ${JSON.stringify(change)}`, async ({ page }) => {
    const { writes } = await fixture(page, { taskFacts: { ...facts, ...change } })
    await openNew(page)
    await expect(page.getByTestId('meta-collection-qualifications')).toContainText('首期仅支持')
    await expect(page.getByTestId('collection-approve')).toBeDisabled()
    expect(writes).toEqual([])
  })
}

test('editing the task ID discards a delayed previous inspection', async ({ page }) => {
  const { writes } = await fixture(page)
  let release
  const pending = new Promise(resolve => { release = resolve })
  await page.route('**/meta_collection_task_facts/42', async route => { await pending; await route.fallback() })
  await page.goto('/engines/2?tab=collection-qualifications')
  await page.getByTestId('collection-workspace-tabs').getByText('新增批准', { exact: true }).click()
  await page.getByTestId('collection-task').fill('42'); await page.getByTestId('collection-inspect').click()
  await page.getByTestId('collection-task').fill('43'); release()
  await expect(page.getByTestId('collection-approve')).toBeDisabled()
  await expect(page.getByTestId('collection-facts')).toHaveCount(0)
  expect(writes).toEqual([])
})

test('a transport failure leaves facts unconfirmed rather than declaring the task unsupported', async ({ page }) => {
  const { writes } = await fixture(page)
  await page.route('**/meta_collection_task_facts/42', route => route.abort('failed'))
  await openNew(page)
  await expect(page.getByTestId('meta-collection-qualifications')).toContainText('操作结果未确认')
  await expect(page.getByTestId('collection-facts')).toHaveCount(0)
  await expect(page.getByTestId('collection-approve')).toBeDisabled()
  expect(writes).toEqual([])
})

test('revocation conflicts preserve the reason and require explicit refresh before using the new exact version', async ({ page }) => {
  const { writes } = await fixture(page, { revokeConflict: true })
  await page.goto('/engines/2?tab=collection-qualifications')
  await page.getByTestId('collection-revoke').click()
  await page.getByTestId('collection-revoke-reason').fill('Stop this task only')
  await page.getByTestId('collection-revoke-confirm').click()
  await expect(page.getByTestId('collection-revoke-reason')).toHaveValue('Stop this task only')
  await expect(page.getByTestId('collection-revoke-confirm')).toBeDisabled()
  expect(writes).toHaveLength(1)
  expect(writes[0].body).toEqual({ version: '9007199254740993', reason: 'Stop this task only' })
  await page.getByTestId('collection-review-revoke').click()
  await expect(page.getByTestId('collection-revoke-confirm')).toBeEnabled()
  expect(writes).toHaveLength(1)
  await page.getByTestId('collection-revoke-confirm').click()
  await expect(page.getByTestId('collection-records')).toContainText('已撤销')
  expect(writes[1].method).toBe('DELETE')
  expect(writes[1].body).toEqual({ version: '9007199254740994', reason: 'Stop this task only' })
})

test('a read-only reviewer sees records without write controls', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: ['system.engine.read', 'system.meta_collection_qualification.read'] })
  await page.goto('/engines/2?tab=collection-qualifications')
  await expect(page.getByTestId('collection-records')).toContainText('Routine governance')
  await expect(page.getByTestId('collection-workspace-tabs').getByRole('radio', { name: '新增批准', exact: true })).toBeDisabled()
  await expect(page.getByTestId('collection-revoke')).toHaveCount(0)
  expect(writes).toEqual([])
})

test('ordinary engine status polling preserves the inspected task and unsaved approval draft', async ({ page }) => {
  const { writes, enginePolls } = await fixture(page)
  await openNew(page); await fillApproval(page)
  await expect.poll(() => enginePolls.length, { timeout: 15000 }).toBeGreaterThanOrEqual(2)
  await expect(page.getByTestId('collection-task')).toHaveValue('42')
  await expect(page.getByTestId('collection-purpose')).toHaveValue('Routine governance')
  await expect(page.getByTestId('collection-facts')).toBeVisible()
  expect(writes).toEqual([])
})

test('approval permission alone cannot inspect or approve a task', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read', 'system.meta_collection_qualification.approve'] })
  await page.goto('/engines/2?tab=collection-qualifications')
  await expect(page.getByTestId('collection-inspect')).toBeDisabled()
  await expect(page.getByTestId('collection-approve')).toBeDisabled()
  await expect(page.getByTestId('meta-collection-qualifications')).toContainText('需要采集资格读取权限')
  expect(reads).toEqual([]); expect(writes).toEqual([])
})

for (const changed of [false, true]) {
  test(`credential rotation preserves only same-authority commands (authority changed: ${changed})`, async ({ page }) => {
    const { writes } = await fixture(page, { approveFailure: 503 })
    await openNew(page); await fillApproval(page); await page.getByTestId('collection-approve').click()
    await expect(page.getByTestId('collection-retry')).toBeVisible()
    let release, arrived
    const held = new Promise(resolve => { release = resolve }), observed = new Promise(resolve => { arrived = resolve })
    await page.route('**/api/v1/system/auth/context', async route => {
      arrived(); await held
      await route.fulfill({ json: { principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
        authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: changed ? permissions.filter(p => !p.endsWith('.approve')) : permissions }] } } })
    })
    try {
      await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().setToken('rotated-collection-fixture', 3600))
      await observed
      await expect(page.getByTestId('meta-collection-qualifications')).toHaveCount(1)
      await expect(page.getByTestId('collection-retry')).not.toBeVisible()
      expect(writes).toHaveLength(1)
    } finally { release() }
    if (changed) {
      await expect(page.getByTestId('collection-records')).toBeVisible()
      await expect(page.getByTestId('collection-retry')).toHaveCount(0)
      await expect(page.getByTestId('collection-workspace-tabs').getByRole('radio', { name: '新增批准', exact: true })).toBeDisabled()
    } else {
      await expect(page.getByTestId('collection-purpose')).toHaveValue('Routine governance')
      await expect(page.getByTestId('collection-retry')).toBeVisible()
      expect(writes).toHaveLength(1)
      await page.getByTestId('collection-retry').click()
      await expect(page.getByTestId('collection-records')).toBeVisible()
      expect(writes).toHaveLength(2); expect(writes[1]).toEqual(writes[0])
    }
  })
}

for (const options of [{ allowed: ['system.engine.read', 'system.engine_access_grant.create'] }, { engineType: 'mysql' }]) {
  test(`unqualified permissions or unsupported engine cannot open collection: ${JSON.stringify(options)}`, async ({ page }) => {
    const { reads, writes } = await fixture(page, options)
    await page.goto('/engines/2?tab=collection-qualifications')
    await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2`)
    await expect(page.getByTestId('meta-collection-qualifications')).toHaveCount(0)
    expect(reads).toEqual([]); expect(writes).toEqual([])
  })
}

test('English collection workspace remains usable in a narrow window', async ({ page }) => {
  const { writes } = await fixture(page, { language: 'en' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/engines/2?tab=collection-qualifications')
  await expect(page.getByRole('tab', { name: 'Collection qualification', exact: true })).toBeVisible()
  await page.getByTestId('collection-workspace-tabs').getByText('New approval', { exact: true }).click()
  await expect(page.getByTestId('collection-inspect')).toBeVisible()
  const bounds = await page.getByTestId('collection-task').boundingBox()
  expect(bounds.x).toBeGreaterThanOrEqual(0); expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
  expect(writes).toEqual([])
})
