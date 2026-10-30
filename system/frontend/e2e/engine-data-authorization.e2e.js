import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { createHash } from 'node:crypto'
import { expect, test } from '@playwright/test'

const permissions = ['system.engine.read', 'system.engine_access_direct_permission.read', 'system.engine_catalog.read', 'system.engine_access_grant.create', 'system.engine_access_grant.read']
const rootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }] }
const schemaPath = { ...rootPath, segments: [...rootPath.segments, { term: 'schema', kind: 'namespace', name: 'outdoor' }] }
const tablePath = { ...rootPath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'activities' }] }
const ftpRootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'root', kind: 'root', name: '' }] }
const ftpDirectoryPath = { ...ftpRootPath, segments: [...ftpRootPath.segments, { term: 'directory', kind: 'directory', name: 'Models' }] }
const ftpFilePath = name => ({ ...ftpRootPath, segments: [...ftpDirectoryPath.segments, { term: 'file', kind: 'file', name }] })
const gdbPath = name => ({ ...ftpRootPath, segments: [...ftpDirectoryPath.segments, { term: 'directory', kind: 'directory', name }] })
const minioRootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'service', kind: 'service', name: '' }] }
const minioBucketPath = { ...minioRootPath, segments: [...minioRootPath.segments, { term: 'bucket', kind: 'bucket', name: 'Models' }] }
const minioObjectPath = name => ({ ...minioRootPath, segments: [...minioBucketPath.segments, { term: 'object', kind: 'object', name }] })
function filterGrants(items, query) {
  const search = query.get('table_search')?.toLowerCase(), kind = query.get('recipient_type'), recipient = query.get('recipient_id')
  const exact = query.get('catalog_path') && JSON.parse(query.get('catalog_path')), state = query.get('state')
  return items.filter(item => (!exact || JSON.stringify(item.catalog_path) === JSON.stringify(exact)) && (!search || item.catalog_path.segments.map(segment => segment.name).join(' / ').toLowerCase().includes(search)) &&
    (!kind || item.recipient_type === kind) && (!recipient || String(item.recipient_id) === recipient) &&
    (!state || state === (item.revocation ? 'revoked' : item.expires_at && new Date(item.expires_at) <= new Date() ? 'expired' : 'active')))
}

test('all records state filter applies before pagination and survives current view round trips', async ({ page }) => {
  const active = Array.from({ length: 23 }, (_, i) => ({ ...historicalGrant(), request_id: `active-${i}` }))
  const expired = { ...historicalGrant(), request_id: 'expired', expires_at: '2020-01-01T00:00:00Z' }
  const revoked = { ...expired, request_id: 'revoked-and-expired', revocation: { revoked_at: '2020-01-01T00:00:00Z' } }
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, history: [...active, expired, revoked] })
  await page.goto('/engines/2?tab=data-authorization')
  const tabs = page.getByTestId('authorization-workspace-tabs'), list = page.getByTestId('source-grant-list')
  await tabs.getByText('全部授权记录', { exact: true }).click()
  await selectAccountFilter(page)
  await page.getByTestId('source-grant-table-filter').fill('activities')
  const status = page.getByTestId('source-grant-state-filter')
  await expect(status).toContainText('全部状态')
  for (const [label, state, count] of [['有效', 'active', 23], ['已撤销', 'revoked', 1], ['已到期', 'expired', 1]]) {
    await status.click(); await page.getByRole('option', { name: label, exact: true }).click()
    await expect(page.getByTestId('engine-source-grants').locator('.el-pagination')).toContainText(`共 ${count} 条`)
    const query = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
    expect(Object.fromEntries(query)).toMatchObject({ state, recipient_type: 'user', recipient_id: '33', table_search: 'activities', page: '1' })
    if (state === 'active') {
      await list.locator('..').locator('.el-pager').getByText('2', { exact: true }).click()
      await expect(list).toContainText('active-20')
    } else await expect(list).not.toContainText('active-')
  }
  await expect(list).not.toContainText('revoked-and-expired')
  await tabs.getByText('当前授权', { exact: true }).click()
  await expect(status).toHaveCount(0)
  await expect(list.getByTestId('account-grant-expand')).toHaveCount(1)
  expect(new URL(reads.filter(url => /\/access_grants\?/.test(url)).at(-1)).searchParams.has('state')).toBe(false)
  await tabs.getByText('全部授权记录', { exact: true }).click()
  await expect(status).toContainText('已到期')
  await expect(list).toContainText('expired')
  await status.click(); await page.getByRole('option', { name: '全部状态', exact: true }).click()
  await expect(page.getByTestId('engine-source-grants').locator('.el-pagination')).toContainText('共 25 条')
  expect(writes).toEqual([])
})
async function fixture(page, { allowed = permissions, conflict = false, denied = false, permit = 'deployment_default', permitRows = null, grantFailure = false, grantDuplicate = false, secondTable = false, history = [], memberStatus = 'active', identityFailure = false, changeConflict = false, changeUnknown = false, language = 'zh-cn', inspectionReason = 'grant', inspectionDenied = false, organizationMember = false, factsState = 'complete', denies = [], denyFailure = false, releaseFailure = false, ftp = '', sourceType = 'ftp', factsTarget = null } = {}) {
  const modelRoot = sourceType === 'minio' ? minioRootPath : ftpRootPath
  const modelBranch = sourceType === 'minio' ? minioBucketPath : ftpDirectoryPath
  const modelPath = ftp === 'gdb' ? gdbPath : sourceType === 'minio' ? minioObjectPath : ftpFilePath
  const modelKind = sourceType === 'minio' ? 'object' : 'file'
  const writes = [], reads = []
  const fingerprintOf = target => createHash('sha256').update(`2:${target.segments.filter(s => s.name).map(s => s.name).join(ftp ? '/' : '.')}`).digest('hex')
  const rowFor = target => ({ engine_id: '2', item_fingerprint: fingerprintOf(target), full_name: target.segments.filter(s => s.name).map(s => s.name).join(ftp ? '/' : '.'), name: target.segments.at(-1).name, item_type: ftp ? modelKind : 'table', state: permit === 'denied' ? 'denied' : permit === 'unknown' ? 'unknown' : 'allowed', source: ['allowed', 'denied'].includes(permit) ? 'explicit' : permit === 'unknown' ? 'deployment_unknown' : 'deployment_default', version: permit === 'unknown' ? '0' : '1' })
  const items = ftp ? [modelPath(`model.${ftp}`)] : [tablePath, ...(secondTable ? [{ ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'persons' }] }] : [])]
  let rows = (permitRows || items.map(rowFor)).map(row => ({ ...row, ancestors: row.ancestors || (ftp ? [{ id: '1', name: '', node_type: sourceType === 'minio' ? 'service' : 'root' }, { id: '2', name: 'Models', node_type: sourceType === 'minio' ? 'bucket' : 'dir' }, ...(ftp === 'gdb' ? [{ id: '3', name: 'model.gdb', node_type: 'dir' }] : [])] : [{ id: '1', name: '', node_type: 'server' }, { id: '2', name: 'outdoor', node_type: 'schema' }]) })).sort((a, b) => a.item_fingerprint.localeCompare(b.item_fingerprint))
  let grants = [...history], failedGrant = false, failedChange = false, failedDeny = false, failedRelease = false
  const restrictions = [...denies]
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true',
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
    if (path.endsWith('/authorization-facts')) {
      reads.push(request.url())
      const fingerprint = path.split('/').at(-2)
      if (ftp) {
        const primary = modelPath(`model.${ftp}`), candidate = createHash('sha256').update(`2:Models/model.${ftp}`).digest('hex')
        if (fingerprint !== candidate) return reply({ error: 'Not a model DataItem', error_code: 'authorization_target_not_found' }, 404)
        return reply({ schema_version: 'meta.data_item_authorization_facts/v1', engine_id: 2, item_fingerprint: fingerprint, item_type: modelKind,
          full_name: `Models/model.${ftp}`, basis_state: factsState, boundary_hash: factsState === 'complete' ? 'c'.repeat(64) : null,
          boundary: factsState === 'complete' ? ftp === 'gdb' ? { layout: 'whole', scope: { format: 'filegdb', rule_version: 1, root: primary } } : { layout: 'single', file_composition: { format: ftp, refs: [
            ...(ftp === 'obj' ? [{ target: modelPath('model.mtl'), role: 'material_library', required: true, primary: false }] : []),
            ...(['3ds', 'dae', 'obj'].includes(ftp) ? [{ target: modelPath('TEST.PNG'), role: 'texture', required: true, primary: false }] : []),
            { target: primary, role: 'model', required: true, primary: true }
          ] } } : null })
      }
      const candidates = [tablePath, { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'persons' }] }]
      const native = candidates.find(p => createHash('sha256').update(`2:${p.segments[1].name}.${p.segments[2].name}`).digest('hex') === fingerprint)
      if (!native || factsState === 'missing') return reply({ error: 'Missing DataItem', error_code: 'authorization_target_not_found' }, 404)
      if (factsState === 'failed') return reply({ error: 'Facts unavailable', error_code: 'authorization_facts_unavailable' }, 503)
      if (factsState === 'forbidden') return reply({ error: 'Forbidden', error_code: 'forbidden' }, 403)
      if (factsState === 'unknown404') return reply({ error: 'Endpoint not found' }, 404)
      return reply({schema_version:'meta.data_item_authorization_facts/v1',engine_id:2,item_fingerprint:fingerprint,item_type:'table',
      full_name:`${native.segments[1].name}.${native.segments[2].name}`,basis_state:factsState,observed_at:new Date().toISOString(),
      boundary_hash:factsState === 'complete' ? 'b'.repeat(64) : null,boundary:factsState === 'complete' ? {layout:'single',native_target:factsTarget || native} : null})
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
    if (path.endsWith('/access_denies')) {
      const body = request.postDataJSON(); writes.push({ path, body })
      let result = restrictions.find(row => row.deny_id === body.deny_id)
      if (!result) { result = { ...body, established_at: new Date().toISOString() }; restrictions.push(result) }
      if (denyFailure && !failedDeny) { failedDeny = true; return reply({ error: 'Restriction result unknown' }, 503) }
      return reply(result)
    }
    if (path.includes('/access_denies/') && path.endsWith('/release')) {
      const body = request.postDataJSON(); writes.push({ path, body })
      const id = path.split('/').at(-2), row = restrictions.find(row => row.deny_id === id)
      row.released = true
      if (releaseFailure && !failedRelease) { failedRelease = true; return reply({ error: 'Release result unknown' }, 503) }
      return reply({ deny_id: id, released_at: new Date().toISOString() })
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
        let filtered
        if (query.get('recipient_type') === 'user' && query.get('recipient_id')) {
          if (inspectionDenied) return reply({ error: '本引擎管理资格不足', error_code: 'forbidden' }, 403)
          const account = query.get('recipient_id'), expanded = new Map()
          const activeDenies = restrictions.filter(item => !item.released && (!item.expires_at || new Date(item.expires_at) > new Date()))
          const candidates = [...current, ...activeDenies].filter(item => String(item.recipient_id) === account && (item.recipient_type === 'user' || organizationMember))
          for (const source of candidates) {
            const key = JSON.stringify(source.catalog_path), target = expanded.get(key) || { ...source, recipient_type: 'user', recipient_id: account, grant_count: 0, inspection: { account_id: account, catalog_path: source.catalog_path, observed_at: '2026-10-09T08:00:00Z', rule_covered: inspectionReason === 'grant', reason: inspectionReason, sources: [], denies: [] } }
            if (source.deny_id) { target.inspection.denies.push(source); target.inspection.reason = 'explicit_deny'; target.inspection.rule_covered = false }
            else {
              if (['grant', 'explicit_deny', 'target_unavailable'].includes(inspectionReason)) target.inspection.sources.push(source)
              target.grant_count += source.grant_count
            }
            expanded.set(key,target)
          }
          const searchQuery = new URLSearchParams(query); searchQuery.delete('recipient_type'); searchQuery.delete('recipient_id')
          filtered = filterGrants([...expanded.values()], searchQuery)
        } else filtered = filterGrants(current, query)
        return reply({ data: filtered.slice((page - 1) * size, page * size), total: filtered.length, page, page_size: size, total_pages: Math.ceil(filtered.length / size) })
      }
      const body = request.postDataJSON(); writes.push({ path, body })
      if (grantDuplicate) return reply({ error: '该接收方已有此数据的有效读取授权，未重复发放', error_code: 'engine_access_grant_relation_exists' }, 409)
      if (conflict) return reply({ error: '本表已有批准安排，不能覆盖' }, 409)
      if (grantFailure && !failedGrant) { failedGrant = true; return reply({ error: '签发结果未知，请按原命令核对' }, 503) }
      const grant = { ...body, approval_mode: 'independent', engine_id: '2', granted_at: '2026-10-07T00:00:00Z', revocation: null }
      grants.push(grant)
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
    if (path.endsWith('/direct-permission')) {
      const fingerprint = path.split('/').at(-2), index = rows.findIndex(row => row.item_fingerprint === fingerprint)
      if (index < 0) return reply({ error: 'Unknown DataItem' }, 404)
      if (request.method() === 'GET') { reads.push(request.url()); return denied ? reply({ error: '本引擎管理委派不足' }, 403) : reply(rows[index]) }
      const body = request.postDataJSON(); writes.push({ path, body })
      if (changeConflict && !failedChange) {
        failedChange = true; rows[index] = { ...rows[index], source: 'explicit', version: '2' }
        return reply({ error: 'Permission changed; review required', error_code: 'resource_version_conflict' }, 409)
      }
      const current = rows[index]
      expect(body.expected_source).toBe(current.source)
      expect(body.expected_version).toBe(current.version)
      expect(body.expected_boundary_hash).toBe('b'.repeat(64))
      rows[index] = { ...current, state: body.allowed ? 'allowed' : 'denied', source: 'explicit', version: current.source === 'explicit' ? String(Number(current.version) + Number((current.state === 'allowed') !== body.allowed)) : '1' }
      if (changeUnknown && !failedChange) { failedChange = true; return reply({ error: 'Result unknown; review the current permission' }, 503) }
      return reply(rows[index])
    }
    if (path.endsWith('/direct-permissions')) {
      reads.push(request.url())
      if (denied) return reply({ error: '本引擎管理委派不足' }, 403)
      const query = new URL(request.url()).searchParams, page = Number(query.get('page')), size = Number(query.get('page_size'))
      const filtered = rows.filter(row => (!query.get('state') || row.state === query.get('state')) && (!query.get('search') || row.full_name.includes(query.get('search'))))
      return reply({ data: filtered.slice((page - 1) * size, page * size), total: filtered.length, page, page_size: size })
    }
    if (path.endsWith('/catalog/entries')) return reply({ data: [], total: 0 })
    if (path.endsWith('/data-items')) {
      reads.push(request.url())
      const query = new URL(request.url()).searchParams, page = Number(query.get('page')), size = Number(query.get('page_size'))
      const data = rows.map(({ engine_id, item_fingerprint, name, full_name, item_type, ancestors }) => ({ engine_id, item_fingerprint, name, full_name, item_type, ancestors }))
      return reply({ data: data.slice((page-1)*size,page*size), total:data.length,page,page_size:size,total_pages:Math.ceil(data.length/size) })
    }
    const engine = { id: '2', name: '授权验证 PostgreSQL', engine_type: ftp ? sourceType : 'postgresql', engine_origin: 'general', lifecycle_state: 'active', connection_status: 'online', is_builtin: false, capabilities: { schema_version: 'engine.capabilities/v1', storage: {} } }
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines/2')) return reply(engine)
    if (path.endsWith('/engines')) return reply([engine])
    throw new Error(`Unexpected configuration fixture request: ${request.method()} ${path}`)
  })
  return { writes, reads, setFactsState: value => { factsState = value } }
}

async function selectAccountFilter(page) {
  await page.getByTestId('source-grant-type-filter').click()
  await page.getByRole('option', { name: '账号', exact: true }).click()
  await page.getByTestId('source-grant-recipient-filter').locator('.el-select').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
}
test('authorization actions share one row and resource tree precisely filters current and history', async ({ page }, testInfo) => {
  const similarPath = { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'activities_copy' }] }
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant(), { ...historicalGrant(), request_id: 'similar', catalog_path: similarPath }] })
  await page.goto('/engines/2?tab=data-authorization')
  const toolbar = page.getByTestId('authorization-workspace-toolbar')
  await expect.poll(async () => {
    const positions = await Promise.all(['新增授权', '当前授权', '全部授权记录'].map(name => toolbar.locator('.el-radio-button__inner').filter({ hasText: name }).boundingBox()))
    return Math.max(...positions.map(box => box.y + box.height / 2)) - Math.min(...positions.map(box => box.y + box.height / 2))
  }).toBeLessThan(3)
  await selectAccountFilter(page)
  await page.locator('.resource-tree-select__toggle').click()
  const dialog = page.locator('.resource-tree-select__panel')
  const namespace = dialog.getByRole('treeitem', { name: 'outdoor', exact: true })
  await namespace.locator('.el-tree-node__expand-icon').first().click()
  await dialog.getByRole('treeitem', { name: 'activities', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('source-grant-exact-filter')).toBeVisible()
  await expect(page.getByTestId('source-grant-table-filter')).toHaveValue('outdoor / activities')
  await expect(page.getByTestId('source-grant-list').getByTestId('account-grant-expand')).toHaveCount(1)
  await expect(page.getByTestId('source-grant-table-filter')).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('authorization-integrated-filter.png'), fullPage: true })
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(page.getByTestId('source-grant-list')).not.toContainText('similar')
  const query = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
  expect(JSON.parse(query.get('catalog_path'))).toEqual(tablePath)
  expect(query.get('recipient_id')).toBe('33')
  expect(query.has('table_search')).toBe(false)
  await page.getByTestId('authorization-workspace-tabs').getByText('当前授权', { exact: true }).click()
  await expect(page.getByTestId('source-grant-exact-filter')).toBeVisible()
  await expect(page.getByTestId('source-grant-list').getByTestId('account-grant-expand')).toHaveCount(1)
  await expect(page.getByTestId('source-grant-table-filter')).toHaveValue('outdoor / activities')
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await page.getByTestId('source-grant-table-filter').fill('activities')
  await expect(page.getByTestId('source-grant-exact-filter')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-list')).toContainText('similar')
  expect(new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams.has('catalog_path')).toBe(false)
  await page.getByTestId('source-grant-table-filter').fill('')
  await expect.poll(() => new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams.has('table_search')).toBe(false)
  expect(writes).toHaveLength(0)
})

test('integrated table filter closes its tree without closing the authorization workspace', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  const input = page.getByTestId('source-grant-table-filter')
  await input.click(); await input.fill('activ')
  await expect(page.locator('.resource-tree-select__panel')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-list')).toContainText('activities')
  await input.press('ArrowDown')
  await expect(page.locator('.resource-tree-select__panel')).toBeVisible()
  await input.press('Escape')
  await expect(page.locator('.resource-tree-select__panel')).toBeHidden()
  await expect(page.getByTestId('authorization-workspace-tabs')).toBeVisible()
  await page.locator('.resource-tree-select__toggle').click()
  await expect(page.locator('.resource-tree-select__panel')).toBeVisible()
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(page.locator('.resource-tree-select__panel')).toHaveCount(0)
  await expect(input).toHaveValue('activ')
  expect(writes).toEqual([])
})

test('Meta identity tree and text filtering do not require native catalog permission', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: grantPermissions.filter(key => key !== 'system.engine_catalog.read'), history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  await page.getByRole('button', { name: '资源树选择', exact: true }).click()
  await expect(page.locator('.resource-tree-select__panel')).toBeVisible()
  await expect.poll(() => reads.some(url => /\/data-items\?/.test(url))).toBe(true)
  await page.getByTestId('source-grant-table-filter').press('Escape')
  await page.getByTestId('source-grant-table-filter').fill('not-found')
  await expect.poll(() => reads.some(url => url.includes('table_search=not-found'))).toBe(true)
  expect(reads.some(url => url.endsWith('/catalog/children'))).toBe(false)
  expect(writes).toHaveLength(0)
})
test('current authorization automatically includes personal department and project sources for a selected account', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read', 'iam.project_group.read'], organizationMember: true,
    history: ['user','department','project_group'].map(kind => historicalGrant(kind)) })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  const list = page.getByTestId('source-grant-list')
  await expect(list.getByTestId('account-grant-status')).toContainText('源规则覆盖')
  await expect(list.locator('.el-table__body > tbody > tr.el-table__row')).toHaveCount(1)
  await expect(list.getByTestId('account-grant-expand')).toHaveText('查看来源（3）')
  await list.getByTestId('account-grant-expand').click()
  const sources = page.getByTestId('account-grant-sources')
  await expect(sources).toContainText('Outdoor reader')
  await expect(sources).toContainText('Outdoor department')
  await expect(sources).toContainText('Outdoor team')
  await page.screenshot({ path: '/tmp/addp-unified-current-account-sources.png', fullPage: true })
  await sources.getByRole('row').filter({ hasText: 'Outdoor department' }).getByTestId('source-grant-revoke').click()
  const organizationDialog = page.getByRole('dialog', { name: '撤销授权', exact: true })
  await expect(organizationDialog).toContainText('所有依赖此来源的成员')
  await organizationDialog.getByRole('button', { name: '取消', exact: true }).click()
  await sources.getByRole('row').filter({ hasText:'Outdoor reader' }).getByTestId('source-grant-revoke').click()
  const dialog = page.getByRole('dialog', { name:'撤销授权', exact:true })
  await dialog.getByTestId('source-grant-revoke-reason').fill('Remove personal source only')
  await dialog.getByTestId('source-grant-revoke-confirm').click()
  await expect(dialog).toBeHidden()
  await expect(list.getByTestId('account-grant-status')).toContainText('源规则覆盖')
  await list.locator('.el-table__expand-icon').click()
  await expect(page.getByTestId('account-grant-sources')).not.toContainText('Outdoor reader')
  await expect(page.getByTestId('account-grant-sources')).toContainText('Outdoor department')
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact:true }).click()
  await expect(list).toContainText('已撤销')
  await expect(list).not.toContainText('Outdoor department')
  expect(writes).toHaveLength(1)
})
test('authorization table keeps current account sources and history isolated across round trips', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => {
    if (message.text().includes('system.engine.dataAuthorization.modes.undefined')) errors.push(message.text())
  })
  const { writes } = await fixture(page, { allowed: grantPermissions, history: [
    historicalGrant(),
    { ...historicalGrant(), request_id: 'expired-history', expires_at: '2020-01-01T00:00:00Z' }
  ] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  const list = page.getByTestId('source-grant-list')
  const mode = page.getByTestId('authorization-workspace-tabs')
  for (let round = 0; round < 2; round++) {
    await expect(list.getByTestId('account-grant-expand')).toHaveText('查看来源（1）')
    await list.getByTestId('account-grant-expand').click()
    await expect(page.getByTestId('account-grant-sources')).toContainText('Outdoor reader')
    await mode.getByText('全部授权记录', { exact: true }).click()
    await expect(list).toContainText('expired-history')
    await expect(list).toContainText('已到期')
    await expect(list.getByTestId('account-grant-expand')).toHaveCount(0)
    await mode.getByText('当前授权', { exact: true }).click()
    await expect(list.getByTestId('account-grant-expand')).toHaveText('查看来源（1）')
    await expect(list).not.toContainText('expired-history')
  }
  expect(errors).toEqual([])
  expect(writes).toEqual([])
})
for (const reason of ['explicit_deny','source_unavailable','target_unavailable']) {
  test(`current account authorization reports ${reason} without granting access`, async ({page}) => {
    const {writes} = await fixture(page,{allowed:grantPermissions,history:[historicalGrant()],inspectionReason:reason})
    await page.goto('/engines/2?tab=data-authorization')
    await selectAccountFilter(page)
    await expect(page.getByTestId('account-grant-status')).toContainText(reason==='explicit_deny'?'命中源拒绝规则':reason==='source_unavailable'?'账号或租户成员关系当前无效':'引擎当前不可用')
    expect(writes).toEqual([])
  })
}
test('current account query denial stays in the authorization window', async ({page}) => {
  await fixture(page,{allowed:grantPermissions,history:[historicalGrant()],inspectionDenied:true})
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('本引擎管理资格不足')
  await expect(page).toHaveURL(/engines\/2\?tab=data-authorization$/)
})

test('current account prohibits and releases reading without changing Grants', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'system.engine_access_deny.create', 'system.engine_access_deny.release'], history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  await page.getByTestId('source-deny-create').click()
  const dialog = page.getByRole('dialog', { name: '禁止读取', exact: true })
  await expect(dialog).toContainText('Outdoor reader')
  await expect(dialog).toContainText('不会撤销已有授权')
  await dialog.getByTestId('source-deny-confirm').click()
  await expect(dialog).toContainText('请填写原因')
  expect(writes).toEqual([])
  await dialog.getByTestId('source-deny-expiry').click()
  await page.getByRole('option', { name: '长期有效，直到解除', exact: true }).click()
  await dialog.getByTestId('source-deny-reason').fill('Explicit account restriction')
  await dialog.getByTestId('source-deny-confirm').click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('account-grant-status')).toContainText('命中源拒绝规则')
  await expect(page.getByTestId('source-deny-create')).toBeDisabled()
  await page.getByTestId('account-grant-expand').click()
  await expect(page.getByTestId('account-source-denies')).toContainText('Outdoor reader')
  await page.getByTestId('source-deny-release').click()
  const release = page.getByRole('dialog', { name: '解除禁止', exact: true })
  await expect(release).toContainText('不创建、恢复或续期读取授权')
  await release.getByTestId('source-deny-reason').fill('End this restriction only')
  await release.getByTestId('source-deny-confirm').click()
  await expect(release).toBeHidden()
  await expect(page.getByTestId('account-grant-status')).toContainText('源规则覆盖')
  expect(writes).toHaveLength(2)
  expect(writes[0].body).toMatchObject({ recipient_type: 'user', recipient_id: '33', action: 'read', expiry_mode: 'until_revoked', expires_at: null,
    item_fingerprint: historicalGrant().item_fingerprint, catalog_path: tablePath })
  expect(writes[1].path).toContain(`/access_denies/${writes[0].body.deny_id}/release`)
  expect(writes.every(row => row.path.includes('/access_denies'))).toBe(true)
})

test('Deny dialog opening animation never steals focus from an opened expiry selector', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.clock.install()
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'system.engine_access_deny.create'], history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  // Keep layout stable while delaying Vue's after-enter callback. A real user
  // can open the selector before that callback; it must not later steal focus.
  await page.addStyleTag({ content: '.dialog-fade-enter-active { animation: none !important; transition: opacity 10s linear !important; }' })
  await page.getByTestId('source-deny-create').click()
  const dialog = page.getByRole('dialog', { name: '禁止读取', exact: true })
  await dialog.getByTestId('source-deny-expiry').click()
  const option = page.getByRole('option', { name: '长期有效，直到解除', exact: true })
  await expect(option).toBeVisible()
  await page.clock.runFor(11000)
  await expect(option).toBeVisible()
  await option.click()
  await expect(dialog.getByTestId('source-deny-expiry')).toContainText('长期有效，直到解除')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  expect(errors).toEqual([])
})

test('revoke dialog opening animation preserves the reason input focus', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.clock.install()
  const { writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  await page.getByTestId('account-grant-expand').click()
  await page.addStyleTag({ content: '.dialog-fade-enter-active { animation: none !important; transition: opacity 10s linear !important; }' })
  await page.getByTestId('source-grant-revoke').click()
  const dialog = page.getByRole('dialog', { name: '撤销授权', exact: true })
  const reason = dialog.getByTestId('source-grant-revoke-reason')
  await reason.fill('Do not interrupt this input')
  await page.clock.runFor(11000)
  await expect(reason).toBeFocused()
  await expect(reason).toHaveValue('Do not interrupt this input')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  expect(errors).toEqual([])
})

test('unknown Deny and release outcomes retry exactly the original commands', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'system.engine_access_deny.create', 'system.engine_access_deny.release'], history: [historicalGrant()], denyFailure: true, releaseFailure: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  await page.getByTestId('source-deny-create').click()
  const dialog = page.getByRole('dialog', { name: '禁止读取', exact: true })
  await dialog.getByTestId('source-deny-expiry').click()
  await page.getByRole('option', { name: '长期有效，直到解除', exact: true }).click()
  await dialog.getByTestId('source-deny-reason').fill('Keep immutable command')
  await dialog.getByTestId('source-deny-confirm').click()
  await expect(dialog).toContainText('Restriction result unknown')
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeDisabled()
  await expect(dialog.getByTestId('source-deny-confirm')).toHaveText('按原命令重试')
  await dialog.getByTestId('source-deny-confirm').click()
  await expect(dialog).toBeHidden()
  expect(writes[0]).toEqual(writes[1])
  await page.getByTestId('account-grant-expand').click()
  await expect(page.getByTestId('source-deny-release')).toHaveCount(1)
  await page.getByTestId('source-deny-release').click()
  const release = page.getByRole('dialog', { name: '解除禁止', exact: true })
  await release.getByTestId('source-deny-reason').fill('Keep release reason')
  await release.getByTestId('source-deny-confirm').click()
  await expect(release).toContainText('Release result unknown')
  await release.getByTestId('source-deny-confirm').click()
  await expect(release).toBeHidden()
  expect(writes[2]).toEqual(writes[3])
  expect(writes).toHaveLength(4)
})

test('Deny-only data remains discoverable and organization release warns about its full scope', async ({ page }) => {
  const deny = { deny_id: '17824529-7cbf-43db-bbe1-c31917836022', catalog_path: tablePath, item_fingerprint: historicalGrant().item_fingerprint,
    recipient_type: 'department', recipient_id: '33', expiry_mode: 'until_revoked', expires_at: null }
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read', 'system.engine_access_deny.release'], organizationMember: true, denies: [deny] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  await expect(page.getByTestId('source-deny-create')).toHaveCount(0)
  await page.getByTestId('account-grant-expand').click()
  await expect(page.getByTestId('account-grant-sources')).toContainText('没有当前有效的授权来源')
  await page.getByTestId('source-deny-release').click()
  const dialog = page.getByRole('dialog', { name: '解除禁止', exact: true })
  await expect(dialog).toContainText('所有受此规则约束的成员')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  await page.getByTestId('source-deny-release').click()
  await dialog.getByTestId('source-deny-reason').fill('Release department restriction')
  await dialog.getByTestId('source-deny-confirm').click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('source-grant-list')).toContainText('当前筛选下没有授权记录')
  expect(writes).toHaveLength(1)
})

test('grant-read permission does not confer Deny creation or release', async ({ page }) => {
  const deny = { deny_id: '17824529-7cbf-43db-bbe1-c31917836022', catalog_path: tablePath, recipient_type: 'user', recipient_id: '33', expiry_mode: 'until_revoked', expires_at: null }
  const { writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant()], denies: [deny] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectAccountFilter(page)
  await page.getByTestId('account-grant-expand').click()
  await expect(page.getByTestId('account-source-denies')).toContainText('Outdoor reader')
  await expect(page.getByTestId('source-deny-create')).toHaveCount(0)
  await expect(page.getByTestId('source-deny-release')).toHaveCount(0)
  expect(writes).toEqual([])
})

test('Deny confirmation is translated and usable on a narrow viewport without writing on cancel', async ({ page }, testInfo) => {
  const { writes } = await fixture(page, { allowed: [...grantPermissions, 'system.engine_access_deny.create'], history: [historicalGrant()], language: 'en' })
  await page.goto('/engines/2?tab=data-authorization')
  const type = page.getByTestId('source-grant-type-filter')
  await type.click(); await page.getByRole('option', { name: 'Account', exact: true }).click()
  const recipient = page.getByTestId('source-grant-recipient-filter')
  await recipient.click(); await page.getByRole('option', { name: /Outdoor reader/ }).click()
  await page.getByTestId('source-deny-create').click()
  await page.setViewportSize({ width: 390, height: 844 })
  const dialog = page.getByRole('dialog', { name: 'Prohibit reading', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('without revoking them')
  await dialog.getByTestId('source-deny-reason').fill('Validate layout only')
  await expect(dialog.getByTestId('source-deny-reason')).toBeFocused()
  const bounds = await dialog.boundingBox()
  expect(bounds.x).toBeGreaterThanOrEqual(0)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
  await page.screenshot({ path: testInfo.outputPath('source-deny-mobile-en.png'), fullPage: true })
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect(writes).toEqual([])
})

test('list authorization action opens a separate window with permission-specific sections and survives reload', async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: [...permissions, 'system.engine_access_delegation.read'] })
  await page.goto('/engines')
  const row = page.getByRole('row').filter({ hasText: '授权验证 PostgreSQL' })
  await row.getByTestId('engine-authorization-open').click()
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2?tab=data-authorization`)
  const dialog = page.getByRole('dialog', { name: '引擎授权 - 授权验证 PostgreSQL', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '数据授权', exact: true })).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '委托授权', exact: true })).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '基本信息', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('engine-access-delegations')).toHaveCount(0)
  expect(reads.some(path => path.endsWith('/access_delegations'))).toBe(false)
  await dialog.getByRole('tab', { name: '委托授权', exact: true }).click()
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2?tab=delegations`)
  await expect(page.getByTestId('engine-access-delegations')).toBeVisible()
  await expect(page.getByTestId('engine-data-authorization')).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole('dialog', { name: '引擎授权 - 授权验证 PostgreSQL', exact: true })).toBeVisible()
  await expect(page.getByTestId('engine-access-delegations')).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines`)
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
const permitAdminPermissions = [...grantPermissions, 'system.engine_access_direct_permission.update', 'system.engine_access_delegation.create']

function historicalGrant(kind = 'user') {
  return { request_id: `history-${kind}`, engine_id: '2', catalog_path: tablePath, recipient_type: kind, recipient_id: '33',
    item_fingerprint: createHash('sha256').update('2:outdoor.activities').digest('hex'),
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
async function selectData(page, schema = false, incomplete = false) {
  const panel = page.getByTestId('engine-data-authorization')
  await expect(page.getByTestId('authorization-workspace-toolbar')).toBeVisible()
  if (await page.getByTestId('authorization-new').isVisible()) await page.getByTestId('authorization-new').click()
  await page.locator('.resource-tree-select__toggle').click()
  const picker = page.locator('.resource-tree-select__panel')
  if (schema) await picker.getByRole('treeitem', { name: 'outdoor', exact: true }).click()
  else {
    const namespace = picker.getByRole('treeitem', { name: 'outdoor', exact: true })
    if (await namespace.getAttribute('aria-expanded') !== 'true') await namespace.locator(':scope > .el-tree-node__content .el-tree-node__expand-icon').click()
    await picker.getByRole('treeitem', { name: 'activities', exact: true }).click()
  }
  await page.getByTestId('approval-add-selection').click()
  if (!incomplete) await expect(page.getByTestId('approval-targets')).toBeVisible()
  return panel
}
test('new authorization unifies Meta identity search and tree selection without granting from free text', async ({ page }, testInfo) => {
  await page.clock.install()
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization')
  await page.getByTestId('authorization-new').click()
  const input = page.getByTestId('approval-resource-input')
  await expect(page.locator('.resource-tree-select__panel')).toHaveCount(0)
  await input.fill('activities')
  await page.clock.runFor(350)
  await expect(page.getByTestId('approval-add-selection')).toBeDisabled()
  const picker = page.locator('.resource-tree-select__panel')
  const result = picker.getByRole('button', { name: /activities outdoor.activities/ })
  await expect(result).toBeVisible()
  const refresh = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/engines'))
  await page.clock.runFor(10000); await refresh
  await expect(result).toBeVisible()
  await expect(picker).not.toContainText('activities_view')
  await result.click()
  await expect(picker).toBeHidden()
  await expect(input).toHaveValue('outdoor.activities')
  await expect(input).toBeFocused()
  await expect(page.getByTestId('approval-add-selection')).toBeEnabled()
  expect(reads.some(url => url.includes('/authorization-facts'))).toBe(false)
  await input.fill('arbitrary table')
  await expect(page.getByTestId('approval-add-selection')).toBeDisabled()
  await input.press('ArrowDown')
  await expect(picker.getByRole('tree')).toBeVisible()
  const namespace = picker.getByRole('treeitem', { name: 'outdoor', exact: true })
  await namespace.locator(':scope > .el-tree-node__content .el-tree-node__expand-icon').click()
  await picker.getByRole('treeitem', { name: 'activities', exact: true }).click()
  await page.getByTestId('approval-add-selection').click()
  await expect(page.getByTestId('approval-targets')).toContainText('outdoor / activities')
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
  await page.screenshot({ path: testInfo.outputPath('new-authorization-integrated-picker.png'), fullPage: true, animations: 'disabled' })
  expect(reads.some(url => url.includes('/authorization-facts'))).toBe(true)
  expect(writes).toEqual([])
})
test('new authorization picker closes with Escape and does not overflow narrow windows', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions })
  await page.setViewportSize({ width: 760, height: 720 })
  await page.goto('/engines/2?tab=data-authorization')
  await page.getByTestId('authorization-new').click()
  const input = page.getByTestId('approval-resource-input')
  await input.fill('activities')
  const picker = page.locator('.resource-tree-select__panel')
  await expect(picker.getByRole('button', { name: /activities outdoor.activities/ })).toBeVisible()
  const box = await picker.boundingBox()
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(760)
  await input.press('Escape')
  await expect(picker).toBeHidden()
  await expect(page.getByTestId('authorization-workspace-toolbar')).toBeVisible()
  await expect(input).toHaveValue('activities')
  expect(writes).toEqual([])
})
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
async function selectFTPModel(page, format) {
  await page.goto('/engines/2?tab=data-authorization')
  await page.getByTestId('authorization-new').click()
  const input = page.getByTestId('approval-resource-input')
  await input.fill(`model.${format}`)
  await page.locator('.resource-tree-select__panel').getByRole('button', { name: new RegExp(`model\\.${format}`) }).click()
  await page.getByTestId('approval-add-selection').click()
}
for (const [sourceType, format] of [['ftp','ifc'], ['ftp','3ds'], ['ftp','dae'], ['minio','ifc'], ['minio','3ds'], ['minio','dae'], ['ftp','obj'], ['minio','obj'], ['ftp','stl'], ['nfs','stl'], ['minio','stl'], ['ftp','glb'], ['nfs','glb'], ['minio','glb'], ['ftp','fbx'], ['nfs','fbx'], ['minio','fbx'], ['nfs','gdb']]) test(`${sourceType} ${format} uses one full DataItem Grant, confirms constituents and supports exact filtering and revocation`, async ({ page }, testInfo) => {
  const { reads, writes } = await fixture(page, { allowed: [...grantPermissions, 'catalog.entry.read'], ftp: format, sourceType })
  const expectedPath = format === 'gdb' ? gdbPath : sourceType === 'minio' ? minioObjectPath : ftpFilePath
  await selectFTPModel(page, format)
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
  await grantDraft(page)
  await page.getByTestId('source-grant-confirm').click()
  const confirmation = page.locator('.el-message-box')
  await expect(confirmation).toContainText(format === 'gdb' ? '当前及以后新增的子表／要素类' : '完整文件组成')
  await expect(confirmation).toContainText(`Models / model.${format}`)
  if (format === 'stl' || format === 'glb' || format === 'fbx') {
    await expect(confirmation).not.toContainText('TEST.PNG')
    await expect(confirmation).not.toContainText('model.mtl')
  }
  if (format === 'obj') await expect(confirmation).toContainText('Models / model.mtl')
  if (['3ds', 'dae', 'obj'].includes(format)) await expect(confirmation).toContainText('Models / TEST.PNG')
  await confirmation.getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('只读授权已签发')
  expect(writes).toHaveLength(1)
  expect(writes[0].body).toMatchObject({ catalog_path: expectedPath(`model.${format}`), item_fingerprint: createHash('sha256').update(`2:Models/model.${format}`).digest('hex'), expected_boundary_hash: 'c'.repeat(64), direct_permission_source: 'deployment_default', direct_permission_version: '1', action: 'read', recipient_id: '33' })
  expect(writes[0].body.file_composition).toBeUndefined()
  expect(writes[0].body.scope).toBeUndefined()
  expect(writes[0].body.scope_boundary).toBeUndefined()
  const filter = page.getByTestId('source-grant-table-filter')
  await filter.press('ArrowDown')
  const picker = page.locator('.resource-tree-select__panel')
  await picker.getByRole('treeitem', { name: 'Models', exact: true }).locator(':scope > .el-tree-node__content .el-tree-node__expand-icon').click()
  await picker.getByRole('treeitem', { name: `model.${format}`, exact: true }).click()
  await expect(page.getByTestId('source-grant-exact-filter')).toBeVisible()
  await expect.poll(() => reads.filter(url => url.includes('catalog_path=')).length).toBeGreaterThan(0)
  expect(JSON.parse(new URL(reads.filter(url => url.includes('catalog_path=')).at(-1)).searchParams.get('catalog_path'))).toEqual(expectedPath(`model.${format}`))
  await page.getByTestId('account-grant-expand').click()
  await page.getByTestId('source-grant-revoke').click()
  await page.getByTestId('source-grant-revoke-reason').fill('Fixture verification complete')
  await page.getByTestId('source-grant-revoke-confirm').click()
  await expect(page.getByTestId('source-grant-list')).toContainText('当前筛选下没有授权记录')
  expect(writes).toHaveLength(2)
  expect(writes[1].path).toMatch(/\/access_grants\/[^/]+\/revoke$/)
  await page.screenshot({ path: testInfo.outputPath(`${sourceType}-${format}-revoked.png`), fullPage: true })
})
for (const sourceType of ['ftp', 'minio']) test(`${sourceType} Meta tree excludes composition fragments and node selection alone never writes`, async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: grantPermissions, ftp: '3ds', sourceType })
  await page.goto('/engines/2?tab=data-authorization'); await page.getByTestId('authorization-new').click()
  await page.getByTestId('approval-resource-input').press('ArrowDown')
  const picker = page.locator('.resource-tree-select__panel'), directory = picker.getByRole('treeitem', { name: 'Models', exact: true })
  await directory.locator(':scope > .el-tree-node__content .el-tree-node__expand-icon').click()
  await expect(picker.getByRole('treeitem', { name: 'TEST.PNG', exact: true })).toHaveCount(0)
  await expect(picker.getByRole('treeitem', { name: 'model.3ds', exact: true })).toBeVisible()
  await directory.locator(':scope > .el-tree-node__content').getByText('Models', { exact: true }).click()
  await expect(page.getByTestId('approval-add-selection')).toBeEnabled()
  expect(reads.some(url=>url.includes('/catalog/children'))).toBe(false)
  expect(writes).toEqual([])
})
for (const factsState of ['incomplete', 'needs_refresh']) test(`FTP ${factsState} cannot issue a whole-model Grant`, async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, ftp: '3ds', factsState })
  await selectFTPModel(page, '3ds')
  await expect(page.getByTestId('engine-data-authorization').locator('.el-alert--error')).toBeVisible()
  await expect(page.getByTestId('approval-targets')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-confirm')).toHaveCount(0)
  expect(writes).toEqual([])
})
for (const [factsState, label] of [
  ['missing', '未找到数据项'], ['incomplete', '基础识别不完整'], ['needs_refresh', '边界需要刷新'],
  ['unsupported', '当前类型暂不支持'], ['failed', '核对失败'], ['forbidden', '无数据项核对权限'], ['unknown404', '核对失败']
]) {
  test(`Meta identity selection cannot issue without complete DataItem facts: ${factsState}`, async ({ page }) => {
    const { reads, writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', factsState })
    await page.goto('/engines/2?tab=data-authorization')
    await selectData(page, false, true)
    await expect(page.getByTestId('engine-data-authorization').locator('.el-alert--error')).toBeVisible()
    await expect(page.getByTestId('approval-targets')).toHaveCount(0)
    await expect(page.getByTestId('source-grant-confirm')).toHaveCount(0)
    expect(reads.some(url => url.includes('/authorization-facts'))).toBe(true)
    expect(writes).toEqual([])
  })
}
test('DataItem basis identity ignores PostgreSQL jsonb object key ordering', async ({ page }) => {
  // jsonb reorders object keys; native catalog candidates retain Go field order.
  // Object key order and an exactly representable Engine ID representation are
  // not a different target, but ordered segment values still are.
  const reorderedPath = { version: tablePath.version, segments: tablePath.segments.map(({ term, kind, name }) => ({ kind, name, term })), engine_id: '2' }
  const { reads, writes, setFactsState } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', factsTarget: reorderedPath })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page)
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
  await expect(page.getByTestId('source-grant-confirm')).toBeEnabled()
  setFactsState('incomplete')
  await page.getByTestId('approval-basis-refresh').click()
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别不完整')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
  setFactsState('complete')
  await page.getByTestId('approval-basis-refresh').click()
  await expect(page.getByTestId('source-grant-confirm')).toBeEnabled()
  await confirmGrant(page)
  await expect(page.getByTestId('source-grant-list')).toContainText('outdoor / activities')
  expect(writes).toHaveLength(1)
  expect(writes[0].body.catalog_path).toEqual(tablePath)
  expect(writes[0].body.item_fingerprint).toBe(createHash('sha256').update('2:outdoor.activities').digest('hex'))
  expect(writes[0].body.expected_boundary_hash).toBe('b'.repeat(64))
  expect(reads.filter(url => url.includes('/authorization-facts'))).toHaveLength(5)
})
test('DataItem basis recheck refreshes only facts and preserves the grant draft', async ({ page }) => {
  const { reads, writes, setFactsState } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', factsState: 'complete' })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page)
  setFactsState('missing')
  await page.getByTestId('approval-basis-refresh').click()
  await expect(page.getByTestId('approval-basis-status')).toContainText('未找到数据项')
  setFactsState('complete')
  const before = reads.filter(url => url.includes('/authorization-facts')).length
  await page.getByTestId('approval-basis-refresh').click()
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
  await expect(page.getByTestId('source-grant-confirm')).toBeEnabled()
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  expect(reads.filter(url => url.includes('/authorization-facts'))).toHaveLength(before + 1)
  expect(writes).toEqual([])
})
test('DataItem facts observation is skipped without its function permission', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: permissions.filter(permission => permission !== 'system.engine_access_grant.read') })
  await page.goto('/engines/2?tab=data-authorization'); await page.getByTestId('authorization-new').click()
  await expect(page.getByTestId('approval-resource-input')).toHaveCount(0)
  await expect(page.getByTestId('engine-data-authorization')).toContainText('源数据授权读取权限')
  expect(reads.some(url => url.includes('/authorization-facts'))).toBe(false)
  expect(writes).toEqual([])
})
test('complete DataItem display cannot replace fresh facts verification at signing', async ({ page }) => {
  const { reads, writes, setFactsState } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
  setFactsState('incomplete')
  await page.getByTestId('source-grant-confirm').click()
  await expect(page.getByTestId('source-grant-form').locator('.el-alert--error')).toBeVisible()
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  expect(reads.filter(url => url.includes('/authorization-facts'))).toHaveLength(3)
  expect(writes).toEqual([])
  await page.getByTestId('approval-basis-refresh').click()
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别不完整')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
})
test('late DataItem basis responses cannot overwrite a removed and re-added target', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page)
  await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
  let release, started, hold = true
  const pending = new Promise(resolve => { release = resolve }), observed = new Promise(resolve => { started = resolve })
  await page.route('**/authorization-facts', async route => {
    if (!hold) return route.fallback()
    hold = false; started(); await pending
    await route.fulfill({ status: 503, json: { error: 'Delayed facts failure' } })
  })
  try {
    await page.getByTestId('approval-basis-refresh').click(); await observed
    await expect(page.getByTestId('approval-basis-status')).toContainText('正在核对')
    await page.getByTestId('approval-targets').getByRole('button', { name: '移除', exact: true }).click()
    await page.getByTestId('approval-add-selection').click()
    await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
    const response = page.waitForResponse(item => item.url().includes('/authorization-facts') && item.status() === 503)
    release(); await response
    await expect(page.getByTestId('approval-basis-status')).toContainText('基础识别完整')
    expect(writes).toEqual([])
  } finally { release() }
})
async function refreshSameAuthorization(page) {
  await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().fetchAuthContext())
}
test('authorization workspace separates current history and creation without writes on navigation', async ({ page }, testInfo) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', history: [historicalGrant()] })
  await page.goto('/engines/2?tab=data-authorization')
  const list = page.getByTestId('source-grant-list')
  await expect(list).toContainText('Outdoor reader')
  await expect(page.getByTestId('source-grant-inspection')).toHaveCount(0)
  await expect(page.getByTestId('approval-add-selection')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  const navigation = page.getByTestId('authorization-workspace-tabs')
  await expect(navigation.getByRole('radio', { checked: true })).toHaveCount(1)
  await expect(navigation.getByRole('radio', { name: '当前授权', exact: true })).toBeChecked()
  await navigation.getByText('全部授权记录', { exact: true }).click()
  await expect(navigation.getByRole('radio', { checked: true })).toHaveCount(1)
  await expect(list).toContainText('history-user')
  await expect(page.getByTestId('source-grant-revoke')).toHaveCount(0)
  await expect(page.getByTestId('engine-source-grants')).toContainText('全部签发记录：当前有效（已签发）、已撤销和已到期')
  await selectData(page); await grantDraft(page)
  await expect(list).toHaveCount(0)
  await expect(navigation).toBeVisible()
  await expect(navigation.getByRole('radio', { checked: true })).toHaveCount(1)
  await expect(navigation.getByRole('radio', { name: '新增授权', exact: true })).toBeChecked()
  await expect(page.getByTestId('authorization-back')).toHaveCount(0)
  await expect(navigation).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('authorization-create-navigation.png'), fullPage: true })
  await expect(page.getByTestId('source-grant-inspection')).toHaveCount(0)
  await page.getByTestId('source-grant-form').getByRole('button', { name: '取消', exact: true }).click()
  await expect(list).toContainText('Outdoor reader')
  await expect(navigation.getByRole('radio', { name: '当前授权', exact: true })).toBeFocused()
  await expect(navigation.getByRole('radio', { checked: true })).toHaveCount(1)
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('authorization-current.png'), fullPage: true })
  expect(writes).toEqual([])
})

test('successful new authorization returns to current grants and locates the issued relation', async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', history: [historicalGrant('department')] })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page); await confirmGrant(page)
  const list = page.getByTestId('source-grant-list')
  await expect(list).toContainText('Outdoor reader')
  await expect(list.getByRole('row').filter({ has: page.getByTestId('grant-recipient') })).toHaveCount(1)
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  await expect(page.getByTestId('approval-add-selection')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-table-filter')).toHaveValue('outdoor / activities')
  const query = new URL(reads.filter(url => url.includes('/access_grants?')).at(-1)).searchParams
  expect(Object.fromEntries(query)).toMatchObject({ recipient_type: 'user', recipient_id: '33', table_search: 'outdoor / activities' })
  // Creation replaces the form with a new table instance. The source action
  // must use that instance, including after a refresh replaces its row data.
  const sources = page.getByTestId('account-grant-sources')
  for (let round = 0; round < 2; round++) {
    await list.getByTestId('account-grant-expand').click()
    await expect(sources).toContainText('Outdoor reader')
    await list.getByTestId('account-grant-expand').click()
    await expect(sources).toHaveCount(0)
    if (round === 0) {
      const response = page.waitForResponse(response => response.request().method() === 'GET' && response.url().includes('/access_grants?'))
      await page.getByTestId('engine-source-grants').getByRole('button', { name: '刷新', exact: true }).click()
      await response
      await expect(list.getByTestId('account-grant-expand')).toBeVisible()
    }
  }
  expect(writes).toHaveLength(1)
})

test('English authorization workspace actions fit a narrow viewport', async ({ page }, testInfo) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, language: 'en', history: [historicalGrant()] })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/engines/2?tab=data-authorization')
  const toolbar = page.getByTestId('authorization-workspace-toolbar')
  await expect(toolbar.getByRole('radio', { name: 'New authorization', exact: true })).toBeVisible()
  for (const action of ['authorization-new']) {
    const bounds = await page.getByTestId(action).boundingBox()
    expect(bounds.x).toBeGreaterThanOrEqual(0)
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
  }
  await page.screenshot({ path: testInfo.outputPath('authorization-workspace-mobile-en.png'), fullPage: true })
  expect(writes).toEqual([])
})
test('deployment-default permission grants without modifying the DataItem permission', async ({ page }, testInfo) => {
  const { writes, reads } = await fixture(page, { allowed: grantPermissions })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page)
  expect(writes).toEqual([])
  await page.screenshot({ path: testInfo.outputPath('first-direct-authorization.png') })
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('只读授权已签发')
  expect(writes).toHaveLength(1)
  expect(writes[0].path).toMatch(/access_grants$/)
  expect(writes[0].body).toMatchObject({ catalog_path: tablePath, direct_permission_source: 'deployment_default', direct_permission_version: '1',
    recipient_id: '33', recipient_type: 'user', action: 'read', expiry_mode: 'until_revoked', expires_at: null })
  expect(reads.every(path => new URL(path, browserTestOrigin('system')).pathname.startsWith('/api/v1/system/'))).toBe(true)
  await selectData(page)
  await expect(page.getByTestId('approval-mode')).toHaveCount(0)
  await grantDraft(page); await confirmGrant(page)
  expect(writes).toHaveLength(2)
  expect(writes[1].body.direct_permission_source).toBe('deployment_default')
})
test('schema expands current ordinary tables, permits removal, and never grants the parent or view', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, secondTable: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page, true)
  const targets = page.getByTestId('approval-targets')
  await expect(targets).toContainText('activities'); await expect(targets).toContainText('persons')
  await expect(targets).not.toContainText('activities_view')
  await targets.getByRole('row').filter({ hasText: 'persons' }).getByRole('button', { name: '移除' }).click()
   await grantDraft(page); await confirmGrant(page)
  expect(writes).toHaveLength(1); expect(writes[0].body.catalog_path).toEqual(tablePath)
})
test('partial batch keeps immutable commands and retries only failed tables', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, secondTable: true, grantFailure: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page, true); await grantDraft(page); await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('签发结果未知')
  expect(writes).toHaveLength(2)
  await expect(page.getByTestId('approval-add-selection')).toBeDisabled()
  await expect(page.getByTestId('authorization-workspace-tabs').getByRole('radio', { name: '当前授权', exact: true })).toBeDisabled()
  await expect(page.getByTestId('source-grant-list')).toHaveCount(0)
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('已找回原命令的签发历史')
  expect(writes).toHaveLength(3); expect(writes[2].body).toEqual(writes[0].body)
  expect(writes[1].body.request_id).not.toBe(writes[0].body.request_id)
})
test('explicit direct permission grants and revokes an exact recipient', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page); await confirmGrant(page)
  const history = page.getByTestId('engine-source-grants')
  await expect(history).toContainText('Outdoor reader')
  await expect(history).toContainText('outdoor')
  await expect(history).not.toContainText('账号 · 33')
  expect(writes).toHaveLength(1); expect(writes[0].body.direct_permission_source).toBe('explicit')
  await page.getByTestId('source-grant-list').locator('.el-table__expand-icon').click()
  await page.getByTestId('source-grant-revoke').click()
  await expect(page.getByRole('dialog', { name: '撤销授权', exact: true })).toContainText('Outdoor reader')
  await page.getByTestId('source-grant-revoke-reason').fill('Read access no longer needed')
  await page.getByTestId('source-grant-revoke-confirm').click()
  await expect(page.getByRole('dialog', { name: '撤销授权', exact: true })).toBeHidden()
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(history).toContainText('已撤销')
  expect(writes).toHaveLength(2)
})
test('unchanged engine and authorization refresh preserve the draft; losing permission clears it', async ({ page }) => {
  await page.clock.install()
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
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
for (const permit of ['deployment_default', 'allowed']) test(`opaque token rotation preserves the ${permit} grant draft`, async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page)
  await grantDraft(page)
  let release, requested
  const held = new Promise(resolve => { release = resolve })
  const arrived = new Promise(resolve => { requested = resolve })
  await page.route('**/api/v1/system/auth/context', async route => {
    requested(); await held
    await route.fulfill({ json: { principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: grantPermissions }] } } })
  })
  await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().setToken('rotated-opaque-fixture', 3600))
  await arrived
  try {
    // Pending authority must not permit interaction, but it must not destroy
    // the in-memory component that owns the unsubmitted draft either.
    await expect(page.getByTestId('engine-data-authorization')).toHaveCount(1)
    await expect(page.getByTestId('source-grant-confirm')).not.toBeVisible()
    expect(writes).toEqual([])
  } finally { release() }
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  await expect(page.getByTestId('source-grant-expiry')).toContainText('长期有效，直到撤销')
  await expect(page.getByTestId('source-grant-form')).toContainText('Outdoor reader')
  await expect(page.getByTestId('approval-targets')).toContainText('activities')
  expect(writes).toEqual([])
})
for (const change of ['permission', 'membership', 'failure']) test(`token rotation never restores a draft after ${change} changes`, async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  await page.route('**/api/v1/system/auth/context', async route => {
    if (change === 'failure') return route.fulfill({ status: 503, json: { error: 'Unavailable' } })
    await route.fulfill({ json: { principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: change === 'membership' ? '5' : '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: change === 'permission' ? grantPermissions.filter(permission => permission !== 'system.engine_access_grant.create') : grantPermissions }] } } })
  })
  await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().setToken('rotated-restricted-fixture', 3600))
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  await expect(page.getByTestId('approval-targets')).toHaveCount(0)
  expect(writes).toEqual([])
})
test('authorization version change clears the selected snapshot', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page.getByTestId('authorization-workspace-toolbar')).toBeVisible()
  // Resolve the existing store before selecting data. The version mutation is
  // synchronous, and a real document navigation still invalidates this handle.
  const store = await page.evaluateHandle(async () => (await import('/src/store/auth.js')).useAuthStore())
  await selectData(page)
  await store.evaluate(store => {
    store.authContext = { ...store.authContext, authorization: { ...store.authContext.authorization, authorization_version: '2' } }
  })
  await expect(page.getByTestId('approval-targets')).toHaveCount(0)
  await expect(page.getByTestId('source-grant-form')).toHaveCount(0)
  expect(await store.evaluate(store => store.authContext.authorization.authorization_version)).toBe('2')
  expect(writes).toEqual([])
  await store.dispose()
})
test('rotation cancels an open grant confirmation without submitting or losing its draft', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  await page.getByTestId('source-grant-confirm').click()
  const confirmation = page.getByRole('dialog').filter({ hasText: '即将授予 Outdoor reader' })
  await expect(confirmation).toBeVisible()
  await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().setToken('rotate-during-confirmation', 3600))
  await expect(confirmation).toBeHidden()
  await expect(page.getByTestId('source-grant-reason')).toHaveValue('Explicit read permission')
  expect(writes).toEqual([])
})
test('new grants select Meta identities without native catalog permission', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: grantPermissions.filter(permission => permission !== 'system.engine_catalog.read') })
  await page.goto('/engines/2?tab=data-authorization')
  await page.getByTestId('authorization-new').click()
  await expect(page.getByTestId('approval-resource-input')).toBeVisible()
  await page.getByTestId('approval-resource-input').press('ArrowDown')
  await expect(page.locator('.resource-tree-select__panel')).toBeVisible()
  await expect.poll(() => reads.some(url => /\/data-items\?/.test(url))).toBe(true)
  expect(reads.some(path => path.includes('/catalog/'))).toBe(false); expect(writes).toEqual([])
})
test('conflicting permission observation retains the original command and does not fall back', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, conflict: true })
  await page.goto('/engines/2?tab=data-authorization')
  await selectData(page); await grantDraft(page); await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('不能覆盖')
  expect(writes).toHaveLength(1); expect(writes[0].body.direct_permission_source).toBe('deployment_default')
})
test('cancelled confirmation does not write; unknown result requires an explicit same-command retry', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', grantFailure: true })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  await confirmGrant(page, true); expect(writes).toEqual([])
  await confirmGrant(page)
  await expect(page.getByTestId('source-grant-outcomes')).toContainText('签发结果未知')
  expect(writes).toHaveLength(1)
  await confirmGrant(page)
  await expect(page.getByTestId('engine-source-grants')).toContainText('已找回原命令的签发历史')
  expect(writes).toHaveLength(2); expect(writes[1].body).toEqual(writes[0].body)
})
test('without authorization permission the route restores to basic details', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read'] })
  await page.goto('/engines/2?tab=data-authorization')
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2`)
  await expect(page.getByTestId('engine-data-authorization')).toHaveCount(0)
  await expect(page.getByTestId('engine-authorization-open')).toHaveCount(0)
  expect(reads).toEqual([]); expect(writes).toEqual([])
})
test('definite duplicate rejection does not keep an automatic original-command retry', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed', grantDuplicate: true, history: [historicalGrant()] })
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
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(panel.getByRole('row').filter({ hasText: '已撤销' })).toHaveCount(2)
  await expect(panel.getByTestId('source-grant-revoke')).toHaveCount(0)
  expect(writes).toHaveLength(1)
})

test('authorization history page two stays selected while loading', async ({ page }) => {
  const history = Array.from({ length: 25 }, (_, index) => ({ ...historicalGrant(), request_id: `page-${index}` }))
  const { reads } = await fixture(page, { allowed: grantPermissions, history })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants')
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(panel).toContainText('page-19')
  await panel.locator('.el-pager').getByText('2', { exact: true }).click()
  await expect(panel).toContainText('page-24')
  await expect(panel).not.toContainText('page-0')
  const historyReads = reads.filter(url => url.includes('/access_grants/history'))
  expect(new URL(historyReads.at(-1)).searchParams.get('page')).toBe('2')
})

test('automatic table and recipient filters cover all pages, persist across list modes and clear independently', async ({ page }) => {
  const history = Array.from({ length: 25 }, (_, index) => ({ ...historicalGrant(), request_id: `filter-${index}`,
    catalog_path: { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: `filter_${index}` }] } }))
  history.push({ ...history[0], request_id: 'other-account', recipient_id: '34' },
    { ...history[0], request_id: 'organization-source', recipient_type: 'department' },
    { ...history[0], request_id: 'revoked-filter', revocation: { revoked_at: '2026-10-08T00:00:00Z' } })
  const { reads, writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read'], history, memberStatus: 'suspended' })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants'), list = panel.getByTestId('source-grant-list')
  const search = panel.getByTestId('source-grant-table-filter')
  await expect(panel.getByTestId('source-grant-filter-query')).toHaveCount(0)
  await expect(panel.getByTestId('source-grant-filter-reset')).toHaveCount(0)
  await expect(panel.locator('.el-pagination')).toContainText('27')
  await search.fill(' filter_24 ')
  await expect(list.getByRole('row').filter({ hasText: 'filter_24' })).toHaveCount(1)
  await expect(panel.locator('.el-pagination')).toContainText('1')
  await search.fill('filter_')
  await panel.getByTestId('source-grant-type-filter').click()
  await page.getByRole('option', { name: '账号', exact: true }).click()
  await panel.getByTestId('source-grant-recipient-filter').locator('.el-select').click()
  await page.getByRole('option').filter({ hasText: 'Outdoor reader' }).click()
  await search.fill('')
  await expect(panel.locator('.el-pagination')).toContainText('25')
  const accountOnly = new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams
  expect(accountOnly.get('recipient_type')).toBe('user')
  expect(accountOnly.get('recipient_id')).toBe('33')
  expect(accountOnly.has('table_search')).toBe(false)
  await search.fill('filter_')
  await expect.poll(() => new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams.get('table_search')).toBe('filter_')
  await expect(panel.locator('.el-pagination')).toContainText('25')
  await expect(list).not.toContainText('Outdoor department')
  await panel.locator('.el-pager').getByText('2', { exact: true }).click()
  await expect(list).toContainText('filter_24')
  // Paging retains the automatically applied conditions.
  let last = new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams
  expect(last.get('table_search')).toBe('filter_')
  expect(last.get('recipient_type')).toBe('user')
  expect(last.get('recipient_id')).toBe('33')
  expect(last.get('page')).toBe('2')
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(panel.locator('.el-pagination')).toContainText('26')
  last = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
  expect(last.get('table_search')).toBe('filter_')
  expect(last.get('recipient_type')).toBe('user')
  expect(last.get('recipient_id')).toBe('33')
  expect(last.get('page')).toBe('1')
  await search.fill('')
  await expect(panel.locator('.el-pagination')).toContainText('26')
  const recipient = panel.getByTestId('source-grant-recipient-filter')
  await recipient.hover()
  await recipient.locator('.el-select__clear').click()
  await expect(panel.locator('.el-pagination')).toContainText('27')
  const type = panel.getByTestId('source-grant-type-filter')
  await type.hover()
  await type.locator('.el-select__clear').click()
  await expect(panel.locator('.el-pagination')).toContainText('28')
  last = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
  expect(last.has('table_search')).toBe(false)
  expect(last.has('recipient_type')).toBe(false)
  expect(last.has('recipient_id')).toBe(false)
  expect(last.get('page')).toBe('1')
  await search.fill('missing-table')
  await expect(panel.locator('.el-pagination')).toContainText('0')
  await expect(list.getByRole('row')).toHaveCount(1)
  await search.hover()
  await search.locator('..').locator('.el-input__clear').click()
  await expect(panel.locator('.el-pagination')).toContainText('28')
  expect(writes).toEqual([])
})

test('organization filters use type and searchable identity without expanding members', async ({ page }) => {
  const history = ['user', 'department', 'project_group'].map(recipient_type => ({ ...historicalGrant(), recipient_type, request_id: `${recipient_type}-grant` }))
  history.push({ ...historicalGrant(), recipient_type: 'department', recipient_id: '34', request_id: 'other-department' },
    { ...history[1], request_id: 'department-revoked', revocation: { revoked_at: '2026-10-08T00:00:00Z' } })
  const { reads, writes } = await fixture(page, { allowed: [...grantPermissions, 'iam.department.read', 'iam.project_group.read'], history })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants'), list = panel.getByTestId('source-grant-list')
  const type = panel.getByTestId('source-grant-type-filter')
  for (const [label, kind, name, code] of [['部门', 'department', 'Outdoor department', 'outdoor_dept'], ['项目组', 'project_group', 'Outdoor team', 'outdoor_stat']]) {
    await type.click()
    await page.getByRole('option', { name: label, exact: true }).click()
    await expect(panel.locator('.el-pagination')).toContainText(kind === 'department' ? '2' : '1')
    let last = new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams
    expect(last.get('recipient_type')).toBe(kind)
    expect(last.has('recipient_id')).toBe(false)
    const recipient = panel.getByTestId('source-grant-recipient-filter')
    await recipient.click()
    await recipient.getByRole('combobox').fill(code)
    await page.getByRole('option', { name: `${name} · ${code}`, exact: true }).click()
    await expect.poll(() => new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams.get('recipient_id')).toBe('33')
    await panel.getByTestId('source-grant-table-filter').fill('activities')
    await expect.poll(() => new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams.get('table_search')).toBe('activities')
    await expect(list.getByRole('row').filter({ hasText: name })).toHaveCount(1)
    await expect(list).not.toContainText('Outdoor reader')
    last = new URL(reads.filter(url => /\/access_grants(?:\?|$)/.test(url)).at(-1)).searchParams
    expect(last.get('recipient_type')).toBe(kind)
    expect(last.get('recipient_id')).toBe('33')
    expect(last.get('table_search')).toBe('activities')
  }
  await type.click()
  await page.getByRole('option', { name: '部门', exact: true }).click()
  await panel.getByTestId('source-grant-recipient-filter').click()
  await page.getByRole('option', { name: 'Outdoor department · outdoor_dept', exact: true }).click()
  await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
  await expect(panel.locator('.el-pagination')).toContainText('2')
  await expect(list).toContainText('已撤销')
  const last = new URL(reads.filter(url => url.includes('/access_grants/history')).at(-1)).searchParams
  expect(last.get('recipient_type')).toBe('department')
  expect(last.get('recipient_id')).toBe('33')
  await panel.getByTestId('source-grant-table-filter').fill('')
  await type.hover()
  await type.locator('.el-select__clear').click()
  await expect(panel.locator('.el-pagination')).toContainText('5')
  expect(writes).toEqual([])
})

test('organization filter respects independent identity-read permission', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, history: [{ ...historicalGrant(), recipient_type: 'department' }] })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants')
  await panel.getByTestId('source-grant-type-filter').click()
  await page.getByRole('option', { name: '部门', exact: true }).click()
  await expect(panel.getByTestId('source-grant-recipient-filter').locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
  await expect(panel).toContainText('无接收方名称查看权限')
  await expect(panel.getByTestId('source-grant-list')).toContainText('部门')
  expect(reads.some(url => url.includes('/tenant/departments'))).toBe(false)
  expect(writes).toEqual([])
})

test('automatic text filtering debounces typing and isolates old responses before the next query', async ({ page }) => {
  const clockStart = new Date('2030-01-01T00:00:00Z')
  await page.clock.install({ time: clockStart })
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, history: [historicalGrant()] })
  let release, started
  const blocked = new Promise(resolve => { release = resolve })
  const requested = new Promise(resolve => { started = resolve })
  await page.route('**/access_grants?**', async route => {
    if (new URL(route.request().url()).searchParams.get('table_search') !== 'old') return route.fallback()
    started(); await blocked
    await route.fulfill({ json: { data: [{ ...historicalGrant(), grant_count: 1,
      catalog_path: { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'old_response' }] } }], total: 1 } })
  })
  await page.goto('/engines/2?tab=data-authorization')
  const panel = page.getByTestId('engine-source-grants'), list = panel.getByTestId('source-grant-list')
  const input = panel.getByTestId('source-grant-table-filter')
  await expect(list).toContainText('Outdoor reader')
  await page.clock.pauseAt(new Date(clockStart.getTime() + 60_000))
  try {
    await input.fill('old')
    await page.clock.runFor(300)
    await requested
    await input.fill('missing')
    const finished = page.waitForResponse(response => new URL(response.url()).searchParams.get('table_search') === 'old')
    release(); await finished
    await page.clock.runFor(100)
    await expect(list).not.toContainText('old_response')
    await input.fill('activities')
    await page.clock.runFor(299)
    expect(reads.some(url => new URL(url, browserTestOrigin('system')).searchParams.has('table_search'))).toBe(false)
    await page.clock.runFor(1)
    await expect(list).toContainText('activities')
    expect(reads.filter(url => new URL(url, browserTestOrigin('system')).searchParams.has('table_search'))).toHaveLength(1)
    // Switching views flushes pending text once instead of querying an old filter.
    await input.fill('missing')
    await page.getByTestId('authorization-workspace-tabs').getByText('全部授权记录', { exact: true }).click()
    await expect(panel.locator('.el-pagination')).toContainText('0')
    await page.clock.runFor(400)
    const historyReads = reads.filter(url => url.includes('/access_grants/history'))
    expect(historyReads).toHaveLength(1)
    expect(new URL(historyReads[0]).searchParams.get('table_search')).toBe('missing')
    expect(writes).toEqual([])
  } finally { release() }
})

test('unauthorized recipient type never loads candidates or issues grants', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page)
  await page.getByTestId('source-grant-recipient-type').click()
  await page.getByRole('option', { name: '部门', exact: true }).click()
  await expect(page.getByTestId('source-grant-form')).toContainText('读取此类接收方候选需要')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
  expect(writes).toEqual([])
})

async function startPermissionChange(page) {
  await page.goto('/engines/2?tab=direct-permissions')
  await page.getByPlaceholder('数据项名称或路径', { exact: true }).fill('activities')
  await page.getByTestId('direct-permission-check').first().check()
  await page.getByTestId('direct-permission-review').click()
  const dialog = page.getByRole('dialog', { name: '确认批量许可变更', exact: true })
  await expect(dialog).toBeVisible()
  return dialog
}
async function confirmPermission(page) {
  await page.getByTestId('direct-permission-save').click()
  const confirmation = page.locator('.el-message-box')
  await expect(confirmation).toContainText('不签发读取权限')
  await confirmation.getByRole('button', { name: '确定', exact: true }).click()
}
test('administrator sets explicit permission from the dedicated tab without touching Grants', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permit: 'denied', history: [historicalGrant()] })
  const dialog = await startPermissionChange(page)
  await expect(dialog).toContainText('不影响 Catalog 业务办理')
  await page.getByTestId('direct-permission-value').getByRole('checkbox').check()
  await page.getByTestId('direct-permission-reason').fill('允许经授权办理员直接授权')
  await page.getByTestId('direct-permission-save').click()
  await page.locator('.el-message-box').getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toEqual([])
  await expect(page.getByTestId('direct-permission-reason')).toHaveValue('允许经授权办理员直接授权')
  await confirmPermission(page)
  await expect(dialog).toHaveCount(0)
  await expect(page.getByTestId('direct-permission-list')).toContainText('允许直接授权')
  await page.getByRole('tab', { name: '数据授权', exact: true }).click()
  await expect(page.getByTestId('source-grant-list')).toContainText('已签发')
  expect(writes).toHaveLength(1)
  expect(writes[0].path).toMatch(/\/data-items\/[a-f0-9]{64}\/direct-permission$/)
  expect(writes[0].body).toEqual({ catalog_path: tablePath, expected_boundary_hash: 'b'.repeat(64), allowed: true, expected_source: 'explicit', expected_version: '1', reason: '允许经授权办理员直接授权' })
})
test('permission conflict preserves input until explicit read-only review and reconfirmation', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permit: 'denied', changeConflict: true })
  await startPermissionChange(page)
  await page.getByTestId('direct-permission-value').getByRole('checkbox').check()
  await page.getByTestId('direct-permission-reason').fill('保留原原因')
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toBeDisabled()
  await expect(page.getByTestId('direct-permission-reason')).toHaveValue('保留原原因')
  expect(writes).toHaveLength(1)
  await page.getByRole('button', { name: '刷新并核对当前许可', exact: true }).click()
  await expect(page.getByTestId('direct-permission-save')).toBeEnabled()
  expect(writes).toHaveLength(1)
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  expect(writes).toHaveLength(2)
  expect(writes[1].body).toMatchObject({ expected_version: '2', expected_source: 'explicit', allowed: true, reason: '保留原原因' })
})
test('unknown permission write outcome is observed without silently replaying it', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permit: 'denied', changeUnknown: true })
  const dialog = await startPermissionChange(page)
  await page.getByTestId('direct-permission-value').getByRole('checkbox').check()
  await page.getByTestId('direct-permission-reason').fill('确认未知写入')
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toBeDisabled()
  await page.getByRole('button', { name: '刷新并核对当前许可', exact: true }).click()
  await expect(dialog).toContainText('版本 2')
  expect(writes).toHaveLength(1)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByTestId('direct-permission-list')).toContainText('允许直接授权')
  expect(writes).toHaveLength(1)
})
test('direct-permission readers can list effective defaults but cannot set permission or issue Grants', async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: ['system.engine.read', 'system.engine_access_direct_permission.read'] })
  await page.goto('/engines/2?tab=direct-permissions')
  await page.getByPlaceholder('数据项名称或路径', { exact: true }).fill('activities')
  await expect(page.getByTestId('direct-permission-list')).toContainText('部署默认')
  await expect(page.getByTestId('direct-permission-check')).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '数据授权', exact: true })).toHaveCount(0)
  expect(reads.some(path => path.includes('/catalog/') || path.includes('/authorization-facts'))).toBe(false)
  expect(writes).toEqual([])
})
test('direct-permission tree loads complete pages before filtering and preserves defaults', async ({ page }) => {
  const rows = Array.from({ length: 144 }, (_, index) => ({ engine_id: '2', item_fingerprint: createHash('sha256').update(`item-${index}`).digest('hex'), full_name: `outdoor/item_${index}`, name: `item_${index}`, item_type: 'table', state: index < 23 ? 'allowed' : 'denied', source: index % 2 ? 'explicit' : 'deployment_default', version: '1' }))
  const { reads, writes } = await fixture(page, { allowed: permissions, permitRows: rows })
  await page.goto('/engines/2?tab=direct-permissions')
  await expect(page.getByTestId('direct-permission-count')).toContainText('共 144 个数据项')
  await page.getByTestId('direct-permission-state').click()
  await page.getByRole('option', { name: '允许直接授权', exact: true }).click()
  await page.getByPlaceholder('数据项名称或路径', { exact: true }).fill('item_22')
  await expect(page.getByTestId('direct-permission-list')).toContainText('item_22')
  await expect(page.getByTestId('direct-permission-list')).toContainText('部署默认')
  expect(reads.filter(url => url.includes('/direct-permissions?'))).toHaveLength(2)
  expect(writes).toEqual([])
})

test('selected denied DataItem remains in the batch and blocks signing instead of silently skipping it', async ({ page }) => {
  const second = { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: 'persons' }] }
  const permitRows = [tablePath, second].map((path, index) => ({ engine_id: '2', item_fingerprint: createHash('sha256').update(`2:outdoor.${path.segments.at(-1).name}`).digest('hex'), name: path.segments.at(-1).name, item_type: 'table', full_name: `outdoor.${path.segments.at(-1).name}`, state: index ? 'denied' : 'allowed', source: 'explicit', version: '1' }))
  const { reads, writes } = await fixture(page, { allowed: grantPermissions, secondTable: true, permitRows })
  await page.goto('/engines/2?tab=data-authorization')
  expect(reads.some(url => url.includes('/direct-permission'))).toBe(false)
  await selectData(page, true); await grantDraft(page)
  await expect(page.getByTestId('approval-targets')).toContainText('不允许直接授权')
  await expect(page.getByTestId('source-grant-form')).toContainText('授予读取权限 · 2')
  await expect(page.getByTestId('source-grant-confirm')).toBeDisabled()
  expect(reads.filter(url => url.includes('/direct-permission'))).toHaveLength(2)
  expect(reads.some(url => url.includes('/direct-permissions?'))).toBe(false)
  await page.getByTestId('approval-targets').getByRole('row').filter({ hasText: 'persons' }).getByRole('button', { name: '移除', exact: true }).click()
  await grantDraft(page); await confirmGrant(page)
  expect(writes).toHaveLength(1)
  expect(writes[0].body.catalog_path).toEqual(tablePath)
})
test('permission management denial stays visible and never creates a setting', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: permissions, denied: true })
  await page.goto('/engines/2?tab=direct-permissions')
  await expect(page.getByTestId('direct-permissions')).toContainText('本引擎管理委派不足')
  expect(writes).toEqual([])
})
test('permission draft and confirmation are discarded when the administrator loses qualification', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permit: 'denied' })
  await startPermissionChange(page)
  await page.getByTestId('direct-permission-reason').fill('不能继续提交')
  await page.getByTestId('direct-permission-save').click()
  await expect(page.locator('.el-message-box')).toBeVisible()
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore(), context = JSON.parse(JSON.stringify(store.authContext))
    context.authorization.role_assignments[0].permissions = context.authorization.role_assignments[0].permissions.filter(p => p !== 'system.engine_access_delegation.create')
    store.authContext = context
  })
  await expect(page.locator('.el-message-box')).toBeHidden()
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  await expect(page.getByTestId('direct-permission-check')).toHaveCount(0)
  expect(writes).toEqual([])
})

test('late signing facts cannot reopen confirmation after leaving the authorization workspace', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: grantPermissions, permit: 'allowed' })
  await page.goto('/engines/2?tab=data-authorization'); await selectData(page); await grantDraft(page)
  let release, started
  const held = new Promise(resolve => { release = resolve }), arrived = new Promise(resolve => { started = resolve })
  await page.route('**/authorization-facts', async route => { started(); await held; await route.fallback() })
  try {
    await page.getByTestId('source-grant-confirm').click(); await arrived
    await page.evaluate(async () => (await import('/src/router/index.js')).default.push('/engines'))
    const response = page.waitForResponse(item => item.url().includes('/authorization-facts'))
    release(); await response
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await expect(page.locator('.el-message-box')).toBeHidden()
    expect(writes).toEqual([])
  } finally { release() }
})

test('same-identity rotation preserves permission input but requires a fresh review before submission', async ({ page }) => {
  const { writes, reads } = await fixture(page, { allowed: permitAdminPermissions, permit: 'denied' })
  await startPermissionChange(page)
  await page.getByTestId('direct-permission-value').getByRole('checkbox').check()
  await page.getByTestId('direct-permission-reason').fill('令牌刷新后仍需核对')
  await page.getByTestId('direct-permission-save').click()
  await expect(page.locator('.el-message-box')).toBeVisible()
  let release, started
  const held = new Promise(resolve => { release = resolve }), arrived = new Promise(resolve => { started = resolve })
  await page.route('**/api/v1/system/auth/context', async route => { started(); await held; await route.fallback() })
  try {
    await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().setToken('rotated-permission-fixture', 3600))
    await arrived
    await expect(page.locator('.el-message-box')).toBeHidden()
    await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
    expect(writes).toEqual([])
  } finally { release() }
  await expect(page.getByTestId('direct-permission-reason')).toHaveValue('令牌刷新后仍需核对')
  await expect(page.getByTestId('direct-permission-value').getByRole('checkbox')).toBeChecked()
  await expect(page.getByTestId('direct-permission-save')).toBeDisabled()
  const factsBefore = reads.filter(url => url.includes('/authorization-facts')).length
  await page.getByRole('button', { name: '刷新并核对当前许可', exact: true }).click()
  await expect(page.getByTestId('direct-permission-save')).toBeEnabled()
  expect(reads.filter(url => url.includes('/authorization-facts'))).toHaveLength(factsBefore + 1)
  expect(writes).toEqual([])
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  expect(writes).toHaveLength(1)
  expect(writes[0].body).toMatchObject({ allowed: true, reason: '令牌刷新后仍需核对' })
})


function treePermissionRows(names = ['activities', 'persons']) {
  return names.map(name => ({ engine_id: '2', item_fingerprint: createHash('sha256').update(`2:outdoor.${name}`).digest('hex'),
    full_name: `outdoor/${name}`, name, item_type: 'table', state: 'denied', source: 'explicit', version: '1',
    ancestors: [{ id: '1', name: '', node_type: 'server' }, { id: '2', name: 'outdoor', node_type: 'schema' }] }))
    .sort((a, b) => a.item_fingerprint.localeCompare(b.item_fingerprint))
}
test('permission node selection distinguishes direct children and recursion, including filtered and collapsed items', async ({ page }, testInfo) => {
  const permitRows = treePermissionRows()
  permitRows.find(row => row.name === 'persons').ancestors.push({ id: '3', name: 'nested.with/slash', node_type: 'directory' })
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permitRows, history: [historicalGrant()] })
  await page.goto('/engines/2?tab=direct-permissions')
  const node = page.locator('[data-permission-node="node:2"]').getByTestId('direct-permission-node-check')
  await node.check()
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 1 项')
  await page.getByTestId('direct-permission-recursive').check()
  await node.check()
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 2 项')
  await page.getByPlaceholder('数据项名称或路径', { exact: true }).fill('activities')
  await expect(page.getByTestId('direct-permission-check')).toHaveCount(1)
  await node.uncheck()
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 0 项')
  await node.check()
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 2 项')
  await page.getByTestId('direct-permission-review').click()
  await expect(page.getByTestId('direct-permission-review-list').locator('tbody tr')).toHaveCount(2)
  await page.screenshot({ path: testInfo.outputPath('permission-batch-review.png'), animations: 'disabled' })
  expect(writes).toEqual([])
  await page.getByTestId('direct-permission-reason').fill('核对当前两项，不包含未来新增项')
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  expect(writes).toHaveLength(2)
  expect(writes.map(write => write.path.split('/').at(-2))).toEqual(permitRows.map(row => row.item_fingerprint))
  expect(writes.every(write => write.path.endsWith('/direct-permission') && write.body.allowed === true)).toBe(true)
})

test('permission batch stops on failure, reviews remaining items, and never resubmits successful items', async ({ page }) => {
  const permitRows = treePermissionRows(['activities', 'persons', 'third'])
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permitRows })
  await page.route('**/authorization-facts', route => {
    const fp = new URL(route.request().url()).pathname.split('/').at(-2), row = permitRows.find(item => item.item_fingerprint === fp)
    const native = { ...tablePath, segments: [...schemaPath.segments, { term: 'table', kind: 'table', name: row.name }] }
    return route.fulfill({ json: { schema_version: 'meta.data_item_authorization_facts/v1', engine_id: 2, item_fingerprint: fp,
      item_type: 'table', full_name: `outdoor.${row.name}`, basis_state: 'complete', boundary_hash: 'b'.repeat(64), boundary: { layout: 'single', native_target: native } } })
  })
  let failed = false
  const attempts = []
  await page.route('**/direct-permission', async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    const fp = new URL(route.request().url()).pathname.split('/').at(-2)
    attempts.push(fp)
    if (fp === permitRows[1].item_fingerprint && !failed) {
      failed = true
      return route.fulfill({ status: 409, json: { error: 'Concurrent update; explicit review required', error_code: 'resource_version_conflict' } })
    }
    return route.fallback()
  })
  await page.goto('/engines/2?tab=direct-permissions')
  await page.locator('[data-permission-node="node:2"]').getByTestId('direct-permission-node-check').check()
  await page.getByTestId('direct-permission-review').click()
  await page.getByTestId('direct-permission-reason').fill('部分失败保留成功项')
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toBeDisabled()
  expect(attempts).toEqual(permitRows.slice(0, 2).map(row => row.item_fingerprint))
  await expect(page.getByTestId('direct-permission-review-list')).toContainText('已成功')
  await page.getByRole('button', { name: '刷新并核对当前许可', exact: true }).click()
  await expect(page.getByTestId('direct-permission-save')).toBeEnabled()
  expect(attempts).toHaveLength(2)
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  expect(attempts).toEqual([permitRows[0], permitRows[1], permitRows[1], permitRows[2]].map(row => row.item_fingerprint))
  expect(writes).toHaveLength(3)
})

test('incomplete permission identity pages expose no partial tree or batch action', async ({ page }) => {
  const permitRows = Array.from({ length: 101 }, (_, i) => ({ ...treePermissionRows()[0], item_fingerprint: i.toString(16).padStart(64, '0') }))
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permitRows })
  await page.route('**/direct-permissions?**', route => new URL(route.request().url()).searchParams.get('page') === '2'
    ? route.fulfill({ status: 503, json: { error: 'Second page unavailable' } }) : route.fallback())
  await page.goto('/engines/2?tab=direct-permissions')
  await expect(page.getByTestId('direct-permissions')).toContainText('Second page unavailable')
  await expect(page.getByTestId('direct-permission-check')).toHaveCount(0)
  await expect(page.getByTestId('direct-permission-node-check')).toHaveCount(0)
  await expect(page.getByTestId('direct-permission-review')).toBeDisabled()
  expect(writes).toEqual([])
})

test('permission batch preflight rejects an incomplete DataItem before any write', async ({ page }) => {
  const { writes, setFactsState } = await fixture(page, { allowed: permitAdminPermissions, permit: 'denied', secondTable: true })
  await page.goto('/engines/2?tab=direct-permissions')
  await page.locator('[data-permission-node="node:2"]').getByTestId('direct-permission-node-check').check()
  setFactsState('incomplete')
  await page.getByTestId('direct-permission-review').click()
  await expect(page.getByTestId('direct-permissions').locator('.el-alert--error')).toBeVisible()
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 2 项')
  expect(writes).toEqual([])
})

test('permission node writes only changed items and supports bulk cancellation without materializing unchanged defaults', async ({ page }) => {
  const permitRows = treePermissionRows()
  permitRows[1] = { ...permitRows[1], state: 'allowed', source: 'deployment_default' }
  const { writes } = await fixture(page, { allowed: permitAdminPermissions, permitRows })
  await page.goto('/engines/2?tab=direct-permissions')
  const node = page.locator('[data-permission-node="node:2"]').getByTestId('direct-permission-node-check')
  await node.check()
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 1 项')
  await page.getByTestId('direct-permission-review').click()
  await page.getByTestId('direct-permission-reason').fill('只开启尚未允许的项')
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  expect(writes).toHaveLength(1)
  expect(writes[0].path.split('/').at(-2)).toBe(permitRows[0].item_fingerprint)
  expect(writes[0].body).toMatchObject({ allowed: true, expected_source: 'explicit', expected_version: '1' })
  await node.uncheck()
  await expect(page.getByTestId('direct-permission-count')).toContainText('待变更 2 项')
  await page.getByTestId('direct-permission-review').click()
  await page.getByTestId('direct-permission-reason').fill('明确取消当前两项的直接许可')
  await confirmPermission(page)
  await expect(page.getByTestId('direct-permission-save')).toHaveCount(0)
  expect(writes).toHaveLength(3)
  expect(writes[1].body).toMatchObject({ allowed: false, expected_source: 'explicit', expected_version: '2' })
  expect(writes[2].body).toMatchObject({ allowed: false, expected_source: 'deployment_default', expected_version: '1' })
  expect(writes.every(write => write.path.endsWith('/direct-permission'))).toBe(true)
})
