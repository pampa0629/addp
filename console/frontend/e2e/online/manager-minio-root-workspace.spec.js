import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { renameSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, login } from './transfer-browser-support.js'

// Login failures must not publish fill arguments or credential screenshots.
test.use({ screenshot: 'off' })

test('MinIO root workspace selects, submits and withdraws metadata and read Grants', async ({ page, browser }) => {
  const env = process.env
  if (env.ADDP_ONLINE_HOSTED !== '1' || env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const binding = JSON.parse(env.ADDP_MINIO_WORKSPACE_BINDING || '{}')
  if (binding.run_id !== env.ADDP_ONLINE_TEST_RUN_ID || !binding.engine_id || !binding.issuer_id || !binding.principal_id || !env.ADDP_MINIO_WORKSPACE_JOURNAL) throw new Error('incomplete workspace binding')
  const base = `/api/v1/system/engines/${binding.engine_id}`
  const root = { version: 'catalog.path/v1', engine_id: Number(binding.engine_id), segments: [{ term: 'service', kind: 'service', name: '' }] }
  const commands = [], checks = [], observations = [], failures = [], revocations = []
  let phase = 'login', loginRequests = 0, api, readerAPI, item, locator, leafFactReads = 0
  const readerContext = await browser.newContext({ baseURL: env.CONSOLE_URL })
  const readerPage = await readerContext.newPage()
  const destination = resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-minio-root-workspace.json')
  const record = result => writeFileSync(destination, JSON.stringify({ schema_version: 'addp.minio-root-workspace/v1', ...binding,
    result, phase, checks, commands, revocations, observations, login_requests: loginRequests, leaf_fact_reads: leafFactReads, failures }))
  for (const current of [page, readerPage]) {
    let businessStarted = false
    current.on('console', message => { if (message.type() === 'error' && !isAnonymousRefreshConsoleError(message, businessStarted)) failures.push('unexpected_console_error') })
    current.on('pageerror', () => failures.push('uncaught_browser_error'))
    current.on('request', request => {
      if (new URL(request.url()).pathname === '/api/v1/system/auth/context' && request.headers().authorization) businessStarted = true
      if (new URL(request.url()).pathname === '/api/v1/system/login' && request.method() === 'POST') loginRequests++
    })
    await current.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  }
  page.on('request', request => {
    const url = new URL(request.url())
    if (url.pathname.startsWith(base + '/data-items/') && url.pathname.endsWith('/authorization-facts')) leafFactReads++
    if (url.pathname === base + '/data-items') expect(JSON.parse(url.searchParams.get('parent') || 'null')).toEqual(root)
  })
  // This is an observation barrier, not a mocked API: preserve the exact request/response.
  await page.route('**/api/v1/system/engines/**', async route => {
    const method = route.request().method(), path = new URL(route.request().url()).pathname
    if (!['POST', 'PUT', 'PATCH', 'DELETE'].includes(method)) return route.continue()
    if (method === 'POST' && [base + '/authorization-nodes/query', base + '/catalog/children'].includes(path)) return route.continue()
    if (method === 'POST' && commands.some(command => path === base + `/access_grants/${command.request_id}/revoke`)) return route.continue()
    if (method !== 'POST' || path !== base + '/access_grants') {
      await route.abort('blockedbyclient');throw new Error('workspace attempted an unrelated authorization write')
    }
    const body = route.request().postDataJSON()
    expect(body.catalog_path).toEqual(root)
    expect(body).toMatchObject({ node_recursive: true, recipient_type: 'user', recipient_id: binding.principal_id, expiry_mode: 'until_revoked' })
    expect(['metadata_read', 'read']).toContain(body.action)
    expect(body.request_id).toMatch(/^[0-9a-f-]{36}$/)
    for (const key of ['item_fingerprint', 'metadata_target', 'boundary_hash', 'file_composition', 'scope']) expect(body[key]).toBeUndefined()
    expect(commands.some(command => command.request_id === body.request_id || command.action === body.action)).toBe(false)
    commands.push(body)
    writeFileSync(env.ADDP_MINIO_WORKSPACE_JOURNAL + '.tmp', JSON.stringify({ run_id: binding.run_id, commands }), { mode: 0o600 })
    renameSync(env.ADDP_MINIO_WORKSPACE_JOURNAL + '.tmp', env.ADDP_MINIO_WORKSPACE_JOURNAL)
    await route.continue()
  })
  try {
    const observed = page.waitForResponse(response => new URL(response.url()).pathname === base + '/access_grants' && response.request().method() === 'GET')
    observed.catch(() => {})
    await login(page, env.ADDP_ONLINE_MANAGER_AUTHORIZER_USERNAME, env.ADDP_ONLINE_MANAGER_AUTHORIZER_PASSWORD,
      `/system/engines/${binding.engine_id}?tab=data-authorization`)
    api = await browserAPIFromResponse(await observed, env.GATEWAY_URL, { principalID: binding.issuer_id, tenantID: binding.tenant_id, permissions: new Set(binding.issuer_permissions) })
    const readerObserved = readerPage.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/auth/context' && Boolean(response.request().headers().authorization))
    readerObserved.catch(() => {})
    await login(readerPage, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    readerAPI = await browserAPIFromResponse(await readerObserved, env.GATEWAY_URL, { principalID: binding.principal_id, tenantID: binding.tenant_id, permissions: new Set(binding.consumer_permissions) })
    const system = page.frameLocator('iframe[data-testid="module-iframe"]')
    const workspace = system.getByTestId('engine-data-authorization')
    await expect(workspace).toBeVisible()
    const query = { recipient_type: 'user', recipient_id: binding.principal_id, page_size: 100 }
    async function currentGrants() {
      const response = await api.get(base + '/access_grants', { params: { recipient_type: 'user', page_size: 100 } });expect(response.status()).toBe(200)
      const rows = await response.json();expect(rows.total).toBe(rows.data.length);return rows.data
    }
    async function visibility(visible, status) {
      const engines = await readerAPI.get('/api/v1/manager/engines');expect(engines.status()).toBe(200)
      expect((await engines.json()).data.some(row => row.id === Number(binding.engine_id))).toBe(visible)
      const tree = await readerAPI.get(`/api/v1/meta/resource-tree/${binding.engine_id}?expand_depth=4`)
      expect(tree.status()).toBe(visible ? 200 : 404)
      if (visible) {
        const items = await readerAPI.get(`/api/v1/meta/engines/${binding.engine_id}/items`);expect(items.status()).toBe(200)
        item = (await items.json()).find(row => row.full_name === binding.full_name);expect(item).toBeTruthy()
        locator = `addp://engine/${binding.engine_id}/path/${binding.full_name}?type=object&item_id=${item.id}`
        const detail = await readerAPI.get(`/api/v1/meta/items/${item.id}`);expect(detail.status()).toBe(200)
        expect(Object.keys((await detail.json()).attributes).sort()).toEqual(['capabilities', 'item', 'schema_version'])
      } else {
        const denied = await tree.json();expect(Object.keys(denied).every(key => ['error', 'error_code'].includes(key))).toBe(true)
        if (item) expect((await readerAPI.get(`/api/v1/meta/items/${item.id}`)).status()).toBe(404)
      }
      const observation = { visible, download_status: status }
      if (status !== null) {
        const download = await readerAPI.get('/api/v1/manager/downloads/file', { params: { locator } });expect(download.status()).toBe(status)
        if (status === 200) {
          const raw = await download.body();writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'minio-workspace-read.zip'), raw)
          const code = `import sys,json,importlib\nsys.path.insert(0,sys.argv[1])\nowner=importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')\nprint(json.dumps(owner.validate_model_bundle(sys.stdin.buffer.read(),json.loads(sys.argv[2]))))`
          observation.bundle = JSON.parse(execFileSync('python3', ['-c', code, resolve(process.cwd(), '../..'), JSON.stringify(binding.members)], { input: raw, timeout: 10000, maxBuffer: 65536 }).toString())
        } else {
          const denied = await download.json();expect(Object.keys(denied).every(key => ['error', 'error_code'].includes(key))).toBe(true)
          expect(denied.error).toBeTruthy();expect(denied.error_code).not.toBe('security_protection_required')
        }
      }
      observations.push(observation)
    }
    async function draft(action) {
      await workspace.getByTestId('authorization-new').click()
      const tree = workspace.getByTestId('approval-resource-tree')
      await tree.getByRole('checkbox').first().locator('..').click()
      const targets = workspace.getByTestId('approval-targets')
      await expect(targets.locator('.el-table__body-wrapper tbody tr')).toHaveCount(1)
      await expect(targets).toContainText('整个引擎（含以后新增的数据项）')
      await expect(targets.getByRole('checkbox')).toBeChecked();await expect(targets.getByRole('checkbox')).toBeDisabled()
      await workspace.getByTestId('source-grant-action').click()
      await system.getByRole('option', { name: action === 'read' ? '读取数据（含元数据）' : '仅查看元数据', exact: true }).click()
      await workspace.locator('.iam-member-select__member').click()
      await system.getByRole('option').filter({ hasText: env.ADDP_ONLINE_TEST_USER_USERNAME }).click()
      await workspace.getByTestId('source-grant-expiry').click()
      await system.getByRole('option', { name: '长期有效，直到撤销', exact: true }).click()
      await workspace.getByTestId('source-grant-reason').fill('Disposable MinIO root workspace acceptance')
      await workspace.getByTestId('source-grant-confirm').click()
      const confirm = system.locator('.el-message-box').filter({ hasText: '即将授予' })
      await expect(confirm).toContainText('节点及后代数据项（含未来新增）');return confirm
    }
    async function revoke(command) {
      const list = workspace.getByTestId('source-grant-list')
      await expect(list.getByTestId('account-grant-expand')).toHaveCount(1)
      await list.getByTestId('account-grant-expand').click()
      const row = workspace.getByTestId('account-grant-sources').getByRole('row').filter({ hasText: env.ADDP_ONLINE_TEST_USER_USERNAME })
      await expect(row).toHaveCount(1)
      await row.getByTestId('source-grant-revoke').click()
      await system.getByTestId('source-grant-revoke-reason').fill('Disposable MinIO root workspace withdrawal')
      const withdrawn = page.waitForResponse(response => new URL(response.url()).pathname === base + `/access_grants/${command.request_id}/revoke`)
      withdrawn.catch(() => {})
      await system.getByTestId('source-grant-revoke-confirm').click();expect((await withdrawn).status()).toBe(200)
      const history = await api.get(base + '/access_grants/history', { params: query });expect(history.status()).toBe(200)
      const rows = await history.json();expect(rows.total).toBe(rows.data.length)
      const saved = rows.data.filter(row => row.request_id === command.request_id);expect(saved).toHaveLength(1)
      expect(saved[0]).toMatchObject({ ...command, engine_id: binding.engine_id, operator_principal_id: binding.issuer_id })
      expect(saved[0].revocation).toBeTruthy();expect(await currentGrants()).toEqual([])
      await expect(list.getByTestId('account-grant-expand')).toHaveCount(0);revocations.push(command.request_id)
    }
    phase = 'initial_visibility';expect(await currentGrants()).toEqual([]);await visibility(false, null)
    phase = 'cancel';const cancelled = await draft('metadata_read');checks.push('root_single_recursive_scope')
    await cancelled.getByRole('button', { name: '取消', exact: true }).click()
    expect(commands).toEqual([]);expect(await currentGrants()).toEqual([])
    await workspace.getByTestId('source-grant-form').getByRole('button', { name: '取消', exact: true }).click()
    checks.push('cancel_without_grant')
    for (const action of ['metadata_read', 'read']) {
      phase = action + '_submit';const confirm = await draft(action)
      const issued = page.waitForResponse(response => new URL(response.url()).pathname === base + '/access_grants' && response.request().method() === 'POST')
      issued.catch(() => {})
      await confirm.getByRole('button', { name: '确定', exact: true }).click()
      const response = await issued;expect(response.status()).toBe(201)
      const command = commands.at(-1), receipt = await response.json()
      expect(receipt).toMatchObject({ ...command, engine_id: binding.engine_id, operator_principal_id: binding.issuer_id, approval_mode: 'independent', revocation: null })
      const current = await currentGrants();expect(current).toHaveLength(1);expect(current[0].request_id).toBe(command.request_id)
      await visibility(true, action === 'read' ? 200 : 403);checks.push(action === 'read' ? 'read_submit_and_bundle' : 'metadata_submit_and_visibility')
      phase = action + '_withdraw';await revoke(command);await visibility(false, 403)
      checks.push(action === 'read' ? 'read_revoke_hides' : 'metadata_revoke_hides')
    }
    expect(commands.map(command => command.action)).toEqual(['metadata_read', 'read'])
    expect(leafFactReads).toBe(0);expect(loginRequests).toBe(2);expect(failures).toEqual([])
    checks.push('same_actors_and_exact_commands');phase = 'completed';record('passed')
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'minio-root-workspace-revoked.png') })
  } catch {
    record('failed');throw new Error(`MinIO root workspace failed at ${phase}; inspect bounded evidence`)
  } finally {
    await api?.dispose();await readerAPI?.dispose();await readerContext.close()
  }
})
