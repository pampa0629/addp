import { test, expect } from '@playwright/test'

const permissions = ['transfer.task.create', 'meta.catalog.read', 'system.engine.read']
function report(status = 'supported') {
  return {
    schema_version: 'addp.transfer_task_config_check/v1',
    tenant_id: 900001, principal_id: '900001', config_hash: 'synthetic-config', binding_hash: 'synthetic-binding',
    observed_at: '2026-10-11T01:00:00Z', status,
    checked: ['request_shape', 'provider_write_definition'],
    not_checked: ['business_rows', 'replace_target_side_effects'],
    runtime_checks_required: ['per_value_encoding', 'database_privileges'], facts: [],
    diagnostics: [{ status, code: { supported: 'json_encoding_supported', unsupported: 'array_element_type_unsupported', unknown: 'json_representation_not_proven' }[status],
      check: 'source_assignment', config_path: '/config/transforms/0/fields/0', source_field: 'payload', target_field: 'payload', physical_type: status === 'unsupported' ? 'TEXT[]' : 'JSONB' }]
  }
}

async function setup(page, handler = route => route.fulfill({ json: report() }), metadata = {}) {
  const writes = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const requestURL = new URL(request.url())
    const path = requestURL.pathname
    if (request.method() === 'GET' && Object.hasOwn(metadata, path)) {
      const response = metadata[path]
      return route.fulfill({ json: typeof response === 'function' ? response(requestURL) : response })
    }
    if (request.method() === 'GET' && new URL(request.url()).pathname === '/api/v1/meta/engines') {
      return route.fulfill({ json: [] })
    }
    writes.push({ method: request.method(), path: new URL(request.url()).pathname, body: request.postDataJSON() })
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/transfer/task-config-checks') return handler(route)
    // No request may fall through to the user's Gateway or business services.
    return route.abort('blockedbyclient')
  })
  await page.goto('/e2e/fixtures/config-check.html')
  await expect(page.getByTestId('task-config-check')).toBeEnabled()
  return writes
}

test('manual check submits the same complete request as the wizard and never saves or runs', async ({ page }) => {
  const writes = await setup(page)
  expect(writes).toEqual([])
  const expected = JSON.parse(await page.getByTestId('fixture-request').textContent())
  await page.getByTestId('task-config-check').click()
  await expect(page.getByTestId('task-config-check-report')).toBeVisible()
  expect(writes).toEqual([{ method: 'POST', path: '/api/v1/transfer/task-config-checks', body: expected }])
  await expect(page.getByTestId('fixture-submits')).toHaveText('0')
  await expect(page.getByTestId('task-submit')).toBeEnabled()
})

for (const [status, title] of [['supported', '已检查定义受支持'], ['unsupported', '发现不支持的定义'], ['unknown', '尚无法确定']]) {
  test(`${status} displays the scope, field location and keeps saving behavior`, async ({ page }) => {
    await setup(page, route => route.fulfill({ json: report(status) }))
    await page.getByTestId('task-config-check').click()
    const panel = page.getByTestId('task-config-check-report')
    await expect(panel).toContainText(title)
    await expect(panel).toContainText('/config/transforms/0/fields/0')
    await expect(panel).toContainText('payload')
    await expect(panel).toContainText('业务行数据')
    await expect(panel).toContainText('逐值编码')
    await expect(page.getByTestId('task-config-check-panel')).toContainText('定义受支持仍需运行时校验')
    await page.getByTestId('task-submit').click()
    await expect(page.getByTestId('fixture-submits')).toHaveText('1')
    await page.getByTestId('english').click()
    await expect(panel).toContainText('Configuration path')
    await expect(panel).toContainText('Runtime checks still required')
  })
}

for (const permission of permissions) {
  test(`missing ${permission} clears the report and blocks a new request`, async ({ page }) => {
    const writes = await setup(page)
    await page.getByTestId('task-config-check').click()
    await expect(page.getByTestId('task-config-check-report')).toBeVisible()
    await page.getByTestId(`revoke-${permission}`).click()
    await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
    await expect(page.getByTestId('task-config-check')).toBeDisabled()
    await expect(page.getByTestId('task-config-check-panel')).toContainText('三项权限')
    expect(writes).toHaveLength(1)
    await expect(page.getByTestId('task-submit')).toBeEnabled()
  })
}

for (const change of ['change-context', 'change-principal']) {
  test(`${change} invalidates the observed report`, async ({ page }) => {
    await setup(page)
    await page.getByTestId('task-config-check').click()
    await expect(page.getByTestId('task-config-check-report')).toBeVisible()
    await page.getByTestId(change).click()
    await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
    await expect(page.getByTestId('task-config-check')).toBeEnabled()
  })
}

test('editing clears a report and late responses cannot replace a newer check', async ({ page }) => {
  let release
  let requests = 0
  const writes = await setup(page, async route => {
    requests++
    if (requests === 2) {
      await new Promise(resolve => { release = resolve })
      return route.fulfill({ json: report('unsupported') })
    }
    return route.fulfill({ json: report() })
  })
  const button = page.getByTestId('task-config-check')
  const panel = page.getByTestId('task-config-check-report')
  await button.click()
  await expect(panel).toBeVisible()
  await page.getByTestId('fixture-name').fill('changed synthetic configuration')
  await expect(panel).toHaveCount(0)
  await button.click()
  await expect.poll(() => requests).toBe(2)
  await page.getByTestId('fixture-name').fill('latest synthetic configuration')
  await button.click()
  await expect(panel).toContainText('已检查定义受支持')
  release()
  await expect(button).toBeEnabled()
  // Wait for the stale HTTP response to arrive before asserting the visible result.
  await expect.poll(() => writes.length).toBe(3)
  await page.waitForLoadState('networkidle')
  await expect(panel).not.toContainText('发现不支持的定义')
  expect(writes[2].body.name).toBe('latest synthetic configuration')
})

test('failed requests and invalid reports are errors rather than unknown conclusions', async ({ page }) => {
  let fail = true
  await setup(page, route => fail ? route.fulfill({ status: 502, json: { error: 'synthetic dependency failure' } }) : route.fulfill({ json: { status: 'supported' } }))
  await page.getByTestId('task-config-check').click()
  await expect(page.getByTestId('task-config-check-error')).toContainText('暂时无法读取')
  await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
  fail = false
  await page.getByTestId('task-config-check').click()
  await expect(page.getByTestId('task-config-check-error')).toContainText('未收到有效')
  await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
})

test('revoking permission while checking discards the in-flight report', async ({ page }) => {
  let release
  await setup(page, async route => {
    await new Promise(resolve => { release = resolve })
    return route.fulfill({ json: report() })
  })
  await page.getByTestId('task-config-check').click()
  await expect.poll(() => Boolean(release)).toBe(true)
  await page.getByTestId('revoke-meta.catalog.read').click()
  release()
  await page.waitForLoadState('networkidle')
  await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
  await expect(page.getByTestId('task-config-check')).toBeDisabled()
})

for (const [status, message] of [[400, '配置格式无效'], [403, '当前身份没有检查权限']]) {
  test(`HTTP ${status} is displayed as a request error`, async ({ page }) => {
    await setup(page, route => route.fulfill({ status, json: { error: 'synthetic rejection' } }))
    await page.getByTestId('task-config-check').click()
    await expect(page.getByTestId('task-config-check-error')).toContainText(message)
    await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
  })
}

test('network failure can be retried without leaving an old report', async ({ page }) => {
  let fail = true
  await setup(page, route => fail ? route.abort('failed') : route.fulfill({ json: report() }))
  await page.getByTestId('task-config-check').click()
  await expect(page.getByTestId('task-config-check-error')).toContainText('配置检查请求失败')
  await expect(page.getByTestId('task-config-check-report')).toHaveCount(0)
  fail = false
  await page.getByTestId('task-config-check').click()
  await expect(page.getByTestId('task-config-check-report')).toBeVisible()
  await expect(page.getByTestId('task-config-check-error')).toHaveCount(0)
})

test('check controls and report remain within the narrow review layout', async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 900 })
  await setup(page, route => route.fulfill({ json: report('unsupported') }))
  await page.getByTestId('task-config-check').click()
  await expect(page.getByTestId('task-config-check-report')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

for (const language of ['mql', 'sql']) {
  test(`returning to source preserves the ${language} definition and parameters while engine facts are unavailable`, async ({ page }) => {
    const writes = await setup(page)
    await page.getByTestId(`query-draft-${language}`).click()
    const original = JSON.parse(await page.getByTestId('fixture-request').textContent())
    expect(original.config.source.query.parameters).toEqual({ needle: 'synthetic-value' })
    await page.getByTestId('source-step').click()
    await expect(page.getByRole('heading', { name: '选择数据源', exact: true })).toBeVisible()
    await expect.poll(async () => JSON.parse(await page.getByTestId('fixture-request').textContent()).config.source.query).toEqual(original.config.source.query)
    await page.getByTestId('review-step').click()
    await page.getByTestId('task-config-check').click()
    await expect(page.getByTestId('task-config-check-report')).toBeVisible()
    expect(writes.filter(request => request.method === 'POST')).toEqual([
      { method: 'POST', path: '/api/v1/transfer/task-config-checks', body: original }
    ])
    await expect(page.getByTestId('fixture-submits')).toHaveText('0')
  })
}

for (const querySupported of [true, false]) {
  test(`restored declared MongoDB query support ${querySupported} is applied without treating loading as unsupported`, async ({ page }) => {
    const locator = 'addp://engine/9000001/path/synthetic/source?type=collection&item_id=90000199'
    const leaf = { id: locator, locator, label: 'source', type: 'collection', hasChildren: false,
      metadata: { item_id: 90000199, full_name: 'synthetic.source', data_type: 'table', representation: 'native' } }
    const branchLocator = 'addp://engine/9000001/path/synthetic?type=database'
    const branch = { id: branchLocator, locator: branchLocator, label: 'synthetic', type: 'database', hasChildren: true, children: [leaf] }
    const rootLocator = 'addp://engine/9000001?type=engine'
    const root = { id: rootLocator, locator: rootLocator, label: 'Synthetic MongoDB', type: 'engine', hasChildren: true, children: [branch] }
    const engine = { id: 9000001, name: 'Synthetic MongoDB', engine_type: 'mongodb', lifecycle_state: 'active', connection_status: 'online',
      capabilities: { engine_family: 'dynamic_schema', storage: { store: { record_read_session: true } },
        compute: { query: { supported: querySupported, read_session: querySupported, languages: ['mql'], default_language: 'mql' } } } }
    const writes = await setup(page, undefined, {
      '/api/v1/meta/engines': [engine],
      '/api/v1/meta/resource-tree/9000001': root,
      '/api/v1/meta/resource-tree/9000001/node': url =>
        [root, branch, leaf].find(node => node.locator === url.searchParams.get('locator')),
      '/api/v1/meta/resource-tree/9000001/ancestors': { ancestors: [root, branch, leaf] },
      '/api/v1/meta/items/90000199/fields': { data: [{ name: '_id', type: 'string', nullable: false }, { name: 'payload', type: 'string', nullable: true }] },
      '/api/v1/transfer/capabilities': { table_formats: [], raw_copy_formats: [] }
    })
    await page.getByTestId('query-draft-mql').click()
    const original = JSON.parse(await page.getByTestId('fixture-request').textContent())
    await page.getByTestId('source-step').click()
    await expect(page.getByText('已选数据项', { exact: true })).toBeVisible()
    await expect.poll(async () => JSON.parse(await page.getByTestId('fixture-request').textContent()).config.source.query)
      .toEqual(querySupported ? original.config.source.query : undefined)
    await page.getByTestId('review-step').click()
    const visible = JSON.parse(await page.getByTestId('fixture-request').textContent())
    expect(visible.config.target).toEqual(original.config.target)
    await page.getByTestId('task-config-check').click()
    await expect(page.getByTestId('task-config-check-report')).toBeVisible()
    expect(writes.filter(request => request.method === 'POST')).toEqual([
      { method: 'POST', path: '/api/v1/transfer/task-config-checks', body: visible }
    ])
    await expect(page.getByTestId('fixture-submits')).toHaveText('0')
  })
}
