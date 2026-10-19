import { expect, test } from '@playwright/test'
import { graphBrowserDiagnostic, graphCanvasHasThemePixels } from '../graph-theme.fixture.js'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('ordinary Graph reader browses private FalkorDB data and searches Knowledge Service through Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_GRAPH_BROWSER_EXPECTATIONS'))
  const errors = [], failed = [], writes = [], developmentRequests = []
  let stage = 'login'
  const credentials = new Set(Object.entries(process.env)
    .filter(([key, value]) => /(?:TOKEN|SECRET|PASSWORD|USERNAME)$/.test(key) && value).map(([, value]) => value))
  const diagnosticFile = resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'graph-browser-diagnostics.json')
  const persistDiagnostics = () => writeFileSync(diagnosticFile, JSON.stringify({ schema_version: 'addp.graph-browser-diagnostics/v1', entries: errors }, null, 2))
  const recordDiagnostic = message => {
    errors.push(graphBrowserDiagnostic(message, stage, credentials))
    persistDiagnostics()
  }
  persistDiagnostics()
  const releaseScripts = new Set()
  let businessStarted = false, anonymousRefresh = 0
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    const authorization = request.headers().authorization
    if (authorization) credentials.add(authorization.replace(/^Bearer\s+/i, ''))
    if (/\/@vite\/|\/@id\/|\/src\//.test(path)) developmentRequests.push(path)
    if (path.startsWith('/module-ui/graph/assets/') && path.endsWith('.js')) releaseScripts.add(path)
    if (path.startsWith('/api/v1/') && request.headers().authorization) businessStarted = true
    if (path.startsWith('/api/v1/graph/') && !['GET', 'HEAD'].includes(request.method()) &&
      !(request.method() === 'POST' && path === `/api/v1/graph/graphs/${expected.graph_id}/search`)) writes.push(path)
  })
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, businessStarted)) { anonymousRefresh++; return }
    if (new URL(response.url()).pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failed.push(response.status())
    }
    if (new URL(response.url()).pathname.includes('/assets/') && response.status() >= 400) failed.push(response.status())
  })
  page.on('pageerror', error => recordDiagnostic({ type: () => 'pageerror', text: () => error.message, location: () => ({}) }))
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (['warning', 'error'].includes(message.type())) recordDiagnostic(message)
  })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const snapshotResponse = page.waitForResponse(response => new URL(response.url()).pathname ===
    `/api/v1/graph/graphs/${expected.graph_id}/browse-snapshot`)
  await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'), required('ADDP_ONLINE_TEST_USER_PASSWORD'),
    `/graph/graphs/${expected.graph_id}/browse`)
  stage = 'graph:identity-and-snapshot'
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
  stage = 'graph:reload'
  await page.reload()
  await expect(page).toHaveURL(`/graph/graphs/${expected.graph_id}/browse`)
  await expect(frame.locator('.graph-name')).toHaveText('graph_protection_fixture')
  await expect(frame.locator('.graph-canvas .el-loading-mask')).toHaveCount(0)
  const themes = ['light', 'dark', 'blue', 'purple']
  for (const [index, label] of ['浅色模式', '深色模式', '蓝色模式', '紫色模式'].entries()) {
    const mode = themes[index]
    stage = `theme:${mode}`
    await page.getByRole('button', { name: /^当前主题:/ }).click()
    await page.getByRole('menuitem', { name: label, exact: true }).click()
    await expect.poll(() => frame.locator('html').evaluate(element => ({
      dark: element.classList.contains('dark'), blue: element.classList.contains('blue'), purple: element.classList.contains('purple')
    }))).toEqual({ dark: mode !== 'light', blue: mode === 'blue', purple: mode === 'purple' })
    await expect.poll(() => frame.locator('.graph-canvas').evaluate(graphCanvasHasThemePixels)).toBe(true)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `graph-private-theme-${mode}.png`) })
  }
  stage = 'graph:search'
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

  stage = 'knowledge-service:navigation'
  const ontologyResponse = page.waitForResponse(response => new URL(response.url()).pathname ===
    `/api/v1/graph/kg/${expected.graph_id}/ontology`)
  await page.goto(`/graph/knowledge-service?graph_id=${expected.graph_id}&tab=test`)
  await json(await ontologyResponse, 'Console Knowledge Service ontology')
  await expect(frame.locator('.ks-title')).toHaveText('graph_protection_fixture')
  stage = 'knowledge-service:search'
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
  stage = 'knowledge-service:config'
  await frame.locator('.ks-tabs #tab-config').click()
  await expect(frame.locator('.config-panel').getByRole('switch')).toBeDisabled()
  expect(writes).toEqual([])
  expect(failed).toEqual([])
  expect(errors).toEqual([])
  expect(anonymousRefresh).toBeLessThanOrEqual(1)
  expect(developmentRequests).toEqual([])
  expect(releaseScripts.size).toBeGreaterThan(0)
  writeFileSync(required('ADDP_ONLINE_GRAPH_BROWSER_REPORT'), JSON.stringify({
    ...expected, schema_version: 'addp.graph-falkordb-protection-browser/v2', result: 'passed',
    permissions: ['graph.graph.read'], node_count: 3, relationship_count: 2,
    browse_snapshot: true, canvas_visible: true, native_search: true, knowledge_search: true,
    public_switch_disabled: true, graph_writes: 0, failed_business_responses: 0,
    browser_errors: 0, anonymous_refresh_401: anonymousRefresh,
    release_assets: true, deep_link_reload: true, theme_modes: themes, theme_canvas_pixels: true
  }))
})
