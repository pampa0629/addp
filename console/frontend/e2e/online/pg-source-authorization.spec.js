import { expect, test } from '@playwright/test'
import { existsSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { isAnonymousRefreshResponse, login } from './transfer-browser-support.js'

// Login diagnostics may contain fill arguments; only explicitly verified screenshots are public.
test.use({ screenshot: 'off' })

test('PG collection qualification workspace approves, recovers and revokes the exact basic task', async ({ page, context }) => {
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const binding = JSON.parse(process.env.ADDP_PG_BROWSER_BINDING)
  const control = process.env.ADDP_PG_BROWSER_CONTROL
  if (!control || !binding.run_id || !binding.task_id || !binding.task_definition_digest || !binding.expires_at) throw new Error('incomplete collection browser binding')
  const base = `/api/v1/system/engines/${binding.engine_id}`
  const route = base + '/meta_collection_qualifications'
  const checks = [], writes = [], failures = []
  let phase = 'login', loginRequests = 0, approvals = 0, revocations = 0, dropped = 0, command = null, original = null, businessStarted = false
  const destination = resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'pg-collection-browser.json')
  async function checkpoint(kind, extra = {}) {
    const marker = { ...binding, phase: kind }
    const temporary = resolve(control, `ready-${kind}.tmp`)
    writeFileSync(temporary, JSON.stringify({ ...marker, ...extra }))
    renameSync(temporary, resolve(control, `ready-${kind}.json`))
    await expect.poll(() => {
      const file = resolve(control, `release-${kind}.json`)
      return existsSync(file) ? JSON.parse(readFileSync(file, 'utf8')) : null
    }, { timeout: 120_000, intervals: [100] }).toEqual(marker)
  }
  context.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/system/login' && request.method() === 'POST') loginRequests++
    if (path === '/api/v1/system/auth/context' && request.headers().authorization) businessStarted = true
  })
  context.on('response', response => {
    if (new URL(response.url()).pathname.startsWith('/api/v1/') && response.status() >= 400 &&
        !isAnonymousRefreshResponse(response, businessStarted)) failures.push('unexpected_api_failure')
  })
  page.on('pageerror', () => failures.push('uncaught_browser_error'))
  // Pass the UI's real credential and body unchanged. Block any unrelated mutation.
  await page.route('**/api/v1/**', async intercepted => {
    const request = intercepted.request(), path = new URL(request.url()).pathname, method = request.method()
    if (method === 'GET' || (method === 'POST' && ['/api/v1/system/login', '/api/v1/system/refresh',
      '/api/v1/system/auth/context-selections', '/api/v1/system/auth/mfa-verifications'].includes(path))) return intercepted.continue()
    try {
      if (method === 'POST' && path === route) {
        const body = request.postDataJSON()
        if (command === null) {
          command = body
          await checkpoint('command', { command }) // Owner registers exact cleanup identity before send.
        }
        expect(body).toEqual(command)
        approvals++
        expect(approvals).toBeLessThanOrEqual(2)
        writes.push({ method, path })
        if (approvals === 1) {
          const response = await intercepted.fetch({ maxRedirects: 0, maxRetries: 0 })
          expect(response.status()).toBe(201)
          original = await response.json()
          await checkpoint('committed', { qualification: original })
          dropped++
          return intercepted.abort('failed') // Committed response loss, not a synthetic approval.
        }
        return intercepted.continue()
      }
      if (method === 'DELETE' && command && path === route + '/' + command.command_id) {
        expect(request.postDataJSON()).toEqual({ version: original.version, reason: 'Disposable PG browser qualification withdrawal' })
        revocations++
        expect(revocations).toBe(1)
        writes.push({ method, path })
        return intercepted.continue()
      }
      failures.push('unowned_api_mutation')
      return intercepted.abort('blockedbyclient')
    } catch {
      failures.push('approval_transport_binding_failed')
      await intercepted.abort('failed')
    }
  })
  const record = (result, unchanged = false) => writeFileSync(destination, JSON.stringify({ ...binding,
    schema_version: 'addp.pg-collection-browser/v1', result, command_id: command?.command_id, checks,
    login_requests: loginRequests, approval_requests: approvals, revocation_requests: revocations,
    dropped_approval_responses: dropped, navigation_writes: writes.filter(row =>
      row.path !== route && row.path !== route + '/' + command?.command_id).length,
    unchanged_identity: unchanged, ...(result === 'failed' ? { phase } : {}) }))
  try {
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    const url = `/system/engines/${binding.engine_id}?tab=collection-qualifications`
    await login(page, process.env.ADDP_ONLINE_PG_OPERATOR_USERNAME, process.env.ADDP_ONLINE_PG_OPERATOR_PASSWORD, url)
    const system = page.frameLocator('iframe[data-testid="module-iframe"]')
    const workspace = system.getByTestId('meta-collection-qualifications')
    await expect(workspace).toBeVisible()
    const moduleFrame = () => page.frames().find(frame => frame.url().includes('/module-ui/system/'))
    const session = await moduleFrame().evaluate(sessionSnapshot, 'system-auth')
    expect(session).toMatchObject({ principal_id: binding.principal_id, tenant_id: binding.tenant_id, persisted_token: false })
    for (const action of ['read', 'approve', 'revoke']) expect(session.permissions).toContain(`system.meta_collection_qualification.${action}`)
    expect(session.permissions).not.toContain('manager.data_item.read')
    const tabs = workspace.getByTestId('collection-workspace-tabs')
    const tab = name => tabs.getByText(name, { exact: true }).click()
    const records = workspace.getByTestId('collection-records')
    const row = () => records.locator('.el-table__body-wrapper tbody tr').filter({ hasText: binding.purpose })
    async function read(action) {
      const pending = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === route)
      await action(); const response = await pending; expect(response.status()).toBe(200); return response.json()
    }
    phase = 'navigation'
    await expect(records).toBeVisible()
    await tab('新增批准'); await expect(records).toHaveCount(0)
    await read(() => tab('资格记录'))
    await read(() => workspace.getByTestId('collection-refresh').click())
    expect(writes).toEqual([])
    checks.push('collection_workspace_read_only_navigation')
    phase = 'inspect'
    await tab('新增批准')
    await expect(workspace.getByTestId('collection-approve')).toBeDisabled()
    await workspace.getByTestId('collection-task').fill(binding.task_id)
    const inspected = page.waitForResponse(response => new URL(response.url()).pathname === base + '/meta_collection_task_facts/' + binding.task_id)
    await workspace.getByTestId('collection-inspect').click()
    const factsResponse = await inspected
    expect(factsResponse.status()).toBe(200)
    expect(await factsResponse.json()).toMatchObject({ task_id: binding.task_id, engine_id: binding.engine_id,
      task_definition_digest: binding.task_definition_digest, scope_kind: 'engine', scan_depth: 'basic', force: false, read_options: {} })
    await expect(workspace.getByTestId('collection-facts')).toContainText('整个引擎')
    await expect(workspace.getByTestId('collection-facts')).toContainText('基础识别（basic）')
    expect(writes).toEqual([])
    checks.push('collection_browser_exact_task_facts')
    phase = 'approve_and_recover'
    await workspace.getByTestId('collection-purpose').fill(binding.purpose)
    const date = new Date(binding.expires_at), pad = value => String(value).padStart(2, '0')
    const local = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
    const input = workspace.locator('.el-date-editor input')
    await input.fill(local); await input.press('Enter')
    await workspace.getByTestId('collection-approve').click()
    await expect(workspace.getByTestId('collection-retry')).toBeVisible()
    await expect(workspace.getByTestId('collection-task')).toBeDisabled()
    await read(() => tab('资格记录'))
    await expect(row()).toHaveCount(1)
    await tab('新增批准')
    expect(approvals).toBe(1)
    const recovered = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === route)
    await workspace.getByTestId('collection-retry').click()
    const restored = await recovered
    expect(restored.status()).toBe(201)
    // The owner has the first committed response and compares every field and exact nanoseconds.
    const recoveredRow = await restored.json()
    for (const key of ['id', 'task_id', 'engine_id', 'task_definition_digest', 'version', 'status', 'purpose']) expect(recoveredRow[key]).toEqual(original[key])
    await expect(row()).toHaveCount(1)
    await expect(row()).toContainText('期限内、未撤销')
    checks.push('collection_browser_same_command_recovery')
    await checkpoint('approved') // Owner verifies repeat success and configuration-change denial.
    phase = 'reload_and_cancel'
    await page.reload()
    await expect(row()).toContainText('期限内、未撤销')
    await read(() => workspace.getByTestId('collection-refresh').click())
    expect(approvals).toBe(2)
    await row().getByTestId('collection-revoke').click()
    const revoke = workspace.getByTestId('collection-revocation')
    await revoke.getByTestId('collection-revoke-reason').fill('Cancelled draft must never write')
    await revoke.getByRole('button', { name: '取消', exact: true }).click()
    await expect(revoke).toHaveCount(0)
    expect(revocations).toBe(0)
    checks.push('collection_browser_record_reload')
    phase = 'revoke'
    await row().getByTestId('collection-revoke').click()
    await revoke.getByTestId('collection-revoke-reason').fill('Disposable PG browser qualification withdrawal')
    const revokedResponse = page.waitForResponse(response => response.request().method() === 'DELETE' && new URL(response.url()).pathname === route + '/' + command.command_id)
    await revoke.getByTestId('collection-revoke-confirm').click()
    expect((await revokedResponse).status()).toBe(200)
    await expect(row()).toContainText('已撤销')
    await expect(row().getByTestId('collection-revoke')).toHaveCount(0)
    await read(() => workspace.getByTestId('collection-refresh').click())
    await expect(row()).toHaveCount(1)
    await expect(row()).toContainText('已撤销')
    checks.push('collection_browser_revoke_and_history')
    await checkpoint('revoked')
    const current = await moduleFrame().evaluate(sessionSnapshot, 'system-auth')
    expect({ ...current, fingerprint: null }).toEqual({ ...session, fingerprint: null })
    expect(writes).toEqual([{ method: 'POST', path: route }, { method: 'POST', path: route }, { method: 'DELETE', path: route + '/' + command.command_id }])
    expect(failures).toEqual([])
    expect(loginRequests).toBe(1)
    record('passed', true)
  } catch {
    record('failed')
    throw new Error(`PG collection qualification workspace failed at ${phase}; inspect bounded evidence`)
  }
})

test('PG authorization workspace filters and revokes the exact Grant through real System and Manager pages', async ({ page, browser }) => {
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const binding = JSON.parse(process.env.ADDP_PG_BROWSER_BINDING)
  if (!binding.run_id || !binding.grant_id || !binding.issuer_id || !binding.engine_id || !binding.locator ||
      binding.direct_permission?.engine_id !== binding.engine_id || binding.direct_permission?.state !== 'allowed' ||
      !binding.direct_permission?.item_fingerprint || !binding.permission_target?.boundary_hash ||
      String(binding.permission_target?.catalog_path?.engine_id) !== binding.engine_id) throw new Error('incomplete workspace binding')
  const destination = resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'pg-authorization-workspace.json')
  const base = `/api/v1/system/engines/${binding.engine_id}`
  const checks = [], previewStatuses = [], directPermissionObservations = [], writes = [], failures = []
  const permitRoute = base + `/data-items/${binding.direct_permission.item_fingerprint}/direct-permission`
  let directPermissionEdit = null
  let phase = 'login', loginRequests = 0
  const readerContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL })
  const readerPage = await readerContext.newPage()
  const competingContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL })
  const competingPage = await competingContext.newPage()
  for (const current of [page, readerPage, competingPage]) {
    current.on('pageerror', () => failures.push('uncaught_browser_error'))
    current.on('request', request => {
      const path = new URL(request.url()).pathname
      if (path === '/api/v1/system/login' && request.method() === 'POST') loginRequests++
      if (isEngineAuthorizationWrite(request.method(), path, base)) writes.push(path)
    })
  }
  const record = result => writeFileSync(destination, JSON.stringify({ ...binding,
    schema_version: 'addp.pg-authorization-workspace/v1', result, checks,
    login_requests: loginRequests, preview_statuses: previewStatuses, direct_permission_observations: directPermissionObservations,
    direct_permission_edit: directPermissionEdit,
    ...(result === 'failed' ? { phase } : {}) }))
  const matchesPreview = response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === binding.locator
  }
  try {
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await readerPage.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await login(page, process.env.ADDP_ONLINE_PG_OPERATOR_USERNAME, process.env.ADDP_ONLINE_PG_OPERATOR_PASSWORD,
      `/system/engines/${binding.engine_id}?tab=data-authorization`)
    const system = page.frameLocator('iframe[data-testid="module-iframe"]')
    const workspace = system.getByTestId('engine-data-authorization')
    await expect(workspace).toBeVisible()
    const frame = page.frames().find(frame => frame.url().includes('/module-ui/system/'))
    const authorizer = await frame.evaluate(sessionSnapshot, 'system-auth')
    expect(authorizer).toMatchObject({ principal_id: binding.issuer_id, tenant_id: binding.tenant_id, persisted_token: false })
    expect(authorizer.permissions).toContain('system.engine_access_grant.revoke')
    expect(authorizer.permissions).toContain('system.engine_access_direct_permission.read')
    expect(authorizer.permissions).not.toContain('manager.data_item.read')
    const tabs = workspace.getByTestId('authorization-workspace-tabs')
    const list = system.getByTestId('source-grant-list')
    async function tab(label) {
      await tabs.getByText(label, { exact: true }).click()
      await expect(tabs.getByRole('radio', { checked: true })).toHaveCount(1)
      await expect(tabs.getByRole('radio', { name: label, exact: true })).toBeChecked()
    }
    async function listing(action, expected, history = false) {
      const pending = page.waitForResponse(response => {
        const url = new URL(response.url())
        return response.request().method() === 'GET' && url.pathname === base + '/access_grants' + (history ? '/history' : '') &&
          Object.entries(expected).every(([key, value]) => typeof value === 'function' ? value(url.searchParams.get(key)) :
            value === null ? !url.searchParams.has(key) : url.searchParams.get(key) === value)
      })
      await action()
      const response = await pending
      expect(response.status()).toBe(200)
      return response.json()
    }
    async function option(testID, label) {
      await system.getByTestId(testID).click()
      await system.getByRole('option', { name: label, exact: true }).click()
    }
    phase = 'direct_permission_filters'
    const permissions = system.getByTestId('direct-permissions')
    const permissionRows = permissions.getByTestId('direct-permission-list').locator('[data-item-fingerprint]')
    const treeLoaded = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'GET' && url.pathname === base + '/direct-permissions' &&
        url.searchParams.get('page') === '1' && url.searchParams.get('page_size') === '100'
    })
    await system.getByRole('tab', { name: '直接授权许可', exact: true }).click()
    const treeResponse = await treeLoaded
    expect(treeResponse.status()).toBe(200)
    const treeResult = await treeResponse.json()
    // This isolated fixture fits one page; the product refuses incomplete trees.
    expect(treeResult.data).toHaveLength(treeResult.total)
    await expect(permissions).toBeVisible()
    await expect(workspace).toHaveCount(0)
    await permissions.getByPlaceholder('数据项名称或路径', { exact: true }).fill('metric_people')
    for (const [state, label, total] of [['', null, 1], ['allowed', '允许直接授权', 1], ['denied', '不允许直接授权', 0]]) {
      if (label) await option('direct-permission-state', label)
      const matches = treeResult.data.filter(row => row.name === 'metric_people' && (!state || row.state === state))
      expect(matches).toHaveLength(total)
      await expect(permissionRows).toHaveCount(total)
      if (total) {
        expect(matches[0]).toMatchObject({ ...binding.direct_permission, name: 'metric_people', item_type: 'table' })
        expect(matches[0].ancestors.length).toBeGreaterThan(0)
        await expect(permissionRows).toContainText('metric_people')
        await expect(permissionRows).toContainText('允许直接授权')
        await expect(permissionRows).toContainText(binding.direct_permission.source === 'explicit' ? '显式设置' : '部署默认')
      }
      directPermissionObservations.push({ search: 'metric_people', state, total, item_fingerprints: matches.map(row => row.item_fingerprint) })
      expect(writes).toEqual([])
    }
    await system.getByRole('tab', { name: '数据授权', exact: true }).click()
    await expect(workspace).toBeVisible()
    await expect(permissions).toHaveCount(0)
    expect(writes).toEqual([])
    checks.push('direct_permission_tab_read_only_filters')

    phase = 'exclusive_navigation'
    await expect(tabs.getByRole('radio', { name: '当前授权', exact: true })).toBeChecked()
    const boxes = await Promise.all(['新增授权', '当前授权', '全部授权记录'].map(name => tabs.getByText(name, { exact: true }).boundingBox()))
    expect(Math.max(...boxes.map(box => box.y + box.height / 2)) - Math.min(...boxes.map(box => box.y + box.height / 2))).toBeLessThan(3)
    await tab('全部授权记录')
    await expect(list).toBeVisible()
    await tab('新增授权')
    await expect(list).toHaveCount(0)
    await expect(system.getByTestId('source-grant-filters')).toHaveCount(0)
    checks.push('exclusive_workspace_tabs')

    phase = 'create_picker'
    await system.getByTestId('approval-resource-input').fill('metric_people')
    const result = system.locator('.resource-tree-select__panel button.search-result').filter({ hasText: 'metric_people' })
    await expect(result).toHaveCount(1)
    await result.click()
    await system.getByTestId('approval-add-selection').click()
    await expect(system.getByTestId('approval-targets')).toContainText('metric_people')
    await expect(system.getByTestId('source-grant-form')).toBeVisible()
    await expect(system.getByTestId('approval-basis-status')).toContainText('基础识别完整')
    expect(writes).toEqual([])
    checks.push('integrated_create_picker_without_write')
    await tab('当前授权')
    await expect(system.getByTestId('approval-targets')).toHaveCount(0)

    phase = 'table_filters'
    const input = system.getByTestId('source-grant-table-filter')
    let rows = await listing(() => input.fill('metric_people'), { table_search: 'metric_people' })
    expect(rows.total).toBe(1)
    rows = await listing(() => input.fill('definitely_absent_pg_table'), { table_search: 'definitely_absent_pg_table' })
    expect(rows.total).toBe(0)
    await expect(list.locator('.el-table__body-wrapper tbody tr')).toHaveCount(0)
    await listing(() => input.fill(''), { table_search: null })
    await system.locator('.resource-tree-select__toggle').click()
    const panel = system.locator('.resource-tree-select__panel')
    const namespace = panel.getByRole('treeitem', { name: 'public', exact: true })
    await namespace.locator('.el-tree-node__expand-icon').first().click()
    const treeRead = listing(() => panel.getByRole('treeitem', { name: 'metric_people', exact: true }).click(), {
      table_search: null, catalog_path: value => {
        if (!value) return false
        const path = JSON.parse(value)
        return String(path.engine_id) === binding.engine_id && path.segments.map(segment => segment.name).join('/') === '/public/metric_people'
      }
    })
    rows = await treeRead
    expect(rows.total).toBe(1)
    expect(rows.data[0].catalog_path.segments.at(-1).name).toBe('metric_people')
    await expect(system.getByTestId('source-grant-exact-filter')).toBeVisible()
    checks.push('table_text_and_tree_filters')

    phase = 'recipient_filters'
    rows = await listing(() => option('source-grant-type-filter', '项目组'), { recipient_type: 'project_group', recipient_id: null })
    expect(rows.total).toBe(0)
    rows = await listing(() => option('source-grant-type-filter', '账号'), { recipient_type: 'user', recipient_id: null })
    expect(rows.total).toBe(1)
    rows = await listing(async () => {
      await system.getByTestId('source-grant-recipient-filter').locator('.el-select').click()
      await system.getByRole('option').filter({ hasText: process.env.ADDP_ONLINE_TEST_USER_USERNAME }).click()
    }, { recipient_type: 'user', recipient_id: binding.principal_id })
    expect(rows.total).toBe(1)
    expect(rows.data[0].inspection).toMatchObject({ account_id: binding.principal_id, reason: 'grant', rule_covered: true,
      sources: [{ request_id: binding.grant_id, recipient_type: 'user', recipient_id: binding.principal_id }], denies: [] })
    await list.getByTestId('account-grant-expand').click()
    await expect(system.getByTestId('account-grant-sources')).toContainText(process.env.ADDP_ONLINE_TEST_USER_USERNAME)
    checks.push('recipient_type_and_account_filters')

    phase = 'reader_before_revoke'
    let preview = readerPage.waitForResponse(matchesPreview)
    await login(readerPage, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD,
      `/manager/data-explorer?locator=${encodeURIComponent(binding.locator)}`)
    const initial = await preview
    previewStatuses.push(initial.status())
    expect(initial.status()).toBe(200)
    expect((await initial.json()).data.rows.map(row => row.person_id).sort()).toEqual(['A', 'B'])
    const manager = readerPage.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(2)
    const reader = await readerPage.evaluate(sessionSnapshot, 'console-auth')
    expect(reader).toMatchObject({ principal_id: binding.principal_id, tenant_id: binding.tenant_id, persisted_token: false })

    phase = 'direct_permission_edit_baseline'
    const unchangedHistory = (await listing(() => tab('全部授权记录'), { recipient_id: binding.principal_id }, true)).data
    expect(unchangedHistory.some(row => row.request_id === binding.grant_id && !row.revocation)).toBe(true)
    expect(authorizer.permissions).toContain('system.engine_access_direct_permission.update')
    expect(authorizer.permissions).toContain('system.engine_access_delegation.create')
    await system.getByRole('tab', { name: '直接授权许可', exact: true }).click()
    await competingPage.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await login(competingPage, process.env.ADDP_ONLINE_PG_OPERATOR_USERNAME, process.env.ADDP_ONLINE_PG_OPERATOR_PASSWORD,
      `/system/engines/${binding.engine_id}?tab=direct-permissions`)
    const competingSystem = competingPage.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(competingSystem.getByTestId('direct-permissions')).toBeVisible()
    const competingFrame = competingPage.frames().find(frame => frame.url().includes('/module-ui/system/'))
    const competingActor = await competingFrame.evaluate(sessionSnapshot, 'system-auth')
    expect(competingActor).toMatchObject({ principal_id: binding.issuer_id, tenant_id: binding.tenant_id,
      permissions: authorizer.permissions, persisted_token: false })
    const reason = 'Disposable PG browser exact permit transition'
    const version = BigInt(binding.direct_permission.version)
    const permit = (state, offset) => ({ ...binding.direct_permission, state, version: String(version + BigInt(offset)) })
    async function openPermit(editor, surface) {
      const panel = surface.getByTestId('direct-permissions')
      await panel.getByPlaceholder('数据项名称或路径', { exact: true }).fill('metric_people')
      const exactRow = panel.getByTestId('direct-permission-list').locator('[data-item-fingerprint]')
      await expect(exactRow).toHaveCount(1)
      await expect(exactRow).toContainText('metric_people')
      await exactRow.getByTestId('direct-permission-check').uncheck()
      const observed = editor.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === permitRoute)
      await panel.getByTestId('direct-permission-review').click()
      const observation = await observed
      expect(observation.status()).toBe(200)
      expect(await observation.json()).toEqual(permit('allowed', 0))
      const dialog = surface.getByRole('dialog', { name: '确认批量许可变更', exact: true })
      await expect(dialog).toBeVisible()
      await dialog.getByTestId('direct-permission-value').getByRole('checkbox').uncheck()
      await dialog.getByTestId('direct-permission-reason').fill(reason)
      return dialog
    }
    const draft = await openPermit(page, system)
    const competingDraft = await openPermit(competingPage, competingSystem)
    expect(writes).toEqual([])
    phase = 'direct_permission_cancel'
    await draft.getByTestId('direct-permission-save').click()
    await system.locator('.el-message-box').getByRole('button', { name: '取消', exact: true }).click()
    await expect(draft.getByTestId('direct-permission-reason')).toHaveValue(reason)
    expect(writes).toEqual([])
    checks.push('browser_direct_permission_cancel_zero_write')
    const edits = []
    async function submitPermit(editor, surface, dialog, allowed, expectedVersion, status) {
      const responsePromise = editor.waitForResponse(response => response.request().method() === 'PUT' && new URL(response.url()).pathname === permitRoute)
      await dialog.getByTestId('direct-permission-save').click()
      const confirmation = surface.locator('.el-message-box')
      await expect(confirmation).toContainText('不签发读取权限')
      await confirmation.getByRole('button', { name: '确定', exact: true }).click()
      const response = await responsePromise
      const body = response.request().postDataJSON()
      expect(String(body.catalog_path.engine_id)).toBe(binding.engine_id)
      expect({ ...body, catalog_path: { ...body.catalog_path, engine_id: binding.permission_target.catalog_path.engine_id } }).toEqual({
        catalog_path: binding.permission_target.catalog_path, expected_boundary_hash: binding.permission_target.boundary_hash,
        allowed, expected_source: 'explicit', expected_version: String(expectedVersion), reason })
      expect(response.status()).toBe(status)
      edits.push({ expected_version: body.expected_version, allowed: body.allowed, status: response.status() })
      return response.json()
    }
    phase = 'direct_permission_close'
    expect(await submitPermit(competingPage, competingSystem, competingDraft, false, version, 200)).toEqual(permit('denied', 1))
    await expect(competingDraft).toHaveCount(0)
    await expect(competingSystem.getByTestId('direct-permission-list')).toContainText('不允许直接授权')
    preview = readerPage.waitForResponse(matchesPreview)
    await manager.locator('.explorer-tree').getByText('metric_people', { exact: true }).click()
    const whileClosed = await preview
    expect(whileClosed.status()).toBe(200)
    expect((await whileClosed.json()).data.rows.map(row => row.person_id).sort()).toEqual(['A', 'B'])
    expect((await readerPage.evaluate(sessionSnapshot, 'console-auth')).fingerprint).toBe(reader.fingerprint)
    phase = 'direct_permission_stale_conflict'
    const conflict = await submitPermit(page, system, draft, false, version, 409)
    expect(conflict.error_code).toBe('resource_version_conflict')
    await expect(draft.getByTestId('direct-permission-save')).toBeDisabled()
    await expect(draft.getByTestId('direct-permission-reason')).toHaveValue(reason)
    expect(writes).toEqual([permitRoute, permitRoute])
    const reviewed = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === permitRoute)
    await draft.getByRole('button', { name: '刷新并核对当前许可', exact: true }).click()
    expect(await (await reviewed).json()).toEqual(permit('denied', 1))
    await expect(draft.getByTestId('direct-permission-save')).toBeEnabled()
    await expect(draft.getByTestId('direct-permission-reason')).toHaveValue(reason)
    expect(writes).toEqual([permitRoute, permitRoute])
    phase = 'direct_permission_reconfirm'
    await draft.getByTestId('direct-permission-value').getByRole('checkbox').check()
    expect(await submitPermit(page, system, draft, true, version + 1n, 200)).toEqual(permit('allowed', 2))
    await expect(draft).toHaveCount(0)
    await expect(system.getByTestId('direct-permission-list')).toContainText('允许直接授权')
    expect(writes).toEqual([permitRoute, permitRoute, permitRoute])
    checks.push('browser_direct_permission_conflict_requires_review')
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'pg-permit-restored.png') })
    // Restore only the presentation filters lost when the DataAuthorization component unmounted.
    await system.getByRole('tab', { name: '数据授权', exact: true }).click()
    await expect(workspace).toBeVisible()
    await listing(() => system.getByTestId('source-grant-table-filter').fill('metric_people'), { table_search: 'metric_people' })
    await listing(() => option('source-grant-type-filter', '账号'), { recipient_type: 'user' })
    await listing(async () => {
      await system.getByTestId('source-grant-recipient-filter').locator('.el-select').click()
      await system.getByRole('option').filter({ hasText: process.env.ADDP_ONLINE_TEST_USER_USERNAME }).click()
    }, { recipient_id: binding.principal_id })
    rows = await listing(() => tab('全部授权记录'), { recipient_id: binding.principal_id }, true)
    expect(rows.data).toEqual(unchangedHistory)
    await listing(() => tab('当前授权'), { recipient_id: binding.principal_id })
    checks.push('browser_direct_permission_close_preserves_grant')
    directPermissionEdit = { item_fingerprint: binding.direct_permission.item_fingerprint,
      boundary_hash: binding.permission_target.boundary_hash, versions: [0n, 1n, 2n].map(offset => String(version + offset)),
      writes: edits, conflict_code: conflict.error_code, cancelled_writes: 0, review_writes: 0,
      grant_unchanged: true, closed_preview_status: whileClosed.status(), same_reader_credential: true, final_state: 'allowed' }

    phase = 'revoke_and_history'
    await listing(() => tab('全部授权记录'), { recipient_type: 'user', recipient_id: binding.principal_id }, true)
    await expect(list).toContainText(binding.grant_id)
    await expect(system.getByTestId('source-grant-revoke')).toHaveCount(0)
    await listing(() => tab('当前授权'), { recipient_type: 'user', recipient_id: binding.principal_id })
    await list.getByTestId('account-grant-expand').click()
    await system.getByTestId('source-grant-revoke').click()
    await system.getByTestId('source-grant-revoke-reason').fill('Disposable PG browser workspace withdrawal')
    const withdrawn = page.waitForResponse(response => new URL(response.url()).pathname === base + `/access_grants/${binding.grant_id}/revoke`)
    rows = await listing(() => system.getByTestId('source-grant-revoke-confirm').click(), { recipient_id: binding.principal_id })
    expect((await withdrawn).status()).toBe(200)
    expect(rows.total).toBe(0)
    await expect(list.getByTestId('account-grant-expand')).toHaveCount(0)
    await listing(() => tab('全部授权记录'), { recipient_id: binding.principal_id }, true)
    rows = await listing(() => option('source-grant-state-filter', '已撤销'), { state: 'revoked', recipient_id: binding.principal_id }, true)
    expect(rows.data.some(row => row.request_id === binding.grant_id && row.revocation)).toBe(true)
    await expect(list).toContainText(binding.grant_id)
    rows = await listing(() => option('source-grant-state-filter', '有效'), { state: 'active', recipient_id: binding.principal_id }, true)
    expect(rows.total).toBe(0)
    expect(writes).toEqual([permitRoute, permitRoute, permitRoute, base + `/access_grants/${binding.grant_id}/revoke`])
    checks.push('browser_revocation_updates_current_and_history')

    phase = 'reader_after_revoke'
    preview = readerPage.waitForResponse(matchesPreview)
    await manager.locator('.explorer-tree').getByText('metric_people', { exact: true }).click()
    const denied = await preview
    previewStatuses.push(denied.status())
    expect(denied.status()).toBe(403)
    await expect(manager.locator('.preview-panel')).toContainText('无权读取此数据项')
    await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(0)
    expect((await readerPage.evaluate(sessionSnapshot, 'console-auth')).fingerprint).toBe(reader.fingerprint)
    await expect(readerPage).toHaveURL(url => url.pathname === '/manager/data-explorer')
    expect(failures).toEqual([])
    expect(loginRequests).toBe(3)
    checks.push('browser_revocation_denies_manager_read')
    record('passed')
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'pg-workspace-revoked.png') })
  } catch {
    record('failed')
    throw new Error(`PG authorization workspace failed at ${phase}; inspect bounded evidence`)
  } finally {
    await competingContext.close()
    await readerContext.close()
  }
})

function isEngineAuthorizationWrite(method, path, base) {
  return path.startsWith(base + '/') && method !== 'GET' &&
    !(method === 'POST' && path === base + '/catalog/children')
}

async function sessionSnapshot(storeID) {
  const provides = document.querySelector('#app')?.__vue_app__?._context.provides || {}
  const pinia = Reflect.ownKeys(provides).map(key => provides[key])
    .find(value => value?._s instanceof Map && value._s.has(storeID))
  const store = pinia?._s.get(storeID)
  if (!store?.token || !store.authContext) return null
  const auth = store.authContext
  const fingerprint = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',
    new TextEncoder().encode(store.token)))).map(value => value.toString(16).padStart(2, '0')).join('')
  return {
    principal_id: auth.principal.id, tenant_id: auth.context.tenant_id,
    membership_id: auth.context.tenant_membership_id,
    authorization_version: auth.authorization.authorization_version,
    permissions: [...store.permissions].sort(), fingerprint,
    departments: auth.organization.departments.map(row => row.department_id),
    projects: auth.organization.project_groups.map(row => row.project_group_id),
    persisted_token: Boolean(localStorage.getItem('token') || sessionStorage.getItem('token'))
  }
}

test('PG member exits silently renew the same Console and Manager session without retaining denied rows', async ({ page, context }) => {
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const binding = JSON.parse(process.env.ADDP_PG_BROWSER_BINDING)
  const control = process.env.ADDP_PG_BROWSER_CONTROL
  const artifacts = process.env.ADDP_ONLINE_ARTIFACT_DIR
  if (!control || !artifacts || !binding.run_id || !binding.locator) throw new Error('incomplete PG browser binding')
  const destination = resolve(artifacts, 'pg-authorization-browser.json')
  const route = `/manager/data-explorer?locator=${encodeURIComponent(binding.locator)}`
  const expectedPermissions = ['manager.data_item.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read',
    'system.execution_authorization.create', 'transfer.task.create', 'transfer.task.delete', 'transfer.task.execute', 'transfer.task.read']
  const observations = [], checks = [], failedResponses = [], pageErrors = []
  let phase = 'login', businessStarted = false, loginRequests = 0, anonymousRefreshes = 0
  const refreshes = [], staleResponses = []
  const matchesPreview = response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === binding.locator
  }
  context.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/system/login' && request.method() === 'POST') loginRequests++
    if (path === '/api/v1/system/auth/context' && request.headers().authorization) businessStarted = true
  })
  context.on('response', response => {
    const path = new URL(response.url()).pathname
    if (path === '/api/v1/system/refresh') {
      if (isAnonymousRefreshResponse(response, businessStarted)) anonymousRefreshes++
      else refreshes.push({ phase, status: response.status() })
    }
    if (response.status() === 401 && phase !== 'login' && (
      path.startsWith('/api/v1/meta/') || path.startsWith('/api/v1/manager/') ||
      ['/api/v1/system/auth/context', '/api/v1/system/users/me'].includes(path))) {
      staleResponses.push(phase)
      return
    }
    if (matchesPreview(response) && response.status() === 403 && phase === 'project_group') return
    if (response.status() >= 400 && (matchesPreview(response) || path === '/api/v1/system/auth/context' ||
      (path === '/api/v1/system/refresh' && !isAnonymousRefreshResponse(response, businessStarted)))) {
      failedResponses.push({ path, status: response.status() })
    }
  })
  page.on('pageerror', () => pageErrors.push('uncaught_browser_error'))
  const report = result => ({ ...binding, schema_version: 'addp.pg-source-authorization-browser/v1',
    result, checks, observations, login_requests: loginRequests, anonymous_refreshes: anonymousRefreshes })
  try {
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    const initialResponse = page.waitForResponse(response => matchesPreview(response) && response.status() === 200)
    await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, route)
    const initialData = (await (await initialResponse).json()).data
    expect(initialData.columns).toEqual(['person_id'])
    expect(initialData.rows.map(row => row.person_id).sort()).toEqual(['A', 'B'])
    const iframe = page.locator('iframe[data-testid="module-iframe"]')
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    const moduleFrame = () => page.frames().find(frame => frame.url().includes('/module-ui/manager/'))
    await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(2)
    const originalFrame = moduleFrame()
    await originalFrame.evaluate(() => { window.__pgSessionObservation = true })
    const original = await page.evaluate(sessionSnapshot, 'console-auth')
    expect(original).toMatchObject({ principal_id: binding.principal_id, tenant_id: binding.tenant_id,
      permissions: expectedPermissions, persisted_token: false })
    let previous = original
    for (const kind of ['department', 'project_group']) {
      phase = kind
      const refreshCount = refreshes.length
      const beforeCookie = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
      const marker = { ...binding, phase: kind }
      const ready = resolve(control, `ready-${kind}.tmp`)
      writeFileSync(ready, JSON.stringify(marker))
      renameSync(ready, resolve(control, `ready-${kind}.json`))
      await expect.poll(() => {
        const file = resolve(control, `release-${kind}.json`)
        return existsSync(file) ? JSON.parse(readFileSync(file, 'utf8')) : null
      }, { timeout: 120_000, intervals: [100] }).toEqual(marker)

      const status = kind === 'department' ? 200 : 403
      const retried = page.waitForResponse(response => matchesPreview(response) && response.status() === status)
      // Reselect the same real tree item: this performs a fresh read, never a scan or a page reload.
      await manager.locator('.explorer-tree').getByText('metric_people', { exact: true }).click()
      const payload = await (await retried).json()
      await expect(iframe).toBeVisible()
      await expect(page).toHaveURL(url => url.pathname === '/manager/data-explorer' &&
        url.searchParams.get('locator') === binding.locator)
      await expect.poll(async () => {
        const parent = await page.evaluate(sessionSnapshot, 'console-auth')
        const child = await moduleFrame()?.evaluate(sessionSnapshot, 'manager-auth')
        return Boolean(parent && child && parent.fingerprint !== previous.fingerprint &&
          child.fingerprint === parent.fingerprint && parent.authorization_version === child.authorization_version &&
          BigInt(parent.authorization_version) > BigInt(previous.authorization_version))
      }).toBe(true)
      const current = await page.evaluate(sessionSnapshot, 'console-auth')
      const child = await moduleFrame().evaluate(sessionSnapshot, 'manager-auth')
      for (const snapshot of [current, child]) {
        expect(snapshot).toMatchObject({ principal_id: original.principal_id, tenant_id: original.tenant_id,
          membership_id: original.membership_id, permissions: expectedPermissions, persisted_token: false,
          departments: [] })
        expect(snapshot.projects.length).toBe(kind === 'department' ? 1 : 0)
      }
      expect(moduleFrame()).toBe(originalFrame)
      expect(await originalFrame.evaluate(() => window.__pgSessionObservation)).toBe(true)
      expect(refreshes.slice(refreshCount)).toEqual([{ phase: kind, status: 200 }])
      expect(staleResponses.includes(kind)).toBe(true)
      const afterCookie = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
      expect(Boolean(beforeCookie?.httpOnly && afterCookie?.httpOnly && beforeCookie.value !== afterCookie.value)).toBe(true)
      if (status === 200) {
        expect(payload.data.rows.map(row => row.person_id).sort()).toEqual(['A', 'B'])
        await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(2)
        await expect(manager.getByTestId('preview-failure')).toHaveCount(0)
        checks.push('department_exit_silent_refresh')
      } else {
        expect(Object.keys(payload).every(key => ['error', 'error_code'].includes(key))).toBe(true)
        expect(payload.error_code === 'security_protection_required').toBe(false)
        await expect(manager.getByTestId('preview-failure')).toContainText('无权读取此数据项')
        await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(0)
        await expect(manager.locator('.preview-panel')).not.toContainText('person_id')
        checks.push('project_exit_silent_refresh')
      }
      observations.push({ closed_type: kind, refreshes: 1, preview_status: status })
      expect(loginRequests).toBe(1)
      expect(anonymousRefreshes).toBeLessThanOrEqual(1)
      expect(failedResponses).toEqual([])
      expect(pageErrors).toEqual([])
      await page.screenshot({ path: resolve(artifacts, `pg-membership-${kind}.png`) })
      previous = current
    }
    checks.push('same_iframe_without_relogin', 'denied_preview_clears_old_rows')
    writeFileSync(destination, JSON.stringify(report('passed')))
  } catch {
    writeFileSync(destination, JSON.stringify({ ...report('failed'), failed_phase: phase }))
    throw new Error(`PG browser member-exit acceptance failed at ${phase}`)
  }
})
