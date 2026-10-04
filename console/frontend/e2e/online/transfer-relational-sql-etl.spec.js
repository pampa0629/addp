import { lineageFieldConnections } from '../../../../common-frontend/graph/src/lineageFields.js'
import { lineageNodeId } from '../../../../common-frontend/graph/src/lineageApi.js'
import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  controlFixture,
  identity,
  json,
  login,
  selectSearchResult,
  waitForExecution
} from './transfer-browser-support.js'

const requiredNames = [
  'ADDP_ONLINE_REPOSITORY',
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_TEST_ENGINE_NAME',
  'ADDP_ONLINE_TEST_ENGINE_ID',
  'ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME',
  'ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE',
  'ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE',
  'ADDP_ONLINE_TRANSFER_FIELD_LINEAGE',
  'GATEWAY_URL'
]

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

async function chooseSelectOption(frame, select, optionText) {
  await select.click()
  const combobox = select.getByRole('combobox')
  await expect(combobox).toHaveAttribute('aria-expanded', 'true')
  const listboxID = await combobox.getAttribute('aria-controls')
  expect(listboxID).toBeTruthy()
  const option = frame.locator(`[id="${listboxID}"]`).getByRole('option', { name: optionText })
  await expect(option).toBeVisible()
  await option.click()
  await expect(combobox).toHaveAttribute('aria-expanded', 'false')
  await expect(select).toContainText(optionText)
}

async function selectOutputField(builder, fieldName) {
  const field = builder.getByTestId(`sql-output-field-${fieldName}`)
  await field.click()
  await expect(field.getByRole('checkbox')).toBeChecked()
}

async function scanEngine(api, engineID) {
  const scan = await json(await api.post('/api/v1/meta/scan/run/manual', {
    data: { engine_id: Number(engineID), scan_depth: 'deep', trigger_type: 'manual', force: true }
  }), 'scan SQL target')
  await expect.poll(async () => {
    const execution = await json(await api.get(`/api/v1/meta/executions/${encodeURIComponent(scan.execution_id)}`), 'read Meta scan')
    if (['failed', 'cancelled', 'timeout'].includes(execution.status)) throw new Error(`Meta scan ended with status ${execution.status}`)
    return execution.status
  }, { timeout: 120_000, intervals: [500, 1000, 2000] }).toBe('success')
}

async function managerFieldGraph(page, locator, itemID, field) {
  await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByRole('radio', { name: '字段级', exact: true })).toBeVisible()
  const graphResponse = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/meta/lineage/graph' &&
      url.searchParams.get('subject_kind') === 'data_item' &&
      url.searchParams.get('item_id') === String(itemID) &&
      url.searchParams.get('granularity') === 'field'
  })
  await frame.getByText('字段级', { exact: true }).click()
  const response = await graphResponse
  expect(response.status()).toBe(200)
  const graph = await response.json()
  expect(graph.granularity).toBe('field')
  expect(graph.subject.kind).toBe('data_item')
  expect(String(graph.subject.item_id)).toBe(String(itemID))
  expect(graph.subject.schema_snapshot_hash).toBeTruthy()
  expect(graph.truncated).toBe(false)
  await expect(frame.locator('.lineage-fields')).toBeVisible()
  await frame.getByRole('button', { name: field, exact: true }).click()
  await expect(frame.locator('.lineage-inspector strong')).toHaveText(field)
  await expect(frame.locator('.lineage-canvas canvas').first()).toBeVisible()
  const selected = graph.nodes.find(node => node.item_id === itemID && node.field_name === field && node.schema_snapshot_hash === graph.subject.schema_snapshot_hash)
  expect(selected?.kind).toBe('field_ref')
  expect(selected.field_lineage_status).toBe('complete')
  const focus = lineageFieldConnections(graph.edges, lineageNodeId(selected))
  const focused = { nodes: graph.nodes.filter(node => focus.fields.has(lineageNodeId(node))), edges: graph.edges.filter((_, index) => focus.connections.has(`lineage-edge:${index}`)) }
  return { graph, focused, frame }
}

async function verifyManagerLineage(page, api, env, sqlExecution) {
  const lineage = JSON.parse(env.ADDP_ONLINE_TRANSFER_FIELD_LINEAGE)
  const native = await managerFieldGraph(page, lineage.target_locator, lineage.target_item_id, 'area')
  expect(native.graph.field_lineage_status).toBe('complete')
  expect(native.graph.subject.schema_snapshot_hash).toBe(lineage.schema_snapshot_hash)
  const edgeIdentity = edge => [edge.source.item_id, edge.source.field_name, edge.target.item_id, edge.target.field_name, edge.transformation, edge.evidence.execution_id]
  expect(native.focused.edges.map(edgeIdentity).sort()).toEqual(lineage.expected_edges.sort())
  expect(native.focused.nodes).toHaveLength(3)
  for (const node of native.graph.nodes) {
    expect(node.kind).toBe('field_ref')
    expect(node.schema_snapshot_hash).toBeTruthy()
  }
  await expect(native.frame.getByRole('button', { name: '全部字段', exact: true })).toBeVisible()
  await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-native-field-lineage.png'), fullPage: true })

  await scanEngine(api, env.ADDP_ONLINE_TEST_ENGINE_ID)
  const items = await json(await api.get(`/api/v1/meta/engines/${env.ADDP_ONLINE_TEST_ENGINE_ID}/items`), 'read SQL target DataItem')
  const targets = items.filter(item => item.full_name === `public.${env.ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE}`)
  expect(targets).toHaveLength(1)
  const target = targets[0]
  const itemQuery = new URLSearchParams({ subject_kind: 'data_item', item_id: String(target.id), direction: 'upstream', depth: '2', limit: '100' })
  await expect.poll(async () => {
    const graph = await json(await api.get(`/api/v1/meta/lineage/graph?${itemQuery}`), 'wait for automatic SQL lineage collection')
    return graph.edges.some(edge => edge.evidence?.execution_id === sqlExecution.execution_id)
  }, { timeout: 120_000, intervals: [500, 1000, 2000] }).toBe(true)
  const locator = `addp://engine/${env.ADDP_ONLINE_TEST_ENGINE_ID}/path/public/${env.ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE}?type=table&item_id=${target.id}`
  const query = await managerFieldGraph(page, locator, target.id, 'amount')
  expect(query.graph.field_lineage_status).toBe('complete')
  expect(query.focused.edges).toHaveLength(1)
  expect(query.focused.edges[0].source.field_name).toBe('amount')
  expect(query.focused.edges[0].target.field_name).toBe('amount')
  expect(query.focused.edges[0].evidence.execution_id).toBe(sqlExecution.execution_id)
  expect(query.focused.edges[0].transformation).toBe('direct')
  await expect(query.frame.locator('.lineage-inspector strong')).toHaveText('amount')
  await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-query-field-lineage.png'), fullPage: true })
}

test('browser executes SQL ETL and verifies native field lineage in Manager', async ({ page }) => {
  test.setTimeout(360_000)
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const env = environment()
  const repository = env.ADDP_ONLINE_REPOSITORY
  const expectedStatement = `SELECT "id", "region", "amount" FROM "public"."${env.ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE}" WHERE "status" = :p1 AND "amount" >= :p2`
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const failedResponses = []
  const consoleErrors = []
  page.on('pageerror', error => consoleErrors.push(error.message))
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedResponses.push({ pathname, status: response.status() })
    }
  })
  page.on('console', message => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })

  let taskID = 0
  let taskDeleted = false
  try {
    const apiIdentity = identity(await json(await api.get('/api/v1/system/auth/context'), 'read API AuthContext'))
    expect(apiIdentity.principalType).toBe('user')
    expect(apiIdentity.contextType).toBe('tenant')
    expect(apiIdentity.tenantID).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    for (const permission of [
      'meta.catalog.read',
      'meta.lineage.read',
      'manager.data_item.read',
      'manager.content.read',
      'transfer.task.create',
      'transfer.task.delete',
      'transfer.task.execute',
      'transfer.task.read'
    ]) {
      expect(apiIdentity.permissions.has(permission), `missing permission ${permission}`).toBe(true)
    }

    const browserAccessToken = await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, "/transfer/tasks/create")
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'read browser AuthContext'))
    expect(browserIdentity.principalID).toBe(apiIdentity.principalID)
    expect(browserIdentity.tenantID).toBe(apiIdentity.tenantID)
    await browserAPI.dispose()

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const wizard = frame.getByTestId('transfer-task-wizard')
    await expect(wizard).toHaveAttribute('data-step', '0')
    await selectSearchResult(
      frame.getByTestId('task-source-picker'),
      env.ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE,
      env.ADDP_ONLINE_TEST_ENGINE_NAME
    )

    const queryToggle = frame.getByTestId('task-query-source-toggle')
    await queryToggle.locator('.el-switch__core').click()
    await expect(queryToggle.getByRole('switch')).toBeChecked()
    await expect(frame.getByTestId('task-query-language-fixed')).toHaveText('SQL')
    await expect(frame.getByTestId('task-query-language-select')).toHaveCount(0)
    const builder = frame.getByTestId('relational-sql-builder')
    await expect(builder).toBeVisible()
    await expect(builder.locator('.el-radio-group')).toHaveCount(0)
    await builder.getByTestId('sql-clear-selected-fields').click()
    await selectOutputField(builder, 'id')
    await selectOutputField(builder, 'region')
    await selectOutputField(builder, 'amount')

    await builder.getByTestId('sql-add-filter').click()
    const statusFilter = builder.getByTestId('sql-filter-0')
    await chooseSelectOption(frame, statusFilter.getByTestId('sql-filter-field'), 'status')
    await statusFilter.getByPlaceholder('输入值').fill('active')

    await builder.getByTestId('sql-add-filter').click()
    const amountFilter = builder.getByTestId('sql-filter-1')
    await chooseSelectOption(frame, amountFilter.getByTestId('sql-filter-field'), 'amount')
    await chooseSelectOption(frame, amountFilter.getByTestId('sql-filter-operator'), '大于等于')
    await amountFilter.getByPlaceholder('输入值').fill('100')

    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '1')
    await chooseSelectOption(frame, frame.getByTestId('task-target-engine'), env.ADDP_ONLINE_TEST_ENGINE_NAME)
    await selectSearchResult(frame.getByTestId('task-target-parent'), 'public', 'public')
    await frame.getByTestId('task-target-table').fill(env.ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE)
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '2')

    const mappings = frame.getByTestId('task-field-mappings')
    await expect(mappings.locator('.el-table__row')).toHaveCount(3)
    await expect(mappings).toContainText('id')
    await expect(mappings).toContainText('region')
    await expect(mappings).toContainText('amount')
    await expect(mappings).not.toContainText('status')
    await expect(mappings).not.toContainText('internal_note')
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '3')

    await frame.getByTestId('task-name').fill(env.ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME)
    await expect(frame.getByTestId('task-load-mode').locator('input[value="snapshot"]')).toBeChecked()
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '4')
    await expect(frame.getByTestId('task-review-step')).toContainText(env.ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME)

    const createResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'POST' && url.pathname === '/api/v1/transfer/task-definitions'
    })
    await frame.getByTestId('task-submit').click()
    const confirmDialog = frame.getByRole('dialog')
    await expect(confirmDialog).toBeVisible()
    await confirmDialog.locator('.el-button--primary').click()
    const createdResponse = await createResponse
    expect(createdResponse.status()).toBe(201)
    const created = await createdResponse.json()
    taskID = Number(created?.id)
    expect(Number.isInteger(taskID) && taskID > 0).toBe(true)

    const execution = await waitForExecution(api, taskID, 1, 2)
    expect(Number(execution.records_read)).toBe(2)
    const taskPayload = await json(await api.get(`/api/v1/transfer/task-definitions/${taskID}`), 'read Transfer task')
    expect(taskPayload?.config?.source?.query).toEqual({
      language: 'sql',
      statement: expectedStatement,
      parameters: { p1: 'active', p2: '100' }
    })
    const mappingsPayload = taskPayload?.config?.transforms
      ?.find(transform => transform?.type === 'field_mapping')?.fields
    expect(mappingsPayload?.map(mapping => mapping.source)).toEqual(['id', 'region', 'amount'])
    expect(taskPayload?.config?.target?.policy?.apply_mode).toBe('replace')
    controlFixture(repository, 'business/scripts/online-transfer-relational-sql-etl-fixture.sh', 'verify')
    await verifyManagerLineage(page, api, env, execution)

    await json(await api.delete(`/api/v1/transfer/task-definitions/${taskID}`), 'delete Transfer task')
    const deleted = await api.get(`/api/v1/transfer/task-definitions/${taskID}`)
    expect(deleted.status()).toBe(404)
    taskDeleted = true

    const unexpectedResponses = failedResponses.filter(response => !(
      taskDeleted && response.status === 404 && response.pathname === `/api/v1/transfer/task-definitions/${taskID}`
    ))
    expect(unexpectedResponses).toEqual([])
    expect(consoleErrors).toEqual([])

    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-relational-sql-etl-browser.json'),
      `${JSON.stringify({
        schema_version: 'addp.transfer-relational-sql-etl-browser/v2',
        suite: 'transfer-relational-sql-etl',
        run_id: env.ADDP_ONLINE_TEST_RUN_ID,
        result: 'passed',
        tenant_id: env.ADDP_ONLINE_TEST_TENANT_ID,
        task_name: env.ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME,
        query_language: 'sql',
        language_selector_hidden: true,
        projected_fields: ['id', 'region', 'amount'],
        parameter_count: 2,
        records_read: 2,
        records_written: 2,
        target_row_count: 2,
        task_deleted: true,
        manager_field_graph_verified: true,
        query_field_lineage_verified: true
      })}\n`,
      'utf8'
    )
  } finally {
    if (taskID > 0 && !taskDeleted) {
      const response = await api.delete(`/api/v1/transfer/task-definitions/${taskID}`)
      if (![200, 404].includes(response.status())) {
        throw new Error(`cleanup Transfer task returned HTTP ${response.status()}`)
      }
    }
    await api.dispose()
  }
})
