import { expect, test } from '@playwright/test'

const permissions = ['system.engine.read', 'system.engine_access_approval_requirement.read',
  'system.engine_access_approval_requirement.initialize', 'system.engine_catalog.read']
const rootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }] }
const schemaPath = { ...rootPath, segments: [...rootPath.segments, { term: 'schema', kind: 'namespace', name: 'outdoor' }] }
const tablePath = { ...rootPath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'activities' }] }
function filterGrants(items, query) {
  const search = query.get('table_search')?.toLowerCase(), account = query.get('account_id')
  return items.filter(item => (!search || item.catalog_path.segments.map(segment => segment.name).join(' / ').toLowerCase().includes(search)) &&
    (!account || item.recipient_type === 'user' && String(item.recipient_id) === account))
}
async function fixture(page, { allowed = permissions, conflict = false, denied = false, existing = false, failWrite = false, grantFailure = false, grantDuplicate = false, secondTable = false, history = [], memberStatus = 'active', identityFailure = false, changeConflict = false, changeUnknown = false, language = 'zh-cn', inspectionReason = 'grant', inspectionDenied = false } = {}) {
  const writes = [], reads = [], inspections = []
  let rows = existing ? [{ id: 'cc0a8000-6000-4000-8000-800000000001', engine_id: '2', mode: typeof existing === 'string' ? existing : 'catalog', version: 1, catalog_path: tablePath }] : []
  let grants = [...history], failedGrant = false, failedChange = false
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'configuration-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: allowed }] } })
    if (path.endsWith('/tenant/memberships') || path.endsWith('/tenant/departments') || path.endsWith('/tenant/project_groups')) {
      reads.push(request.url())
      if (identityFailure) return reply({ error: 'Name lookup unavailable' }, 503)
      if (path.endsWith('/tenant/memberships')) return reply({ data: [{ id: '34', principal_id: '33', principal_type: 'user', display_name: 'Outdoor reader', username: 'outdoor', status: memberStatus, principal_status: memberStatus }], total_pages: 1 })
      const department = path.endsWith('/tenant/departments')
      return reply({ data: [{ id: '33', name: department ? 'Outdoor department' : 'Outdoor team', code: department ? 'outdoor_dept' : 'outdoor_stat', status: 'active' }], total_pages: 1 })
    }
    if (path.endsWith('/access_delegations')) {
      reads.push(path)
      return reply({ data: [], total: 0, page: 1, page_size: 10, total_pages: 1 })
    }
    if (path.endsWith('/access_grants/history')) {
      reads.push(request.url())
      const query = new URL(request.url()).searchParams, page = Number(query.get('page') || 1), size = Number(query.get('page_size') || 20)
      const filtered = filterGrants(grants, query)
      return reply({ data: filtered.slice((page - 1) * size, page * size), total: filtered.length, page, page_size: size, total_pages: Math.ceil(filtered.length / size) })
    }
    if (path.endsWith('/access_grants/inspection')) {
      const body = request.postDataJSON(); inspections.push(body)
      if (typeof body.catalog_path?.engine_id !== 'number' || typeof body.account_id !== 'string') return reply({ error: '无效的请求参数' }, 400)
      if (inspectionDenied) return reply({ error: '本引擎管理资格不足', error_code: 'forbidden' }, 403)
      return reply({ account_id: body.account_id, catalog_path: body.catalog_path, observed_at: '2026-10-09T08:00:00Z', rule_covered: inspectionReason === 'grant', reason: inspectionReason,
        sources: ['grant', 'explicit_deny'].includes(inspectionReason) ? ['user', 'department', 'project_group'].map(recipient_type => ({ recipient_type, recipient_id: '33', expiry_mode: 'until_revoked', expires_at: null, grant_count: 1 })) : [] })
    }
    if (path.endsWith('/access_grants')) {
      reads.push(request.url())
      if (request.method() === 'GET') {
        const grouped = new Map()
        for (const grant of grants.filter(item => !item.revocation && (!item.expires_at || new Date(item.expires_at) > new Date()))) {
          const key = JSON.stringify([grant.catalog_path, grant.recipient_type, grant.recipient_id])
          const previous = grouped.get(key)
          grouped.set(key, { ...grant, grant_count: (previous?.grant_count || 0) + 1 })
        }
        const current = [...grouped.values()], query = new URL(request.url()).searchParams, page = Number(query.get('page') || 1), size = Number(query.get('page_size') || 20)
        const filtered = filterGrants(current, query)
        return reply({ data: filtered.slice((page - 1) * size, page * size), total: filtered.length, page, page_size: size, total_pages: Math.ceil(filtered.length / size) })
      }
      const body = request.postDataJSON(); writes.push({ path, body })
      if (grantDuplicate) return reply({ error: '该接收方已有此数据的有效读取授权，未重复发放', error_code: 'engine_access_grant_relation_exists' }, 409)
      if (conflict) return reply({ error: '本表已有批准安排，不能覆盖' }, 409)
      if (grantFailure && !failedGrant) { failedGrant = true; return reply({ error: '签发结果未知，请按原命令核对' }, 503) }
      const grant = { ...body, approval_mode: 'independent', engine_id: '2', granted_at: '2026-10-07T00:00:00Z', revocation: null }
      grants.push(grant)
      if (body.initialize_approval) rows.push({ engine_id: '2', mode: 'independent', version: 1, catalog_path: body.catalog_path })
      return reply(grant, 201)
    }
    if (path.includes('/access_grants/') && path.endsWith('/revoke')) {
      const body = request.postDataJSON(); writes.push({ path, body })
      const id = path.split('/').at(-2), anchor = grants.find(item => item.request_id === id)
      const same = item => JSON.stringify([item.catalog_path, item.recipient_type, item.recipient_id]) === JSON.stringify([anchor.catalog_path, anchor.recipient_type, anchor.recipient_id])
      const ids = grants.filter(item => same(item) && !item.revocation).map(item => item.request_id)
      const revocation = { request_id: id, revoked_at: '2026-10-07T01:00:00Z', reason: body.reason, revoked_request_ids: ids }
      grants = grants.map(item => same(item) && !item.revocation ? { ...item, revocation: { ...revocation, request_id: item.request_id } } : item)
      return reply(revocation)
    }
    if (path.includes('/access_approval_requirements/')) {
      if (request.method() === 'GET') { reads.push(path); return reply(rows[0]) }
      const body = request.postDataJSON(); writes.push({ path, body })
      if (changeConflict && !failedChange) {
        failedChange = true; rows[0] = { ...rows[0], version: 2 }
        return reply({ error: 'Mode changed', error_code: 'resource_version_conflict' }, 409)
      }
      rows[0] = { ...rows[0], mode: body.mode, version: body.version + 1 }
      if (changeUnknown && !failedChange) { failedChange = true; return reply({ error: 'Result unknown, review the current mode' }, 503) }
      return reply(rows[0])
    }
    if (path.endsWith('/access_approval_requirements')) {
      if (request.method() === 'GET') { reads.push(path); return denied ? reply({ error: '本引擎管理委派不足' }, 403) : reply({ data: rows, total: rows.length, page: 1, page_size: 10, total_pages: 1 }) }
      const body = request.postDataJSON(); writes.push({ path, body })
      if (conflict) return reply({ error: '本表已有批准安排，不能覆盖' }, 409)
      if (failWrite) return reply({ error: '结果尚未确认，请核对' }, 503)
      const returnedPath = { ...body.catalog_path, segments: body.catalog_path.segments.map(segment => ({ kind: segment.kind, name: segment.name, term: segment.term })) }
      rows = [{ id: 'cc0a8000-6000-4000-8000-800000000002', engine_id: '2', mode: body.mode, version: 1, catalog_path: returnedPath }]
      return reply(rows[0], 201)
    }
    if (path.endsWith('/catalog/entries')) return reply({ data: [], total: 0 })
    if (path.endsWith('/catalog/children')) {
      reads.push(path)
      const segments = request.postDataJSON().path.segments
      if (!segments.length) return reply({ nodes: [{ name: '', path: rootPath, term: 'server', kind: 'server', role: 'branch' }] })
      if (segments.length === 1) return reply({ nodes: [{ name: 'outdoor', path: schemaPath, term: 'schema', kind: 'namespace', role: 'branch' }] })
      if (segments.length === 2) return reply({ nodes: [
        { name: 'activities', path: tablePath, term: 'table', kind: 'table', role: 'leaf' },
        ...(secondTable ? [{ name: 'persons', path: { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'persons' }] }, term: 'table', kind: 'table', role: 'leaf' }] : []),
        { name: 'activities_view', path: { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'view', name: 'activities_view' }] }, term: 'table', kind: 'view', role: 'leaf' }
      ] })
      throw new Error('Unexpected leaf browse')
    }
    const engine = { id: '2', name: '授权验证 PostgreSQL', engine_type: 'postgresql', engine_origin: 'general', lifecycle_state: 'active', connection_status: 'online', capability_groups: ['storage'] }
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines/2')) return reply(engine)
    if (path.endsWith('/engines')) return reply([engine])
    throw new Error(`Unexpected configuration fixture request: ${request.method()} ${path}`)
  })
  return { writes, reads, inspections }
}

test('account inspection reuses the selected exact table and shows all three sources without granting', async ({ page }) => {
  const { writes, inspections } = await fixture(page, { allowed: [...permissions, 'system.engine_access_grant.read', 'iam.tenant_membership.read', 'iam.department.read', 'iam.project_group.read'] })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('source-grant-inspection')
  await panel.getByTestId('inspection-account').locator('.el-select').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await expect(panel.getByTestId('inspection-query')).toBeDisabled()
  await selectInspectionTable(page)
  await panel.getByTestId('inspection-query').click()
  await expect(panel.getByTestId('inspection-result')).toContainText('源规则覆盖')
  await expect(panel.getByTestId('inspection-sources')).toContainText('Outdoor reader')
  await expect(panel.getByTestId('inspection-sources')).toContainText('Outdoor department')
  await expect(panel.getByTestId('inspection-sources')).toContainText('Outdoor team')
  expect(inspections).toEqual([{ account_id: '33', catalog_path: tablePath }])
  expect(writes).toEqual([])
  await panel.screenshot({ path: '/tmp/addp-account-inspection.jpg' })
  await panel.getByTestId('inspection-account').hover()
  await panel.getByTestId('inspection-account').locator('.el-select__clear').click()
  await expect(panel.getByTestId('inspection-result')).toHaveCount(0)
})

test('changing the selected table discards an in-flight inspection response', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: [...permissions, 'system.engine_access_grant.read', 'iam.tenant_membership.read'], secondTable: true })
  let resolveStarted, release
  const started = new Promise(resolve => { resolveStarted = resolve })
  const response = new Promise(resolve => { release = resolve })
  await page.route('**/access_grants/inspection', async route => {
    const body = route.request().postDataJSON()
    resolveStarted(); await response
    await route.fulfill({ json: { account_id: body.account_id, catalog_path: body.catalog_path, observed_at: '2026-10-09T08:00:00Z', rule_covered: true, reason: 'grant', sources: [] } })
  })
  await page.goto('/engines/2?tab=data-authorization')
  await selectInspectionTable(page)
  const panel = page.getByTestId('source-grant-inspection')
  await panel.getByTestId('inspection-account').locator('.el-select').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await panel.getByTestId('inspection-query').click()
  await started
  await selectInspectionTable(page, 'persons')
  const finished = page.waitForResponse('**/access_grants/inspection')
  release(); await finished
  await expect(panel.getByTestId('inspection-target')).toContainText('persons')
  await expect(panel.getByTestId('inspection-result')).toHaveCount(0)
  expect(writes).toEqual([])
})

for (const reason of ['no_grant', 'explicit_deny', 'source_unavailable']) {
  test(`account inspection reports ${reason} separately from final data access`, async ({ page }) => {
    const { writes } = await fixture(page, { allowed: [...permissions, 'system.engine_access_grant.read', 'iam.tenant_membership.read'], inspectionReason: reason })
    await page.goto('/engines/2?tab=data-authorization')
    await selectInspectionTable(page)
    const panel = page.getByTestId('source-grant-inspection')
    await panel.getByTestId('inspection-account').locator('.el-select').click()
    await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
    await panel.getByTestId('inspection-query').click()
    await expect(panel.getByTestId('inspection-result')).toContainText(reason === 'no_grant' ? '没有有效' : reason === 'explicit_deny' ? '命中源拒绝规则' : '账号或租户成员关系当前无效')
    if (reason === 'explicit_deny') {
      await expect(panel.getByTestId('inspection-sources')).toContainText('无接收方名称查看权限')
      await expect(panel).not.toContainText('拒绝原因')
    } else await expect(panel.getByTestId('inspection-sources')).toContainText('没有当前有效的授权来源')
    expect(writes).toEqual([])
  })
}

test('inspection denial stays in the authorization window and does not navigate to forbidden', async ({ page }) => {
  await fixture(page, { allowed: [...permissions, 'system.engine_access_grant.read', 'iam.tenant_membership.read'], inspectionDenied: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectInspectionTable(page)
  const panel = page.getByTestId('source-grant-inspection')
  await panel.getByTestId('inspection-account').locator('.el-select').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await panel.getByTestId('inspection-query').click()
  await expect(panel.getByTestId('inspection-error')).toContainText('本引擎管理资格不足')
  await expect(page).toHaveURL(/\/engines\/2\?tab=data-authorization$/)
})

test('list authorization action opens a separate window with permission-specific sections and survives reload', async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: [...permissions, 'system.engine_access_delegation.read'] })
  await page.goto('/engines')
  const row = page.getByRole('row').filter({ hasText: '授权验证 PostgreSQL' })
  await row.getByTestId('engine-authorization-open').click()
  await expect(page).toHaveURL('http://127.0.0.1:4173/engines/2?tab=data-authorization')
  const dialog = page.getByRole('dialog', { name: '引擎授权 - 授权验证 PostgreSQL', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '数据授权', exact: true })).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '委托授权', exact: true })).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '基本信息', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('engine-access-delegations')).toHaveCount(0)
  expect(reads.some(path => path.endsWith('/access_delegations'))).toBe(false)
  await dialog.getByRole('tab', { name: '委托授权', exact: true }).click()
  await expect(page).toHaveURL('http://127.0.0.1:4173/engines/2?tab=delegations')
  await expect(page.getByTestId('engine-access-delegations')).toBeVisible()
  await expect(page.getByTestId('engine-data-authorization')).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole('dialog', { name: '引擎授权 - 授权验证 PostgreSQL', exact: true })).toBeVisible()
  await expect(page.getByTestId('engine-access-delegations')).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL('http://127.0.0.1:4173/engines')
  await row.getByRole('button', { name: '详情', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '引擎详情 - 授权验证 PostgreSQL', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '基本信息', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '数据授权', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '委托授权', exact: true })).toHaveCount(0)
  expect(writes).toEqual([])
})

test('only data authorization permission exposes the authorization action but not handlers', async ({ page }) => {
  const { writes, reads } = await fixture(page)
  await page.goto('/engines')
  await page.getByTestId('engine-authorization-open').click()
  await expect(page.getByTestId('engine-data-authorization')).toBeVisible()
  await expect(page.getByRole('tab', { name: '委托授权', exact: true })).toHaveCount(0)
  expect(reads.some(path => path.endsWith('/access_delegations'))).toBe(false)
  expect(writes).toEqual([])
})


const grantPermissions = [...permissions, 'system.engine_access_grant.create', 'system.engine_access_grant.read', 'system.engine_access_grant.revoke', 'iam.tenant_membership.read']
const modeAdminPermissions = [...grantPermissions, 'system.engine_access_approval_requirement.update', 'catalog.entry.read']

async function startModeChange(page) {
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page)
  await page.getByTestId('approval-change').click()
  const dialog = page.getByRole('dialog', { name: '切换批准方式', exact: true })
  await expect(dialog).toBeVisible()
  return dialog
}
async function setChangedMode(page, mode) {
  await page.getByTestId('approval-change-mode').click()
  await page.getByRole('option', { name: mode, exact: true }).click()
}
async function confirmModeChange(page) {
  await page.getByTestId('approval-change-save').click()
  const confirm = page.locator('.el-message-box')
  await expect(confirm).toContainText('本次不发放读取权')
  await confirm.getByRole('button', { name: '确定', exact: true }).click()
}
test('mode administrator explicitly changes existing business approval without issuing or mutating grants', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: modeAdminPermissions, existing: true, history: [historicalGrant()] })
  const dialog = await startModeChange(page)
  await expect(dialog).toContainText('既有授权的范围、期限及撤销关系不变')
  await setChangedMode(page, '直接批准')
  await page.getByTestId('approval-change-reason').fill('业务流程调整，交接为直接批准')
  await page.getByTestId('approval-change-save').click()
  await page.locator('.el-message-box').getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  await expect(page.getByTestId('approval-change-reason')).toHaveValue('业务流程调整，交接为直接批准')
  await confirmModeChange(page)
  await expect(dialog).toHaveCount(0)
  await expect(page.getByTestId('approval-targets')).toContainText('直接批准')
  await expect(page.getByTestId('engine-source-grants')).toContainText('已签发')
  expect(writes).toEqual([{ path: '/api/v1/system/engines/2/access_approval_requirements/cc0a8000-6000-4000-8000-800000000001',
    body: { version: 1, mode: 'independent', reason: '业务流程调整，交接为直接批准' } }])
})
test('mode conflict preserves input and requires read-only review followed by explicit confirmation', async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: modeAdminPermissions, existing: true, changeConflict: true })
  await startModeChange(page)
  await setChangedMode(page, '直接批准')
  await page.getByTestId('approval-change-reason').fill('保留交接原因')
  await confirmModeChange(page)
  await expect(page.getByTestId('approval-change-error')).toContainText('页面不会自动重试')
  await expect(page.getByTestId('approval-change-reason')).toHaveValue('保留交接原因')
  await expect(page.getByTestId('approval-change-save')).toBeDisabled()
  expect(writes).toHaveLength(1)
  await page.getByTestId('approval-change-reload').click()
  await expect(page.getByTestId('approval-change-save')).toBeEnabled()
  expect(writes).toHaveLength(1)
  expect(reads.some(path => path.includes('/access_approval_requirements/'))).toBe(true)
  await confirmModeChange(page)
  await expect(page.getByTestId('approval-change-save')).toHaveCount(0)
  expect(writes).toHaveLength(2)
  expect(writes[1].body).toEqual({ version: 2, mode: 'independent', reason: '保留交接原因' })
})
test('unknown switch outcome is resolved by observation without repeating a completed change', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: modeAdminPermissions, existing: true, changeUnknown: true })
  const dialog = await startModeChange(page)
  await setChangedMode(page, '直接批准')
  await page.getByTestId('approval-change-reason').fill('确认未知结果')
  await confirmModeChange(page)
  await expect(page.getByTestId('approval-change-save')).toBeDisabled()
  await page.getByTestId('approval-change-reload').click()
  await expect(dialog).toContainText('当前方式：直接批准')
  await expect(page.getByTestId('approval-change-save')).toBeDisabled()
  expect(writes).toHaveLength(1)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByTestId('approval-targets')).toContainText('直接批准')
})
test('ordinary handlers cannot change approval modes', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page)
  await expect(page.getByTestId('approval-change')).toHaveCount(0)
  expect(writes).toEqual([])
})
test('switching to direct approval requires the current administrator to be a qualified successor', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: modeAdminPermissions.filter(key => key !== 'system.engine_access_grant.create'), existing: true })
  const dialog = await startModeChange(page)
  await expect(dialog).toContainText('还需要当前账号具备授予读取权限')
  await page.getByTestId('approval-change-mode').click()
  await expect(page.getByRole('option', { name: '直接批准', exact: true })).toHaveClass(/is-disabled/)
  expect(writes).toEqual([])
})
test('English approval mode dialog fits a narrow viewport without issuing access', async ({ page }, testInfo) => {
  const { writes } = await fixture(page, { allowed: modeAdminPermissions, existing: true, language: 'en' })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByTestId('approval-change').click()
  const dialog = page.getByRole('dialog', { name: 'Change approval mode', exact: true })
  await expect(dialog).toBeVisible()
  await page.getByTestId('approval-change-reason').fill('Explicit approval handoff')
  await expect(page.getByTestId('approval-change-reason')).toBeFocused()
  const bounds = await dialog.boundingBox()
  expect(bounds.x).toBeGreaterThanOrEqual(0)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
  await page.screenshot({ path: testInfo.outputPath('approval-mode-mobile-en.png'), fullPage: true, animations: 'disabled' })
  expect(writes).toEqual([])
})
function historicalGrant(kind = 'user') {
  return { request_id: `history-${kind}`, engine_id: '2', catalog_path: tablePath, recipient_type: kind, recipient_id: '33',
    approval_mode: 'independent', expiry_mode: 'until_revoked', granted_at: '2026-10-07T00:00:00Z', revocation: null }
}
test('grant history resolves inactive account names without opening a grant form', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant()], memberStatus: 'suspended' })
  await page.goto('/engines/2?tab=data-authorization')
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('Outdoor reader')
  await expect(history).toContainText('outdoor')
  await expect(history).not.toContainText('账号 · 33')
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  const requests = reads.filter(path => path.includes('/tenant/memberships'))
  expect(requests).toHaveLength(1)
  expect(new URL(requests[0]).searchParams.has('status')).toBe(false)
  await history.getByTestId('grant-recipient').hover()
  await expect(page.getByRole('tooltip')).toContainText('账号编号：33')
  expect(writes).toEqual([])
})
test('grant history keeps account department and project group identities distinct', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read', 'iam.project_group.read'],
    history: ['user', 'user', 'department', 'project_group'].map(historicalGrant) })
  await page.goto('/engines/2?tab=data-authorization')
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('Outdoor reader')
  await expect(history).toContainText('Outdoor department')
  await expect(history).toContainText('outdoor_dept')
  await expect(history).toContainText('Outdoor team')
  await expect(history).toContainText('outdoor_stat')
  for (const kind of ['memberships', 'departments', 'project_groups']) expect(reads.filter(path => path.includes(`/tenant/${kind}`))).toHaveLength(1)
  expect(writes).toEqual([])
})
test('grant history remains visible without permission to query recipient names', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: grantPermissions.filter(permission => permission !== 'iam.tenant_membership.read'), history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('无接收方名称查看权限')
  await expect(history).toContainText('账号编号：33')
  await expect(history).toContainText('已签发')
  expect(reads.some(path => path.includes('/tenant/memberships'))).toBe(false)
  expect(writes).toEqual([])
})
test('failed recipient name lookup does not erase grant history or imply deletion', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant()], identityFailure: true })
  await page.goto('/engines/2?tab=data-authorization')
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('接收方名称查询失败，请刷新重试')
  await expect(history).toContainText('账号编号：33')
  await expect(history).toContainText('已签发')
  expect(writes).toEqual([])
})
async function selectData(page, schema = false) {
  const panel = page.getByTestId('engine-data-authorization')
  await panel.locator('.picker-node-label').filter({ hasText: /^outdoor$/ }).click()
  if (!schema) {
    const namespace = panel.getByRole('treeitem', { name: 'outdoor', exact: true })
    if (await namespace.getAttribute('aria-expanded') !== 'true') await namespace.locator(':scope > .el-tree-node__content .el-tree-node__expand-icon').click()
    await panel.locator('.picker-node-label').filter({ hasText: /^activities$/ }).click()
  }
  await page.getByTestId('approval-add-selection').click()
  await expect(page.getByTestId('approval-targets')).toBeVisible()
  return panel
}
async function selectInspectionTable(page, name = 'activities') {
  const panel = page.getByTestId('engine-data-authorization')
  const namespace = panel.getByRole('treeitem', { name: 'outdoor', exact: true })
  if (await namespace.getAttribute('aria-expanded') !== 'true') await namespace.locator(':scope > .el-tree-node__content .el-tree-node__expand-icon').click()
  await panel.locator('.picker-node-label').filter({ hasText: new RegExp(`^${name}$`) }).click()
}
async function chooseMode(page, mode = '直接批准') {
  await page.getByTestId('approval-mode').click()
  await page.getByRole('option', { name: mode, exact: true }).click()
}
async function grantDraft(page) {
  const form = page.getByTestId('source-grant-form')
  await expect(form).toBeVisible()
  await form.locator('.iam-member-select__member').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await page.getByTestId('source-grant-expiry').click()
  await page.getByRole('option', { name: '长期有效，直到撤销', exact: true }).click()
  await page.getByTestId('source-grant-reason').fill('Explicit read permission')
}
async function confirmGrant(page, cancel = false) {
  await page.getByTestId('source-grant-confirm').click()
  const confirm = page.getByRole('dialog').filter({ hasText: '即将授予 Outdoor reader' })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: cancel ? '取消' : '确定', exact: true }).click()
  await expect(confirm).toBeHidden()
}
async function refreshSameAuthorization(page) {
  await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().fetchAuthContext())
}
test('first direct approval and read grant use one explicit command without Catalog or a separate configuration write', async ({ page }, testInfo) => {
  const { writes, reads } = await fixture(page, { allowed: grantPermissions })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await chooseMode(page); await grantDraft(page)
  expect(writes).toEqual([])
  await page.screenshot({ path: testInfo.outputPath('first-direct-authorization.png') })
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('只读授权已签发')
  expect(writes).toHaveLength(1)
  expect(writes[0].path).toMatch(/access_grants$/)
  expect(writes[0].body).toMatchObject({ catalog_path: tablePath, initialize_approval: true, requirement_version: '1',
    recipient_id: '33', recipient_type: 'user', action: 'read', expiry_mode: 'until_revoked', expires_at: null })
  expect(reads.every(path => new URL(path, 'http://127.0.0.1:4173').pathname.startsWith('/api/v1/system/'))).toBe(true)
  await selectData(page)
  await expect(page.getByTestId('approval-mode')).toHaveCount(0)
  await grantDraft(page); await confirmGrant(page)
  expect(writes).toHaveLength(2)
  expect(writes[1].body.initialize_approval).toBe(false)
})
test('schema expands current ordinary tables, permits removal, and never grants the parent or view', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, secondTable: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page, true)
  const targets = page.getByTestId('approval-targets')
  await expect(targets).toContainText('activities'); await expect(targets).toContainText('persons')
  await expect(targets).not.toContainText('activities_view')
  await targets.getByRole('row').filter({ hasText: 'persons' }).getByRole('button', { name: '移除' }).click()
  await chooseMode(page); await grantDraft(page); await confirmGrant(page)
  expect(writes).toHaveLength(1); expect(writes[0].body.catalog_path).toEqual(tablePath)
})
test('partial batch keeps immutable commands and retries only failed tables', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, secondTable: true, grantFailure: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page, true); await chooseMode(page); await grantDraft(page); await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('签发结果未知')
  expect(writes).toHaveLength(2)
  await expect(page.getByTestId('approval-add-selection')).toBeDisabled()
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('已找回原命令的签发历史')
  expect(writes).toHaveLength(3); expect(writes[2].body).toEqual(writes[0].body)
  expect(writes[1].body.request_id).not.toBe(writes[0].body.request_id)
})
test('existing direct approval grants and revokes an explicit recipient without initialization', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page); await confirmGrant(page)
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('Outdoor reader')
  await expect(history).toContainText('outdoor')
  await expect(history).not.toContainText('账号 · 33')
  expect(writes).toHaveLength(1); expect(writes[0].body.initialize_approval).toBe(false)
  await page.getByTestId('source-grant-revoke').click()
  await expect(page.getByRole('dialog', { name: '撤销授权', exact: true })).toContainText('Outdoor reader')
  await page.getByTestId('source-grant-revoke-reason').fill('Read access no longer needed')
  await page.getByTestId('source-grant-revoke-confirm').click()
  await expect(page.getByRole('dialog', { name: '撤销授权', exact: true })).toBeHidden()
  await history.getByText('授权历史', { exact: true }).click()
  await expect(history).toContainText('已撤销')
  expect(writes).toHaveLength(2)
})
test('unchanged engine and authorization refresh preserve the draft; losing permission clears it', async ({ page }) => {
  await page.clock.install()
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  const response = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/engines'))
  await page.clock.runFor(10000); await response
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  await refreshSameAuthorization(page)
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    const context = JSON.parse(JSON.stringify(store.authContext))
    context.authorization.role_assignments[0].permissions = context.authorization.role_assignments[0].permissions.filter(permission => permission !== 'system.engine_access_grant.create')
    store.authContext = context
  })
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  await expect(page.getByTestId('approval-targets')).toHaveCount(0)
  expect(writes).toEqual([])
})
test('authorization version change clears the selected snapshot', async ({ page }) => {
  await fixture(page, { allowed: grantPermissions })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await chooseMode(page)
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    store.authContext = { ...store.authContext, authorization: { ...store.authContext.authorization, authorization_version: '2' } }
  })
  await expect(page.getByTestId('approval-targets')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
})
test('read-only configuration neither browses sources nor exposes grant controls', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read', 'system.engine_access_approval_requirement.read'], existing: true })
  await page.goto('/engines/2?tab=data-authorization')
  await page.getByText('已配置的批准方式', { exact: true }).click()
  await expect(page.getByTestId('engine-data-authorization')).toContainText('业务批准')
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  expect(reads.some(path => path.includes('/catalog/'))).toBe(false); expect(writes).toEqual([])
})
test('missing engine catalog permission explains why source selection is unavailable', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: permissions.filter(permission => permission !== 'system.engine_catalog.read') })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('engine-data-authorization')).toContainText('选择数据表需要引擎目录浏览权限')
  expect(reads.some(path => path.includes('/catalog/'))).toBe(false); expect(writes).toEqual([])
})
test('existing business approval is not overwritten and Catalog absence never downgrades it', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'catalog' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page)
  await expect(page.getByTestId('approval-targets')).toContainText('业务批准')
  await expect(page.getByTestId('approval-mode')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  await expect(page.getByRole('button').filter({ hasText: '前往业务批准' })).toBeDisabled()
  expect(writes).toEqual([])
})
test('available Catalog offers business approval with a navigation action but no direct grant', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'catalog.entry.read'] })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await chooseMode(page, '业务批准')
  await page.getByTestId('approval-reason').fill('Business owner confirmation required')
  await page.getByTestId('approval-initialize').click()
  const confirm = page.getByRole('dialog').filter({ hasText: '业务批准需在 Catalog' }).filter({ has: page.getByRole('button', { name: '确定', exact: true }) })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByRole('button').filter({ hasText: '前往业务批准' })).toBeEnabled()
  expect(writes).toHaveLength(1); expect(writes[0].path).toMatch(/access_approval_requirements$/)
  expect(writes[0].body.mode).toBe('catalog')
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
})
test('conflicting first approval retains the original command and does not fall back', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, conflict: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await chooseMode(page); await grantDraft(page); await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('不能覆盖')
  expect(writes).toHaveLength(1); expect(writes[0].body.initialize_approval).toBe(true)
})
test('cancelled confirmation does not write; unknown result requires an explicit same-command retry', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent', grantFailure: true })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  await confirmGrant(page, true); expect(writes).toEqual([])
  await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('签发结果未知')
  expect(writes).toHaveLength(1)
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('已找回原命令的签发历史')
  expect(writes).toHaveLength(2); expect(writes[1].body).toEqual(writes[0].body)
})
test('management scope denial is visible and never initializes as a fallback', async ({ page }) => {
  const { writes } = await fixture(page, { denied: true, allowed: ['system.engine.read', 'system.engine_access_approval_requirement.read'] })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('engine-data-authorization')).toContainText('本引擎管理委派不足')
  expect(writes).toEqual([])
})
test('without authorization permission the route restores to basic details', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read'] })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page).toHaveURL('http://127.0.0.1:4173/engines/2')
  await expect(page.getByTestId('engine-data-authorization')).toHaveCount(0)
  await expect(page.getByTestId('engine-authorization-open')).toHaveCount(0)
  expect(reads).toEqual([]); expect(writes).toEqual([])
})
test('definite duplicate rejection does not keep an automatic original-command retry', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent', grantDuplicate: true, history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page); await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('未重复发放')
  await expect(page.getByTestId('source-grant-confirm')).toHaveText('授予读取权限')
  expect(writes).toHaveLength(1)
})

test('current relations collapse duplicates and withdraw every personal grant, not organization grants', async ({ page }) => {
  const personal = [1, 2].map(index => ({ ...historicalGrant(), request_id: `personal-${index}` }))
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read'], history: [...personal, historicalGrant('department')] })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants')
  await expect(panel.getByRole('row').filter({ hasText: 'Outdoor reader' })).toHaveCount(1)
  await expect(panel.getByRole('row').filter({ hasText: 'Outdoor reader' })).toContainText('2')
  await panel.getByRole('row').filter({ hasText: 'Outdoor reader' }).getByTestId('source-grant-revoke').click()
  const dialog = page.getByRole('dialog', { name: '撤销授权', exact: true })
  await expect(dialog).toContainText('全部有效授权')
  await expect(dialog).toContainText('部门或项目组')
  await dialog.getByTestId('source-grant-revoke-reason').fill('Withdraw the complete personal relation')
  await dialog.getByTestId('source-grant-revoke-confirm').click()
  await expect(dialog).toBeHidden()
  await expect(panel.getByRole('row').filter({ hasText: 'Outdoor reader' })).toHaveCount(0)
  await expect(panel.getByRole('row').filter({ hasText: 'Outdoor department' })).toHaveCount(1)
  await panel.getByText('授权历史', { exact: true }).click()
  await expect(panel.getByRole('row').filter({ hasText: '已撤销' })).toHaveCount(2)
  await expect(panel.getByTestId('source-grant-revoke')).toHaveCount(0)
  expect(writes).toHaveLength(1)
})

test('authorization history page two stays selected while loading', async ({ page }) => {
  const history = Array.from({ length: 25 }, (_, index) => ({ ...historicalGrant(), request_id: `page-${index}` }))
  const { reads } = await fixture(page, { allowed: grantPermissions, history })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants')
  await panel.getByText('授权历史', { exact: true }).click()
  await expect(panel).toContainText('page-19')
  await panel.locator('.el-pager').getByText('2', { exact: true }).click()
  await expect(panel).toContainText('page-24')
  await expect(panel).not.toContainText('page-0')
  const historyReads = reads.filter(url => url.includes('/access_grants/history'))
  expect(new URL(historyReads.at(-1)).searchParams.get('page')).toBe('2')
})

test('table and account filters cover all pages, persist across list modes and reset together', async ({ page }) => {
  const history = Array.from({ length: 25 }, (_, index) => ({ ...historicalGrant(), request_id: `filter-${index}`,
    catalog_path: { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: `filter_${index}` }] } }))
  history.push({ ...history[0], request_id: 'other-account', recipient_id: '34' },
    { ...history[0], request_id: 'organization-source', recipient_type: 'department' },
    { ...history[0], request_id: 'revoked-filter', revocation: { revoked_at: '2026-10-08T00:00:00Z' } })
  const { reads, writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read'], history, memberStatus: 'suspended' })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants'), list = panel.getByTestId('source-grant-list')
  const search = panel.getByTestId('source-grant-table-filter')
  const query = panel.getByTestId('source-grant-filter-query')
  await expect(panel.locator('.el-pagination')).toContainText('27')
  await search.fill(' filter_24 ')
  await query.click()
  await expect(list.getByRole('row').filter({ hasText: 'filter_24' })).toHaveCount(1)
  await expect(panel.locator('.el-pagination')).toContainText('1')
  await search.fill('filter_')
  await panel.getByTestId('source-grant-account-filter').locator('.el-select').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await search.fill('')
  await query.click()
  await expect(panel.locator('.el-pagination')).toContainText('25')
  const accountOnly = new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams
  expect(accountOnly.get('account_id')).toBe('33')
  expect(accountOnly.has('table_search')).toBe(false)
  await search.fill('filter_')
  await query.click()
  await expect(panel.locator('.el-pagination')).toContainText('25')
  await expect(list).not.toContainText('Outdoor department')
  await panel.locator('.el-pager').getByText('2', { exact: true }).click()
  await expect(list).toContainText('filter_24')
  // Unsubmitted text is not applied by refresh or paging.
  await search.fill('unsubmitted')
  await panel.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(list).toContainText('filter_24')
  let last = new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams
  expect(last.get('table_search')).toBe('filter_')
  expect(last.get('account_id')).toBe('33')
  expect(last.get('page')).toBe('2')
  await panel.getByText('授权历史', { exact: true }).click()
  await expect(panel.locator('.el-pagination')).toContainText('26')
  last = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
  expect(last.get('table_search')).toBe('filter_')
  expect(last.get('account_id')).toBe('33')
  expect(last.get('page')).toBe('1')
  await panel.getByTestId('source-grant-filter-reset').click()
  await expect(search).toHaveValue('')
  await expect(panel.locator('.el-pagination')).toContainText('28')
  last = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
  expect(last.has('table_search')).toBe(false)
  expect(last.has('account_id')).toBe(false)
  expect(last.get('page')).toBe('1')
  await search.fill('missing-table')
  await query.click()
  await expect(panel.locator('.el-pagination')).toContainText('0')
  await expect(list.getByRole('row')).toHaveCount(1)
  await panel.getByTestId('source-grant-filter-reset').click()
  await expect(panel.locator('.el-pagination')).toContainText('28')
  expect(writes).toEqual([])
})

test('unauthorized recipient type never loads candidates or issues grants', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page)
  await page.getByTestId('source-grant-recipient-type').click()
  await page.getByRole('option', { name: '部门', exact: true }).click()
  await expect(page.getByTestId('source-grant-form')).toContainText('读取此类接收方候选需要')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
  expect(writes).toEqual([])
})
