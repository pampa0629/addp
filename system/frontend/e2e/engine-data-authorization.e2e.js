import { expect, test } from '@playwright/test'

const permissions = ['system.engine.read', 'system.engine_access_approval_requirement.read',
  'system.engine_access_approval_requirement.initialize', 'system.engine_catalog.read']
const rootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }] }
const schemaPath = { ...rootPath, segments: [...rootPath.segments, { term: 'schema', kind: 'namespace', name: 'outdoor' }] }
const tablePath = { ...rootPath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'activities' }] }
async function fixture(page, { allowed = permissions, conflict = false, denied = false, existing = false, failWrite = false, grantFailure = false, secondTable = false } = {}) {
  const writes = [], reads = []
  let rows = existing ? [{ id: 'cc0a8000-6000-4000-8000-800000000001', engine_id: '2', mode: typeof existing === 'string' ? existing : 'catalog', version: 1, catalog_path: tablePath }] : []
  let grants = [], failedGrant = false
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'configuration-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: allowed }] } })
    if (path.endsWith('/tenant/memberships')) return reply({ data: [{ id: '34', principal_id: '33', principal_type: 'user', display_name: 'Outdoor reader', username: 'outdoor', status: 'active', principal_status: 'active' }], total_pages: 1 })
    if (path.endsWith('/access_delegations')) {
      reads.push(path)
      return reply({ data: [], total: 0, page: 1, page_size: 10, total_pages: 1 })
    }
    if (path.endsWith('/access_grants')) {
      reads.push(path)
      if (request.method() === 'GET') return reply({ data: grants, total: grants.length, page: 1, page_size: 20, total_pages: 1 })
      const body = request.postDataJSON(); writes.push({ path, body })
      if (conflict) return reply({ error: '本表已有批准安排，不能覆盖' }, 409)
      if (grantFailure && !failedGrant) { failedGrant = true; return reply({ error: '签发结果未知，请按原命令核对' }, 503) }
      const grant = { ...body, approval_mode: 'independent', engine_id: '2', granted_at: '2026-10-07T00:00:00Z', revocation: null }
      grants.push(grant)
      if (body.initialize_approval) rows.push({ engine_id: '2', mode: 'independent', version: 1, catalog_path: body.catalog_path })
      return reply(grant, 201)
    }
    if (path.includes('/access_grants/') && path.endsWith('/revoke')) {
      const body = request.postDataJSON(); writes.push({ path, body })
      const revocation = { request_id: grants[0].request_id, revoked_at: '2026-10-07T01:00:00Z', reason: body.reason }
      grants[0] = { ...grants[0], revocation }; return reply(revocation)
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
  return { writes, reads }
}

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
  expect(reads.every(path => path.startsWith('/api/v1/system/'))).toBe(true)
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
  await expect(history).toContainText('账号 · 33')
  expect(writes).toHaveLength(1); expect(writes[0].body.initialize_approval).toBe(false)
  await page.getByTestId('source-grant-revoke').click()
  await page.getByTestId('source-grant-revoke-reason').fill('Read access no longer needed')
  await page.getByTestId('source-grant-revoke-confirm').click()
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
test('unauthorized recipient type never loads candidates or issues grants', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page)
  await page.getByTestId('source-grant-recipient-type').click()
  await page.getByRole('option', { name: '部门', exact: true }).click()
  await expect(page.getByTestId('source-grant-form')).toContainText('读取此类接收方候选需要')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
  expect(writes).toEqual([])
})
