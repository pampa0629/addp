import { expect, test } from '@playwright/test'

const permissions = ['system.engine.read', 'system.engine_access_approval_requirement.read',
  'system.engine_access_approval_requirement.initialize', 'system.engine_catalog.read']
const rootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }] }
const schemaPath = { ...rootPath, segments: [...rootPath.segments, { term: 'schema', kind: 'namespace', name: 'outdoor' }] }
const tablePath = { ...rootPath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'activities' }] }
async function fixture(page, { allowed = permissions, conflict = false, denied = false, existing = false, failWrite = false, grantFailure = false } = {}) {
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
    if (path.endsWith('/access_grants')) {
      reads.push(path)
      if (request.method() === 'GET') return reply({ data: grants, total: grants.length, page: 1, page_size: 20, total_pages: 1 })
      const body = request.postDataJSON(); writes.push({ path, body })
      if (grantFailure && !failedGrant) { failedGrant = true; return reply({ error: '签发结果未知，请按原命令核对' }, 503) }
      grants = [{ ...body, approval_mode: 'independent', engine_id: '2', granted_at: '2026-10-07T00:00:00Z', revocation: null }]
      return reply(grants[0], 201)
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
    if (path.endsWith('/catalog/children')) {
      reads.push(path)
      const segments = request.postDataJSON().path.segments
      if (!segments.length) return reply({ nodes: [{ name: '', path: rootPath, term: 'server', kind: 'server', role: 'branch' }] })
      if (segments.length === 1) return reply({ nodes: [{ name: 'outdoor', path: schemaPath, term: 'schema', kind: 'namespace', role: 'branch' }] })
      if (segments.length === 2) return reply({ nodes: [
        { name: 'activities', path: tablePath, term: 'table', kind: 'table', role: 'leaf' },
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
async function selectTable(page) {
  const panel = page.getByTestId('engine-data-authorization')
  await panel.locator('.picker-node-label').filter({ hasText: /^outdoor$/ }).click()
  await panel.getByRole('treeitem').filter({ hasText: /^outdoor$/ }).locator('.el-tree-node__expand-icon').click()
  await panel.locator('.picker-node-label').filter({ hasText: /^activities$/ }).click()
  return panel
}
async function draft(page, mode = 'System 独立批准') {
  await selectTable(page)
  await page.getByTestId('approval-mode').click()
  await page.getByRole('option', { name: mode, exact: true }).click()
  await expect(page.getByRole('option', { name: mode, exact: true })).toBeHidden()
  await page.getByTestId('approval-reason').fill('Explicit table approval arrangement')
}
async function confirm(page) {
  await page.getByTestId('approval-initialize').click()
  const dialog = page.getByRole('dialog').filter({ hasText: '本次只配置批准方式' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
}

async function refreshEngineProjection(page) {
  const response = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/engines'))
  await page.clock.runFor(10000)
  await response
}

async function refreshSameAuthorization(page) {
  await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().fetchAuthContext())
}

test('unchanged engine and authorization refreshes preserve the approval draft; a new authorization version clears it', async ({ page }) => {
  await page.clock.install()
  const { writes } = await fixture(page)
  await page.goto('/engines/2?tab=data-authorization'); await draft(page)
  const panel = page.getByTestId('engine-data-authorization')
  await refreshEngineProjection(page)
  await expect(page.getByTestId('approval-reason')).toHaveValue('Explicit table approval arrangement')
  await expect(page.getByTestId('approval-mode')).toContainText('System 独立批准')
  await expect(panel.getByRole('treeitem', { name: 'outdoor', exact: true })).toHaveAttribute('aria-expanded', 'true')
  await refreshSameAuthorization(page)
  await expect(page.getByTestId('approval-reason')).toHaveValue('Explicit table approval arrangement')
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    store.authContext = { ...store.authContext, authorization: { ...store.authContext.authorization, authorization_version: '2' } }
  })
  await expect(page.getByTestId('approval-reason')).toHaveValue('')
  await expect(panel.getByRole('treeitem', { name: 'outdoor', exact: true })).toHaveAttribute('aria-expanded', 'false')
  expect(writes).toEqual([])
})

test('selects an unscanned table, explicitly configures independent approval and never writes a grant', async ({ page }, testInfo) => {
  const { writes, reads } = await fixture(page)
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-data-authorization')
  await expect(panel).toBeVisible()
  await page.getByTestId('approval-initialize').click()
  await expect(panel).toContainText('请选择普通数据表')
  expect(writes).toHaveLength(0)
  await draft(page)
  await page.screenshot({ path: testInfo.outputPath('independent-approval-configuration.png') })
  expect(writes).toHaveLength(0)
  await confirm(page)
  await expect(panel).toContainText('批准方式已保存，尚未授予任何数据读取权限')
  expect(writes).toEqual([{ path: '/api/v1/system/engines/2/access_approval_requirements', body: {
    catalog_path: tablePath, mode: 'independent', reason: 'Explicit table approval arrangement'
  } }])
  expect(reads.every(path => path.startsWith('/api/v1/system/'))).toBe(true)
  await page.reload(); await expect(panel).toContainText('System 独立批准')
  expect(writes).toHaveLength(1)
})

test('read-only configuration does not browse data or expose write controls', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read', 'system.engine_access_approval_requirement.read'], existing: true })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('engine-data-authorization')).toContainText('Catalog 业务确认')
  await expect(page.getByTestId('approval-initialize')).toHaveCount(0)
  expect(reads.some(path => path.includes('/catalog/'))).toBe(false)
  expect(writes).toEqual([])
})

test('configuration permission without engine catalog permission explains the missing qualification without browsing', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: permissions.filter(permission => permission !== 'system.engine_catalog.read') })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('engine-data-authorization')).toContainText('选择数据表需要引擎目录浏览权限')
  await expect(page.getByTestId('approval-initialize')).toHaveCount(0)
  expect(reads.some(path => path.includes('/catalog/'))).toBe(false)
  expect(writes).toEqual([])
})

test('a view cannot be selected and existing configuration cannot be overwritten', async ({ page }) => {
  const { writes } = await fixture(page, { existing: true })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = await selectTable(page)
  await expect(panel).toContainText('本表已配置为“Catalog 业务确认”')
  await expect(page.getByTestId('approval-initialize')).toBeDisabled()
  await panel.locator('.picker-node-label').filter({ hasText: /^activities_view$/ }).click()
  await expect(page.getByTestId('approval-initialize')).toBeDisabled()
  expect(writes).toEqual([])
})

test('conflicting configuration retains the draft without automatic retry', async ({ page }) => {
  const { writes } = await fixture(page, { conflict: true })
  await page.goto('/engines/2?tab=data-authorization'); await draft(page, 'Catalog 业务确认'); await confirm(page)
  await expect(page.getByTestId('engine-data-authorization')).toContainText('本表已有批准安排，不能覆盖')
  await expect(page.getByTestId('approval-reason')).toHaveValue('Explicit table approval arrangement')
  expect(writes).toHaveLength(1)
})

test('management scope denial is visible and never initializes as a fallback', async ({ page }) => {
  const { writes } = await fixture(page, { denied: true, allowed: ['system.engine.read', 'system.engine_access_approval_requirement.read'] })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('engine-data-authorization')).toContainText('本引擎管理委派不足')
  expect(writes).toEqual([])
})

test('unknown write result is not presented as saved and cancelling confirmation never writes', async ({ page }) => {
  const { writes } = await fixture(page, { failWrite: true })
  await page.goto('/engines/2?tab=data-authorization'); await draft(page)
  await page.getByTestId('approval-initialize').click()
  await page.getByRole('dialog').filter({ hasText: '本次只配置批准方式' }).getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  await confirm(page)
  const panel = page.getByTestId('engine-data-authorization')
  await expect(panel).toContainText('结果尚未确认，请核对')
  await expect(panel).not.toContainText('批准方式已保存')
  await expect(page.getByTestId('approval-reason')).toHaveValue('Explicit table approval arrangement')
  expect(writes).toHaveLength(1)
})

test('without configuration permission the data-authorization tab restores to basic details', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read'] })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page).toHaveURL('http://127.0.0.1:4173/engines/2')
  await expect(page.getByTestId('engine-data-authorization')).toHaveCount(0)
  expect(reads).toEqual([]); expect(writes).toEqual([])
})

const grantPermissions = [...permissions, 'system.engine_access_grant.create', 'system.engine_access_grant.read', 'system.engine_access_grant.revoke', 'iam.tenant_membership.read']
async function grantDraft(page) {
  await page.getByTestId('source-grant-open').click()
  const dialog = page.getByRole('dialog', { name: '授予读取权限', exact: true })
  await dialog.locator('.iam-member-select__member').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await page.getByTestId('source-grant-expiry').click()
  await page.getByRole('option', { name: '长期有效，直到撤销', exact: true }).click()
  await page.getByTestId('source-grant-reason').fill('Explicit read permission')
}
async function confirmGrant(page) {
  await page.getByTestId('source-grant-confirm').click()
  const confirm = page.getByRole('dialog').filter({ hasText: '即将授予 Outdoor reader' })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '确定', exact: true }).click()
  await expect(confirm).toBeHidden()
}
test('unchanged engine and authorization refreshes preserve the read grant draft; losing create permission closes it', async ({ page }) => {
  await page.clock.install()
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization'); await grantDraft(page)
  const dialog = page.getByRole('dialog', { name: '授予读取权限', exact: true })
  await refreshEngineProjection(page)
  await expect(dialog).toBeVisible()
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  await refreshSameAuthorization(page)
  await expect(dialog).toBeVisible()
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    const context = JSON.parse(JSON.stringify(store.authContext))
    context.authorization.role_assignments[0].permissions = context.authorization.role_assignments[0].permissions.filter(permission => permission !== 'system.engine_access_grant.create')
    store.authContext = context
  })
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('source-grant-open')).toHaveCount(0)
  expect(writes).toEqual([])
})
test('independent read issuance and revocation use an explicit recipient without Catalog', async ({ page }, testInfo) => {
  const { writes, reads } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('source-grant-open')).toBeVisible()
  expect(writes).toEqual([])
  await grantDraft(page)
  await page.screenshot({ path: testInfo.outputPath('independent-read-grant.png') })
  expect(writes).toEqual([])
  await confirmGrant(page)
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('只读授权已签发')
  await expect(history).toContainText('账号 · 33')
  expect(writes[0].body).toMatchObject({ catalog_path: tablePath, requirement_version: '1', recipient_id: '33', recipient_type: 'user', action: 'read', expiry_mode: 'until_revoked', expires_at: null })
  expect(writes).toHaveLength(1)
  await page.getByTestId('source-grant-revoke').click()
  await page.getByTestId('source-grant-revoke-reason').fill('Read access no longer needed')
  await page.getByTestId('source-grant-revoke-confirm').click()
  await expect(history).toContainText('已撤销')
  await expect(page.getByTestId('source-grant-revoke')).toHaveCount(0)
  expect(writes).toHaveLength(2)
  expect(reads.every(path => path.startsWith('/api/v1/system/'))).toBe(true)
})
test('unknown issuance retains one immutable command and only retries after confirmation', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent', grantFailure: true })
  await page.goto('/engines/2?tab=data-authorization'); await grantDraft(page); await confirmGrant(page)
  await expect(page.getByRole('dialog', { name: '授予读取权限', exact: true })).toContainText('签发结果未知')
  expect(writes).toHaveLength(1)
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('已找回原命令的签发历史')
  expect(writes).toHaveLength(2); expect(writes[1].body).toEqual(writes[0].body)
})
test('Catalog approval never exposes independent issuance', async ({ page }) => {
  await fixture(page, { allowed: grantPermissions, existing: 'catalog' })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('engine-data-authorization')).toContainText('Catalog 业务确认')
  await expect(page.getByTestId('source-grant-open')).toHaveCount(0)
})
test('changing to an unauthorized recipient type keeps the form open and never calls its API', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, existing: 'independent' })
  await page.goto('/engines/2?tab=data-authorization'); await page.getByTestId('source-grant-open').click()
  await page.getByTestId('source-grant-recipient-type').click()
  await page.getByRole('option', { name: '部门', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '授予读取权限', exact: true })).toContainText('读取此类接收方候选需要')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
  expect(writes).toEqual([])
})
