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

async function setup(page, handler = route => route.fulfill({ json: report() })) {
  const writes = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
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
