import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const root = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }] }
const schema = { ...root, segments: [...root.segments, { term: 'schema', kind: 'namespace', name: 'outdoor' }] }
const table = name => ({ ...root, segments: [...schema.segments, { term: 'table', kind: 'table', name }] })
const capabilities = { schema_version: 'engine.capabilities/v1', engine_family: 'tabular', storage: {
  catalog: { supported: true, real_time: true }, facts: { supported: true, field_info: true },
  catalog_model: { path_version: 'catalog.path/v1', root_term: 'server', levels: [
    { term: 'schema', kinds: ['namespace'], role: 'branch' }, { term: 'table', kinds: ['table', 'view'], role: 'leaf' }
  ] }
} }
const fields = Array.from({ length: 24 }, (_, i) => ({ name: `column_${i + 1}`, type: i ? 'string' : 'int', native_type: i ? 'text' : 'bigint',
  nullable: i !== 0, primary_key: i === 0, comment: 'PRIVATE_COMMENT', default_expression: 'PRIVATE_DEFAULT' }))

async function fixture(page, { allowed = ['system.engine.read', 'system.engine_catalog.read'], caps = capabilities, language = 'zh-cn', hold = false } = {}) {
  const reads = [], unexpected = [], pending = []
  const state = { status: 200, wrongPath: false, missingNullable: false, listReads: 0, version: 1, displayPoll: false }
  const context = permissions => ({ principal: { id: '31', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '4' },
    authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions }] } })
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'structure-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '31', display_name: 'Structure reader' })
    if (path.endsWith('/auth/context')) return reply(context(allowed))
    if (path.endsWith('/engines')) state.listReads++
    const engine = { id: '2', name: 'Structure PostgreSQL' + (state.displayPoll ? ` ${state.listReads}` : ''), engine_type: 'postgresql', engine_origin: 'general',
      lifecycle_state: 'active', connection_status: 'online', version: state.version, last_check_at: new Date(1793750400000 + state.listReads * 10000).toISOString(), capabilities: caps }
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines')) return reply([engine])
    if (path.endsWith('/engines/2')) return reply(engine)
    if (request.method() === 'POST' && path === '/api/v1/system/engines/2/catalog/children') {
      const body = request.postDataJSON(); reads.push({ path, body })
      const entry = (name, p, role, term, kind) => ({ name, path: p, role, term, kind })
      return reply({ nodes: body.path.segments.length === 0 ? [entry('', root, 'branch', 'server', 'server')] : body.path.segments.length === 1
        ? [entry('outdoor', schema, 'branch', 'schema', 'namespace')]
        : ['ontology_r53_persons_json_check', 'other_table'].map(name => entry(name, table(name), 'leaf', 'table', 'table')) })
    }
    if (request.method() === 'POST' && path === '/api/v1/system/engines/2/catalog/facts') {
      const body = request.postDataJSON(); reads.push({ path, body })
      if (hold) await new Promise(resolve => pending.push(resolve))
      if (state.status !== 200) return reply({ error: 'PRIVATE_BACKEND_ERROR' }, state.status)
      return reply({ path: state.wrongPath ? table('wrong_table') : body.path, kind: 'table',
        table: { fields: body.path.segments.at(-1).name === 'other_table' ? [{ name: 'other_id', type: 'int', native_type: 'integer', nullable: false }]
          : fields.map(f => state.missingNullable ? { ...f, nullable: undefined } : f), row_count: 999 }, samples: ['PRIVATE_SAMPLE'], statistics: { secret: 'PRIVATE_STATISTIC' } })
    }
    unexpected.push({ method: request.method(), path }); return reply({ error: 'Unexpected request' }, 500)
  })
  return { reads, unexpected, pending, state, context }
}
async function openTables(page) {
  await page.goto('/engines/2?tab=structure')
  const tree = page.getByTestId('engine-table-structure').locator('.resource-tree-picker')
  await tree.locator('.el-tree-node__content').filter({ hasText: /^outdoor$/ }).locator('.el-tree-node__expand-icon').click()
  await expect(tree.getByText('ontology_r53_persons_json_check', { exact: true })).toBeVisible()
  return tree
}
const choose = (tree, name = 'ontology_r53_persons_json_check') => tree.getByText(name, { exact: true }).click()

test('table structure is lazy, shows every field and explicit nullable without retaining business attributes', async ({ page }, testInfo) => {
  const { reads, unexpected } = await fixture(page)
  await page.goto('/engines/2'); await expect(page.getByRole('tab', { name: '表结构', exact: true })).toBeVisible()
  expect(reads).toEqual([])
  const tree = await openTables(page); await choose(tree)
  const result = page.getByTestId('structure-fields')
  await expect(result.locator('tbody tr')).toHaveCount(24)
  await expect(result.locator('tbody tr').first()).toContainText('column_1')
  await expect(result.locator('tbody tr').first().locator('td').nth(3)).toHaveText('否')
  await expect(result.locator('tbody tr').nth(1).locator('td').nth(3)).toHaveText('是')
  await expect(result.locator('tbody tr').first().locator('td').nth(4)).toHaveText('是')
  await result.getByText('column_24', { exact: true }).scrollIntoViewIfNeeded()
  await expect(result.getByText('column_24', { exact: true })).toBeVisible()
  await expect(page.getByTestId('engine-table-structure')).not.toContainText('PRIVATE_')
  expect(reads.filter(x => x.path.endsWith('/facts')).map(x => x.body)).toEqual([{ path: table('ontology_r53_persons_json_check') }])
  expect(unexpected).toEqual([])
  await page.screenshot({ path: testInfo.outputPath('table-structure-complete.png'), fullPage: true })
})
for (const [label, options] of [['no permission', { allowed: ['system.engine.read'] }],
  ['dynamic sampled schema', { caps: { ...capabilities, engine_family: 'dynamic_schema' } }],
  ['no field facts', { caps: { ...capabilities, storage: { ...capabilities.storage, facts: { supported: true } } } }]]) {
  test(`table structure is unavailable with ${label} and never queries a catalog`, async ({ page }) => {
    const { reads, unexpected } = await fixture(page, options)
    await page.goto('/engines/2?tab=structure')
    await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2`)
    await expect(page.getByRole('tab', { name: '表结构', exact: true })).toHaveCount(0)
    expect(reads).toEqual([]); expect(unexpected).toEqual([])
  })
}
for (const status of [403, 500]) test(`table structure ${status} clears prior fields without fallback or retry`, async ({ page }) => {
  const f = await fixture(page), tree = await openTables(page); await choose(tree)
  await expect(page.getByTestId('structure-fields')).toBeVisible()
  f.state.status = status; await page.getByTestId('engine-table-structure').getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('structure-error')).toBeVisible()
  await expect(page.getByTestId('structure-fields')).toHaveCount(0)
  await expect(page.getByTestId('structure-error')).not.toContainText('PRIVATE_')
  expect(f.reads.filter(x => x.path.endsWith('/facts'))).toHaveLength(2); expect(f.unexpected).toEqual([])
})
for (const invalid of ['wrongPath', 'missingNullable']) test(`table structure rejects ${invalid} instead of showing partial or wrong fields`, async ({ page }) => {
  const f = await fixture(page); f.state[invalid] = true
  const tree = await openTables(page); await choose(tree)
  await expect(page.getByTestId('structure-error')).toBeVisible()
  await expect(page.getByTestId('structure-fields')).toHaveCount(0); expect(f.unexpected).toEqual([])
})
test('newer table selection wins over a late response for the previous target', async ({ page }) => {
  const f = await fixture(page, { hold: true }), tree = await openTables(page); await choose(tree)
  await expect.poll(() => f.pending.length).toBe(1)
  await choose(tree, 'other_table'); await expect.poll(() => f.pending.length).toBe(2)
  f.pending[1](); await expect(page.getByTestId('structure-fields')).toContainText('other_id')
  const late = page.waitForResponse(response => response.url().endsWith('/catalog/facts') && response.request().postDataJSON().path.segments.at(-1).name === 'ontology_r53_persons_json_check')
  f.pending[0](); await late; await expect(page.getByTestId('structure-fields')).not.toContainText('column_1')
  await expect(page.getByTestId('structure-target')).toHaveText('outdoor / other_table'); expect(f.unexpected).toEqual([])
})
test('leaving a pending structure request never restores its fields', async ({ page }) => {
  const f = await fixture(page, { hold: true }), tree = await openTables(page); await choose(tree)
  await expect.poll(() => f.pending.length).toBe(1)
  await page.getByRole('tab', { name: '基本信息', exact: true }).click()
  const late = page.waitForResponse(response => response.url().endsWith('/catalog/facts'))
  f.pending[0](); await late
  await expect(page.getByTestId('engine-table-structure')).toHaveCount(0); expect(f.unexpected).toEqual([])
})
test('losing catalog permission clears the structure and canonicalizes the route', async ({ page }) => {
  const f = await fixture(page), tree = await openTables(page); await choose(tree)
  await expect(page.getByTestId('structure-fields')).toBeVisible()
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    store.authContext = { ...store.authContext, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['system.engine.read'] }] } }
  })
  await expect(page.getByTestId('engine-table-structure')).toHaveCount(0)
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2`); expect(f.unexpected).toEqual([])
})
test('English table structure fits a narrow viewport', async ({ page }, testInfo) => {
  const f = await fixture(page, { language: 'en' }); await page.setViewportSize({ width: 640, height: 900 })
  const tree = await openTables(page); await choose(tree)
  await expect(page.getByRole('tab', { name: 'Table structure', exact: true })).toBeVisible()
  await expect(page.getByTestId('structure-fields').locator('tbody tr').first().locator('td').nth(3)).toHaveText('No')
  const fits = await page.getByTestId('engine-table-structure').evaluate(el => el.scrollWidth <= el.clientWidth + 1)
  expect(fits).toBe(true); expect(f.unexpected).toEqual([])
  await page.screenshot({ path: testInfo.outputPath('table-structure-en-narrow.png'), fullPage: true })
})

test('equivalent status projections preserve expanded nodes and selected fields without new catalog requests', async ({ page }) => {
  await page.clock.install()
  const f = await fixture(page); f.state.displayPoll = true
  const tree = await openTables(page), initialChildren = f.reads.length
  for (let i = 0; i < 2; i++) {
    const next = f.state.listReads + 1
    await page.clock.fastForward(10001)
    await expect(page.getByRole('heading', { name: `引擎详情 - Structure PostgreSQL ${next}`, exact: true })).toBeVisible()
    await expect(tree.getByText('ontology_r53_persons_json_check', { exact: true })).toBeVisible()
    expect(f.reads).toHaveLength(initialChildren)
  }
  await choose(tree); await expect(page.getByTestId('structure-fields').locator('tbody tr')).toHaveCount(24)
  const afterSelection = f.reads.length
  for (let i = 0; i < 3; i++) {
    const next = f.state.listReads + 1
    await page.clock.fastForward(10001)
    await expect(page.getByRole('heading', { name: `引擎详情 - Structure PostgreSQL ${next}`, exact: true })).toBeVisible()
    await expect(page.getByTestId('structure-fields').locator('tbody tr')).toHaveCount(24)
    await expect(page.getByTestId('structure-target')).toHaveText('outdoor / ontology_r53_persons_json_check')
    expect(f.reads).toHaveLength(afterSelection)
  }
  expect(f.unexpected).toEqual([])
})
test('equivalent authorization projections preserve fields but a new authorization version clears them', async ({ page }) => {
  const f = await fixture(page), tree = await openTables(page); await choose(tree)
  await expect(page.getByTestId('structure-fields')).toBeVisible(); const count = f.reads.length
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    store.authContext = { ...JSON.parse(JSON.stringify(store.authContext)), token: { issued_at: 'new-observation' } }
  })
  await expect(page.getByTestId('structure-fields')).toBeVisible(); expect(f.reads).toHaveLength(count)
  await page.evaluate(async () => {
    const store = (await import('/src/store/auth.js')).useAuthStore()
    store.authContext = { ...store.authContext, authorization: { ...store.authContext.authorization, authorization_version: 'new-version' } }
  })
  await expect(page.getByTestId('structure-fields')).toHaveCount(0)
  await expect(page.getByTestId('structure-target')).toHaveText('')
  expect(f.reads.filter(x => x.path.endsWith('/facts'))).toHaveLength(1); expect(f.unexpected).toEqual([])
})
test('a real engine configuration version change clears the selected structure', async ({ page }) => {
  await page.clock.install()
  const f = await fixture(page); f.state.displayPoll = true
  const tree = await openTables(page); await choose(tree)
  await expect(page.getByTestId('structure-fields')).toBeVisible()
  f.state.version = 2; const next = f.state.listReads + 1
  await page.clock.fastForward(10001)
  await expect(page.getByRole('heading', { name: `引擎详情 - Structure PostgreSQL ${next}`, exact: true })).toBeVisible()
  await expect(page.getByTestId('structure-fields')).toHaveCount(0)
  expect(f.reads.filter(x => x.path.endsWith('/facts'))).toHaveLength(1); expect(f.unexpected).toEqual([])
})
