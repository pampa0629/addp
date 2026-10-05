import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login, selectSearchResult } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

function validateDocuments(rows, projected = false, ordered = false) {
  expect(rows).toHaveLength(25)
  const expectedIDs = Array.from({ length: 25 }, (_, index) => String(9007199254740993n + BigInt(index)))
  expect(rows.map(row => row.order_id).sort()).toEqual([...expectedIDs].sort())
  if (ordered) expect(rows.map(row => row.order_id)).toEqual(expectedIDs)
  for (const row of rows) {
    const index = expectedIDs.indexOf(row.order_id)
    expect(row.customer).toEqual({ name: `customer-${index}` })
    if (projected) expect(Object.keys(row).sort()).toEqual(['customer', 'order_id'])
    else expect(row.items).toEqual([{ sku: 'SKU-001', quantity: index + 1 }])
  }
}

test('Elasticsearch scan, document previews and DSL execution converge through Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_ELASTICSEARCH_EXPECTATIONS'))
  const gateway = required('GATEWAY_URL')
  const api = await request.newContext({ baseURL: gateway, extraHTTPHeaders: {
    Authorization: `Bearer ${required('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')}`
  } })
  const apiIdentity = identity(await json(await api.get('/api/v1/system/auth/context'), 'API identity'))
  expect(apiIdentity.principalType).toBe('user')
  expect(apiIdentity.tenantID).toBe(String(expected.tenant_id))
  expect(apiIdentity.principalID).toBe(expected.principal_id)
  await api.dispose()
  const token = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'),
    required('ADDP_ONLINE_TEST_USER_PASSWORD'), `/meta/scan?engine_id=${expected.engine_id}`)
  const browserAPI = await request.newContext({ baseURL: gateway, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  const screenshot = name => page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `elasticsearch-${name}-console.png`) })
  try {
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(browserIdentity).toEqual(apiIdentity)
    const meta = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(meta.locator('.left-panel .el-table__body-wrapper tr').filter({ hasText: 'Hosted Elasticsearch' })).toHaveCount(1)
    await expect(meta.locator('.right-panel .el-table__body-wrapper tr')).toHaveCount(2)
    const submitted = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/meta/scan/run/manual' && response.request().method() === 'POST')
    await meta.getByRole('button', { name: /^(重新扫描引擎|Rescan Engine)$/i }).click()
    const scan = await json(await submitted, 'Meta UI scan')
    await expect.poll(async () => {
      const execution = await json(await browserAPI.get(`/api/v1/meta/executions/${encodeURIComponent(scan.execution_id)}`), 'Meta execution')
      if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error('Meta Elasticsearch scan failed')
      return execution.status
    }, { timeout: 120_000 }).toBe('success')
    await expect(meta.locator('.scan-status')).toContainText(/扫描完成|completed/i)
    const items = await json(await browserAPI.get(`/api/v1/meta/engines/${expected.engine_id}/items`), 'rescanned indices')
    expect(items.map(item => ({ name: item.full_name, id: item.id })).sort((a, b) => a.name.localeCompare(b.name)))
      .toEqual([{ name: 'addp_empty.v1', id: expected.empty_item_id }, { name: 'addp_orders.v1', id: expected.index_item_id }])
    await screenshot('meta')

    const initialPreview = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('locator') === expected.index_locator)
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(expected.index_locator)}`)
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    const initial = await json(await initialPreview, 'Manager orders preview')
    expect(initial.preview_type).toBe('table')
    expect(initial.data.total).toBe(25)
    await expect(manager.locator('.explorer-tree .tree-node.index')).toHaveCount(2)
    const fullPreview = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('locator') === expected.index_locator && new URL(response.url()).searchParams.get('page_size') === '100')
    await manager.locator('.table-preview .el-pagination .el-select').click()
    await manager.getByRole('option', { name: /^100/ }).click()
    const full = await json(await fullPreview, 'Manager full sample')
    validateDocuments(full.data.rows)
    await expect(manager.locator('.table-preview .el-table__body-wrapper tr')).toHaveCount(25)
    await expect(manager.locator('.table-preview')).toContainText('9007199254740993')
    await screenshot('orders')
    const emptyResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('locator') === expected.empty_index_locator)
    await manager.locator('.explorer-tree').getByText('addp_empty.v1', { exact: true }).click()
    const empty = await json(await emptyResponse, 'Manager empty index')
    expect(empty.preview_type).toBe('table')
    expect(empty.data.rows).toEqual([])
    expect(empty.data.total).toBe(0)
    await expect(manager.locator('.table-preview .el-table__body-wrapper tr')).toHaveCount(0)
    await screenshot('empty')

    await page.goto('/develop/sql')
    const develop = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(develop.locator('.engine-select')).toContainText('Hosted Elasticsearch')
    await expect(develop.locator('.toolbar-primary .el-tag')).toHaveText('ES_DSL')
    await selectSearchResult(develop.locator('.catalog-panel .resource-tree-picker'), 'addp_orders.v1', 'addp_orders.v1')
    const query = { query: { term: { tags: 'sample' } }, size: 25, sort: [{ order_id: 'asc' }], _source: ['order_id', 'customer'] }
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.evaluate(text => navigator.clipboard.writeText(text), JSON.stringify(query, null, 2))
    await develop.locator('.monaco-editor').click({ position: { x: 70, y: 20 } })
    await page.keyboard.press('ControlOrMeta+A')
    await page.keyboard.press('ControlOrMeta+V')
    const preflight = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/query-preflight' && response.request().method() === 'POST')
    const created = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/executions' && response.request().method() === 'POST')
    const finished = page.waitForResponse(async response => {
      if (!/^\/api\/v1\/develop\/executions\/[^/]+$/.test(new URL(response.url()).pathname) ||
        response.request().method() !== 'GET' || !response.ok()) return false
      return ['success', 'failed', 'timeout', 'cancelled'].includes((await response.json()).status)
    }, { timeout: 120_000 })
    await develop.getByRole('button', { name: /^(执行|Execute)$/i }).click()
    const analysisResponse = await preflight
    expect(JSON.parse(analysisResponse.request().postDataJSON().query)).toEqual(query)
    const analysis = await json(analysisResponse, 'Develop UI preflight')
    expect(analysis.allowed).toBe(true)
    expect(analysis.diagnostics.filter(diagnostic => diagnostic.severity === 'error')).toEqual([])
    const creation = await created
    const body = creation.request().postDataJSON()
    expect(body.content.query_type).toBe('es_dsl')
    expect(body.content.target_locator).toBe(expected.index_locator)
    expect(JSON.parse(body.content.query)).toEqual(query)
    const execution = await json(creation, 'Develop UI execution')
    const resultResponse = await finished
    expect(new URL(resultResponse.url()).pathname).toBe(`/api/v1/develop/executions/${execution.execution_id}`)
    const completed = await json(resultResponse, 'Develop UI result')
    expect(completed.execution_id).toBe(execution.execution_id)
    expect(completed.status).toBe('success')
    validateDocuments(completed.metadata.result.summary.preview_rows, true, true)
    await expect(develop.locator('.query-result .result-summary')).toContainText('25')
    const resultTable = develop.locator('.query-result .result-table.el-table')
    await expect(resultTable).toHaveCount(1)
    await expect(resultTable).toContainText('9007199254740993')
    await screenshot('query')
    const workflowResponse = page.waitForResponse(response => new URL(response.url()).pathname ===
      `/api/v1/develop/executions/${expected.spark.execution_id}` && response.request().method() === 'GET')
    await page.goto(`/develop/executions/${expected.spark.execution_id}`)
    const workflowExecution = await json(await workflowResponse, 'formal Spark execution')
    expect(workflowExecution.execution_id).toBe(expected.spark.execution_id)
    expect(workflowExecution.status).toBe('success')
    expect(workflowExecution.metadata.result.runtime_execution_id).toBe(expected.spark.runtime_execution_id)
    await expect(develop.locator('.execution-detail-page')).toBeVisible()
    await expect(develop.locator('.execution-detail-page')).toContainText(expected.spark.execution_id)
    const finalResult = develop.locator('.workflow-final-result-json')
    await expect(finalResult).toBeVisible()
    await expect.poll(async () => JSON.parse(await finalResult.innerText())).toEqual(expected.spark.final_result)
    await expect(finalResult).toContainText('9007199254740993')
    await screenshot('workflow')
    writeFileSync(required('ADDP_ONLINE_ELASTICSEARCH_BROWSER_REPORT'), JSON.stringify({
      run_id: required('ADDP_ONLINE_TEST_RUN_ID'), engine_id: expected.engine_id, tenant_id: expected.tenant_id,
      principal_id: browserIdentity.principalID, manager_rows: 25, develop_rows: 25,
      empty_index: true, meta_ui_scan: true, develop_ui_query: true,
      spark_execution_id: expected.spark.execution_id, spark_workflow_result: true
    }))
  } finally {
    await browserAPI.dispose()
  }
})
