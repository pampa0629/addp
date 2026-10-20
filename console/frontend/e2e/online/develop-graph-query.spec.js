import { expect, test } from '@playwright/test'
import { graphCanvasHasThemePixels } from '../graph-theme.fixture.js'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login, selectSearchResult } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

const permissions = ['develop.task.read', 'develop.task.execute', 'develop.data_read.execute',
  'system.execution_authorization.create', 'meta.catalog.read'].sort()
const graphQuery = 'MATCH (a:Person)-[r:Link]->(b:Person) RETURN a,r,b LIMIT 10'
const textValue = "中文' RETURN 1 //"

test('ordinary reader executes native Cypher through the real Develop workbench and Security boundary', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_DEVELOP_GRAPH_EXPECTATIONS'))
  expect(['unmanaged', 'protected']).toContain(expected.phase)
  const errors = [], failed = [], writes = [], executionIDs = []
  let businessStarted = false, anonymousRefresh = 0
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/v1/') && request.headers().authorization) businessStarted = true
    if (!path.startsWith('/api/v1/') || ['GET', 'HEAD'].includes(request.method())) return
    const allowed = request.method() === 'POST' && ['/api/v1/system/refresh', '/api/v1/system/login',
      '/api/v1/system/auth/login', '/api/v1/system/auth/context-selections',
      '/api/v1/develop/query-preflight', '/api/v1/develop/executions'].includes(path)
    // Login owns its authentication requests; business writes are limited to read-query submission.
    if (businessStarted && !allowed && /^\/api\/v1\/(develop|meta|graph|manager|service|transfer)\//.test(path)) writes.push(path)
  })
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, businessStarted)) { anonymousRefresh++; return }
    if (new URL(response.url()).pathname.startsWith('/api/v1/') && response.status() >= 400) failed.push(response.status())
  })
  page.on('pageerror', () => errors.push('uncaught_browser_error'))
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (['warning', 'error'].includes(message.type())) errors.push(message.type())
  })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const engineResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/engines')
  await login(page, required('ADDP_ONLINE_GRAPH_DEVELOP_USERNAME'), required('ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD'), '/develop/sql?action=create')
  const engines = await engineResponse
  const api = await browserAPIFromResponse(engines, required('GATEWAY_URL'), {
    principalID: expected.principal_id, tenantID: expected.tenant_id, permissions: new Set(permissions)
  })
  await api.dispose()
  const options = await json(engines, 'Develop engine options')
  for (const target of expected.targets) expect(options.some(engine => engine.id === target.engine_id && engine.engine_type === expected.engine_type)).toBe(true)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const resultTable = frame.locator('.query-result .result-table.el-table')
  await expect(frame.locator('.toolbar-primary .el-tag')).toHaveText('CYPHER')
  await expect(frame.locator('.toolbar-actions').getByRole('button', { name: /保存查询|Save Query/ })).toHaveCount(0)
  const screenshot = name => page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `develop-graph-${name}-console.png`) })

  async function selectGraph(index) {
    const target = expected.targets[index]
    const engine = options.find(engine => engine.id === target.engine_id)
    await frame.locator('.engine-select').click()
    await frame.getByRole('option', { name: new RegExp(engine.name) }).click()
    if (index === 1 && target.engine_id !== expected.targets[0].engine_id) {
      const dialog = frame.getByRole('dialog', { name: /切换查询引擎|Switch Query Engine/ })
      await expect(dialog).toBeVisible()
      await dialog.getByRole('button', { name: /清空并切换|Clear and Switch/ }).click()
    }
    await expect(frame.locator('.engine-select')).toContainText(engine.name)
    await selectSearchResult(frame.locator('.catalog-panel .resource-tree-picker'), target.names.at(-1), target.names.at(-1))
  }
  async function edit(query) {
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.evaluate(value => navigator.clipboard.writeText(value), query)
    await frame.locator('.monaco-editor').click({ position: { x: 70, y: 20 } })
    await page.keyboard.press('ControlOrMeta+A')
    await page.keyboard.press('ControlOrMeta+V')
  }
  async function execute(query, targetIndex, parameterized = false) {
    const analyzed = page.waitForResponse(response => {
      if (new URL(response.url()).pathname !== '/api/v1/develop/query-preflight' || response.request().method() !== 'POST') return false
      const body = response.request().postDataJSON()
      if (body.query !== query) return false
      if (!parameterized) return true
      const definitions = body.query_parameters || []
      return definitions.length === 2 && definitions[0].name === 'text' && definitions[0].default === textValue &&
        definitions[1].name === 'number' && definitions[1].default === 7
    })
    const created = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/executions' && response.request().method() === 'POST')
    const finished = page.waitForResponse(async response => {
      if (!/^\/api\/v1\/develop\/executions\/[^/]+\/result$/.test(new URL(response.url()).pathname) ||
          response.request().method() !== 'GET' || !response.ok()) return false
      return ['success', 'failed', 'timeout', 'cancelled'].includes((await response.json()).status)
    }, { timeout: 120_000 })
    await frame.locator('.toolbar-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
    if (parameterized) await frame.locator('.el-dialog').filter({ hasText: /执行参数|Execution Parameters/ }).getByRole('button', { name: /^(执行|Execute)$/ }).click()
    const analysis = await json(await analyzed, 'same-query Cypher preflight')
    expect(analysis.allowed).toBe(true)
    expect(analysis.effect).toBe('read')
    expect(analysis.diagnostics.filter(diagnostic => diagnostic.severity === 'error')).toEqual([])
    const response = await created
    const body = response.request().postDataJSON()
    expect(body.content.query_type).toBe('cypher')
    expect(body.content.query).toBe(query)
    const target = expected.targets[targetIndex]
    expect(body.execution_config.engine_id).toBe(target.engine_id)
    const locator = new URL(body.content.target_locator)
    expect(locator.hostname).toBe('engine')
    expect(decodeURIComponent(locator.pathname)).toBe(`/${target.engine_id}/path/${target.names.join('/')}`)
    expect(locator.searchParams.get('type')).toBe('graph')
    if (parameterized) {
      expect(body.content.query_parameters.map(({ name, type, default: value }) => ({ name, type, value }))).toEqual([
        { name: 'text', type: 'string', value: textValue }, { name: 'number', type: 'integer', value: 7 }
      ])
    } else expect(body.content.query_parameters).toEqual([])
    const started = await json(response, 'Develop workbench submission')
    expect(started.execution_id).toMatch(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
    executionIDs.push(started.execution_id)
    const terminal = await finished
    expect(new URL(terminal.url()).pathname).toBe(`/api/v1/develop/executions/${started.execution_id}/result`)
    const execution = await json(terminal, 'same-user query result')
    expect(execution.execution_id).toBe(started.execution_id)
    return execution
  }
  async function verifyGraph(execution, name) {
    expect(execution.status).toBe('success')
    const result = execution.metadata.result
    expect(result.result_kind).toBe('graph')
    expect(result.rows_count).toBe(2)
    expect(result.graph_data.nodes.map(node => node.properties.id).sort()).toEqual(['p1', 'p2', 'p3'])
    expect(result.graph_data.relationships).toHaveLength(2)
    expect(result.graph_data.relationships.every(edge => edge.type === 'Link')).toBe(true)
    await expect(frame.locator('.graph-result-view .graph-canvas canvas')).toBeVisible()
    await expect(frame.locator('.graph-result-view .graph-toolbar')).toContainText('3')
    await expect(frame.locator('.graph-result-view .graph-toolbar')).toContainText('2')
    await screenshot(name)
  }
  await selectGraph(0)
  await edit(graphQuery)
  let phaseEvidence
  if (expected.phase === 'unmanaged') {
    await verifyGraph(await execute(graphQuery, 0), 'graph')
    const themes = ['light', 'dark', 'blue', 'purple']
    for (const [index, label] of ['浅色模式', '深色模式', '蓝色模式', '紫色模式'].entries()) {
      const mode = themes[index]
      await page.getByRole('button', { name: /^当前主题:/ }).click()
      await page.getByRole('menuitem', { name: label, exact: true }).click()
      await expect.poll(() => frame.locator('html').evaluate(element => ({
        dark: element.classList.contains('dark'), blue: element.classList.contains('blue'), purple: element.classList.contains('purple')
      }))).toEqual({ dark: mode !== 'light', blue: mode === 'blue', purple: mode === 'purple' })
      await expect.poll(() => frame.locator('.graph-result-view .graph-canvas').evaluate(graphCanvasHasThemePixels)).toBe(true)
      await screenshot(`theme-${mode}`)
    }
    await frame.locator('.result-actions .el-radio-button').filter({ hasText: /^(表格|Table)$/ }).click()
    await expect(resultTable.locator('.el-table__body-wrapper tr')).toHaveCount(2)
    const tableQuery = 'RETURN $text AS text, $number AS number'
    await edit(tableQuery)
    await frame.locator('.editor-heading-actions').getByRole('button', { name: /查询参数|Query Parameters/ }).click()
    for (const [index, name, type, value] of [[0, 'text', '文本', textValue], [1, 'number', '整数', '7']]) {
      await frame.locator('.parameter-toolbar').getByRole('button', { name: /添加参数|Add Parameter/ }).click()
      const parameter = frame.locator('.parameter-item').nth(index)
      await parameter.locator('.parameter-reference-field input').fill(name)
      await parameter.locator('.el-select').click()
      await frame.getByRole('option', { name: type, exact: true }).click()
      await parameter.locator('.parameter-default-scalar .el-checkbox').click()
      await expect(parameter.getByRole('checkbox')).toBeChecked()
      await parameter.locator('.parameter-default-scalar .el-input__inner').fill(value)
    }
    await frame.getByRole('button', { name: '关闭查询参数面板' }).click()
    const typed = await execute(tableQuery, 0, true)
    expect(typed.status).toBe('success')
    expect(typed.metadata.result.result_kind).toBe('table')
    expect(typed.metadata.result.summary.preview_rows).toEqual([{ text: textValue, number: 7 }])
    await expect(resultTable.locator('.el-table__body-wrapper tr')).toHaveCount(1)
    await expect(resultTable).toContainText(textValue)
    await screenshot('table')
    phaseEvidence = { theme_modes: themes, theme_canvas_pixels: true, typed_parameters: true, table_rows: 1, table_visible: true, graph_table_visible: true }
  } else {
    const denied = await execute(graphQuery, 0)
    expect(denied.status).toBe('failed')
    expect(denied.metadata || {}).not.toHaveProperty('result')
    expect(denied.error_details.code).toBe('develop.query.protection_required')
    expect(denied.error_details.diagnostic).toEqual({ schema_version: 'addp.execution-failure/v3', stage: 'preparation', reason: 'data_protection_required' })
    await expect(frame.locator('.execution-failure')).toContainText('develop.query.protection_required')
    await expect(resultTable).toHaveCount(0)
    await expect(frame.locator('.graph-result-view')).toHaveCount(0)
    await screenshot('protected')
    await selectGraph(1)
    if (expected.targets[1].engine_id !== expected.targets[0].engine_id) await edit(graphQuery)
    await verifyGraph(await execute(graphQuery, 1), 'control')
    phaseEvidence = { protection_required: true, protected_result_absent: true, protection_stage: 'preparation', control_readable: true }
  }
  expect(writes).toEqual([])
  expect(failed).toEqual([])
  expect(errors).toEqual([])
  expect(anonymousRefresh).toBeLessThanOrEqual(1)
  writeFileSync(required('ADDP_ONLINE_DEVELOP_GRAPH_REPORT'), JSON.stringify({
    ...expected, schema_version: 'addp.develop-graph-query-browser/v1', result: 'passed', permissions,
    graph_nodes: 3, graph_relationships: 2, graph_rows: 2, graph_canvas: true, ...phaseEvidence,
    execution_ids: executionIDs, browser_errors: 0, failed_business_responses: 0, unexpected_writes: 0,
    anonymous_refresh_401: anonymousRefresh
  }))
})
