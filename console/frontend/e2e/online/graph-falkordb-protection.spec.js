import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('ordinary Graph reader browses private FalkorDB data and searches Knowledge Service through Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_GRAPH_BROWSER_EXPECTATIONS'))
  const errors = [], failed = [], writes = []
  let businessStarted = false, anonymousRefresh = 0
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/v1/') && request.headers().authorization) businessStarted = true
    if (path.startsWith('/api/v1/graph/') && !['GET', 'HEAD'].includes(request.method()) &&
      !(request.method() === 'POST' && path === `/api/v1/graph/graphs/${expected.graph_id}/search`)) writes.push(path)
  })
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, businessStarted)) { anonymousRefresh++; return }
    if (new URL(response.url()).pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failed.push(response.status())
    }
  })
  page.on('pageerror', () => errors.push('uncaught_browser_error'))
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (['warning', 'error'].includes(message.type())) errors.push(message.type())
  })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const snapshotResponse = page.waitForResponse(response => new URL(response.url()).pathname ===
    `/api/v1/graph/graphs/${expected.graph_id}/browse-snapshot`)
  await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'), required('ADDP_ONLINE_TEST_USER_PASSWORD'),
    `/graph/graphs/${expected.graph_id}/browse`)
  const response = await snapshotResponse
  const api = await browserAPIFromResponse(response, required('GATEWAY_URL'), {
    principalID: expected.principal_id, tenantID: expected.tenant_id, permissions: new Set(['graph.graph.read'])
  })
  try {
    const graph = await json(await api.get(`/api/v1/graph/graphs/${expected.graph_id}`), 'same private graph binding')
    expect(graph.engine_id).toBe(expected.engine_id)
    expect(graph.is_public).toBe(false)
  } finally {
    await api.dispose()
  }
  const snapshot = await json(response, 'Console native graph snapshot')
  expect(snapshot.stats.node_count).toBe(3)
  expect(snapshot.stats.relationship_count).toBe(2)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.locator('.graph-name')).toHaveText('graph_protection_fixture')
  await expect(frame.locator('.filter-panel')).toContainText('Person')
  await expect(frame.locator('.filter-panel')).toContainText('Link')
  await expect(frame.locator('.graph-canvas canvas')).toBeVisible()
  const search = page.waitForResponse(response => response.request().method() === 'POST' &&
    new URL(response.url()).pathname === `/api/v1/graph/graphs/${expected.graph_id}/search`)
  await frame.locator('.toolbar-center input').fill('Fixture')
  await frame.locator('.toolbar-center input').press('Enter')
  const found = await json(await search, 'Console native full-text search')
  expect(found.nodes.map(node => node.properties.id).sort()).toEqual(['p1', 'p2', 'p3'])
  await expect(frame.locator('.toolbar-left .el-tag--success')).toContainText('3')
  await expect(frame.locator('.graph-canvas canvas')).toBeVisible()
  await expect(frame.locator('.graph-canvas .el-loading-mask')).toHaveCount(0)
  await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'graph-private-browse-console.png') })

  const ontologyResponse = page.waitForResponse(response => new URL(response.url()).pathname ===
    `/api/v1/graph/kg/${expected.graph_id}/ontology`)
  await page.goto(`/graph/knowledge-service?graph_id=${expected.graph_id}&tab=test`)
  await json(await ontologyResponse, 'Console Knowledge Service ontology')
  await expect(frame.locator('.ks-title')).toHaveText('graph_protection_fixture')
  const knowledgeSearch = page.waitForResponse(response => response.request().method() === 'GET' &&
    new URL(response.url()).pathname === `/api/v1/graph/kg/${expected.graph_id}/search`)
  await frame.locator('.test-form input').first().fill('Fixture')
  await frame.locator('.test-form button.el-button--primary').click()
  const entities = await json(await knowledgeSearch, 'Console Knowledge Service search')
  expect(entities.total).toBe(3)
  expect(entities.data.map(entity => entity.properties.id).sort()).toEqual(['p1', 'p2', 'p3'])
  await expect.poll(async () => {
    try { return JSON.parse(await frame.locator('.test-result').innerText()).data.map(entity => entity.properties.name).sort() }
    catch { return [] }
  }).toEqual(['Fixture one', 'Fixture three', 'Fixture two'])
  await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'graph-private-knowledge-console.png') })
  await frame.locator('.ks-tabs #tab-config').click()
  await expect(frame.locator('.config-panel').getByRole('switch')).toBeDisabled()
  expect(writes).toEqual([])
  expect(failed).toEqual([])
  expect(errors).toEqual([])
  expect(anonymousRefresh).toBeLessThanOrEqual(1)
  writeFileSync(required('ADDP_ONLINE_GRAPH_BROWSER_REPORT'), JSON.stringify({
    ...expected, schema_version: 'addp.graph-falkordb-protection-browser/v1', result: 'passed',
    permissions: ['graph.graph.read'], node_count: 3, relationship_count: 2,
    browse_snapshot: true, canvas_visible: true, native_search: true, knowledge_search: true,
    public_switch_disabled: true, graph_writes: 0, failed_business_responses: 0,
    browser_errors: 0, anonymous_refresh_401: anonymousRefresh
  }))
})
