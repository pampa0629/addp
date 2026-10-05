import { observeLineageCanvas, lineageCanvasText, lineageCanvasSnapshot, dragLineageTable } from '../../../../common-frontend/basic/tests/fixtures/lineageCanvas.js'
import { lineageFieldConnections, FIELD_FONT_SIZE } from '../../../../common-frontend/graph/src/lineageFields.js'
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
  'ADDP_ONLINE_TRANSFER_MONGODB_FIELD_LINEAGE',
  'ADDP_ONLINE_ORCHESTRATED_FIELD_LINEAGE',
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

async function managerFieldGraph(page, locator, itemID, field, depth = 2) {
  await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByRole('radio', { name: '字段级', exact: true })).toBeVisible()
  if (depth !== 2) await chooseSelectOption(frame, frame.locator('.lineage-depth'), `${depth} 层`)
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

async function showFieldOverview(frame) {
  await frame.getByRole('button', { name: '全部字段', exact: true }).click()
  await expect(frame.locator('.lineage-inspector')).toHaveCount(0)
  await frame.getByRole('button', { name: '适应窗口', exact: true }).click()
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

  const chain = JSON.parse(env.ADDP_ONLINE_ORCHESTRATED_FIELD_LINEAGE)
  const mongodb = JSON.parse(env.ADDP_ONLINE_TRANSFER_MONGODB_FIELD_LINEAGE)
  expect(mongodb.execution_ids).toHaveLength(2)
  expect(mongodb.execution_ids[0]).not.toBe(mongodb.latest_execution_id)
  let mongodbGraphRequests = 0
  const countGraphRequest = req => {
    const url = new URL(req.url())
    if (url.pathname === '/api/v1/meta/lineage/graph' && url.searchParams.get('granularity') === 'field' && url.searchParams.get('item_id') === String(mongodb.target_item_id)) mongodbGraphRequests++
  }
  page.on('request', countGraphRequest)
  try {
    const ods = await managerFieldGraph(page, mongodb.target_locator, mongodb.target_item_id, 'activity_date_raw')
    expect(ods.graph.field_lineage_status).toBe('complete')
    expect(ods.graph.subject.schema_snapshot_hash).toBe(mongodb.schema_snapshot_hash)
    expect(ods.graph.nodes).toHaveLength(18)
    expect(ods.graph.edges.map(edgeIdentity).sort()).toEqual(chain.expected_edges.sort())
    expect(ods.focused.edges.map(edgeIdentity).sort()).toEqual(chain.date_edges.sort())
    for (const node of ods.graph.nodes) {
      const isSource = node.item_id === mongodb.source_item_id
      expect(node.engine_id).toBe(isSource ? mongodb.source_engine_id : mongodb.target_engine_id)
      expect(node.schema_snapshot_hash).toBe(chain.snapshot_hashes[String(node.item_id)])
      // Completeness is projected for the requested table's root fields only.
      if (node.item_id === mongodb.target_item_id) expect(node.field_lineage_status).toBe('complete')
    }
    await ods.frame.getByRole('button', { name: 'leader_nickname_snapshot', exact: true }).click()
    await expect(ods.frame.locator('.lineage-inspector strong')).toHaveText('leader_nickname_snapshot')
    const nickname = ods.graph.nodes.find(node => node.item_id === mongodb.target_item_id && node.field_name === 'leader_nickname_snapshot')
    const focused = lineageFieldConnections(ods.graph.edges, lineageNodeId(nickname))
    const edges = ods.graph.edges.filter((_, index) => focused.connections.has(`lineage-edge:${index}`))
    expect(edges.map(edgeIdentity).sort()).toEqual(chain.nickname_edges.sort())
    await expect(ods.frame.getByRole('button', { name: '全部字段', exact: true })).toBeVisible()
    await showFieldOverview(ods.frame)
    expect(mongodbGraphRequests).toBe(1)
    await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-mongodb-ods-field-lineage.png'), fullPage: true })
  } finally {
    page.off('request', countGraphRequest)
  }
  expect(chain.rounds).toHaveLength(3)
  expect(chain.schema_evolution.history_verified).toBe(true)
  expect(chain.schema_evolution.unproven_current_verified).toBe(true)
  expect(chain.schema_evolution.new_hash).toBe(chain.schema_snapshot_hash)
  expect(chain.schema_evolution.old_hash).not.toBe(chain.schema_snapshot_hash)
  expect(chain.rounds[0].parent_execution_id).not.toBe(chain.rounds[1].parent_execution_id)
  const dwd = await managerFieldGraph(page, chain.target_locator, chain.target_item_id, 'activity_date', 3)
  expect(dwd.graph.subject.schema_snapshot_hash).toBe(chain.schema_snapshot_hash)
  expect(dwd.focused.edges.map(edgeIdentity).sort()).toEqual(chain.date_edges.sort())
  expect(dwd.focused.nodes).toHaveLength(4)
  for (const node of dwd.graph.nodes) {
    expect(node.schema_snapshot_hash).toBe(chain.snapshot_hashes[String(node.item_id)])
    if (node.item_id === chain.target_item_id) expect(node.field_lineage_status).toBe('complete')
  }
  const latestChildren = Object.values(chain.rounds[2].child_execution_ids)
  for (const edge of dwd.graph.edges) expect(latestChildren).toContain(edge.evidence.execution_id)
  await expect(dwd.frame.getByRole('button', { name: 'person_nickname', exact: true })).toHaveCount(0)
  const changedDateEdge = dwd.graph.edges.find(edge => edge.target.item_id === chain.target_item_id && edge.target.field_name === 'activity_date')
  expect(changedDateEdge.transformation).toBe('derived')
  await dwd.frame.getByRole('button', { name: 'person_display_name', exact: true }).click()
  await expect(dwd.frame.locator('.lineage-inspector strong')).toHaveText('person_display_name')
  const selected = dwd.graph.nodes.find(node => node.item_id === chain.target_item_id && node.field_name === 'person_display_name')
  const focus = lineageFieldConnections(dwd.graph.edges, lineageNodeId(selected))
  expect(dwd.graph.edges.filter((_, index) => focus.connections.has(`lineage-edge:${index}`)).map(edgeIdentity).sort()).toEqual(chain.nickname_edges.sort())
  await showFieldOverview(dwd.frame)
  const canvas = dwd.frame.locator('.lineage-canvas canvas').first()
  // Folding changes the card presentation without changing the field graph facts.
  await dwd.frame.getByRole('button', { name: '收起字段', exact: true }).click()
  await expect(dwd.frame.getByRole('button', { name: '收起字段', exact: true })).toBeDisabled()
  const fieldNames = dwd.graph.nodes.map(node => node.field_name)
  const isFieldLabel = row => fieldNames.some(name => row.text === name || (row.text.endsWith('…') && name.startsWith(row.text.slice(0, -1))))
  await expect.poll(async () => (await lineageCanvasText(canvas)).filter(isFieldLabel).length).toBe(0)
  await dwd.frame.getByRole('button', { name: 'person_display_name', exact: true }).click()
  await expect(dwd.frame.locator('.lineage-inspector strong')).toHaveText('person_display_name')
  await expect.poll(async () => (await lineageCanvasText(canvas)).some(row => row.text === 'person_display_name')).toBe(true)
  await dwd.frame.getByRole('button', { name: '展开字段', exact: true }).click()
  await expect(dwd.frame.getByRole('button', { name: '展开字段', exact: true })).toBeDisabled()
  await showFieldOverview(dwd.frame)
  const search = dwd.frame.getByRole('textbox', { name: '搜索字段', exact: true })
  await search.fill('PERSON_DISPLAY')
  await expect(dwd.frame.locator('.lineage-field-options button')).toHaveCount(2)
  await dwd.frame.getByRole('button', { name: 'person_display_name', exact: true }).click()
  await expect(dwd.frame.locator('.lineage-inspector strong')).toHaveText('person_display_name')
  const viewer = dwd.frame.locator('.lineage-viewer')
  await expect.poll(async () => {
    const row = (await lineageCanvasText(canvas)).filter(row => row.text === 'person_display_name').at(-1)
    return row ? Math.abs(row.y - (await canvas.boundingBox()).height / 2) : Infinity
  }).toBeLessThan(3)
  const ordinaryBounds = await canvas.boundingBox()
  const selectedPosition = (await lineageCanvasText(canvas)).filter(row => row.text === 'person_display_name').at(-1)
  await dwd.frame.getByRole('button', { name: '全屏查看', exact: true }).click()
  await expect.poll(() => viewer.evaluate(element => element.ownerDocument.fullscreenElement === element)).toBe(true)
  await expect.poll(() => page.evaluate(() => document.fullscreenElement?.tagName)).toBe('IFRAME')
  await expect.poll(async () => (await canvas.boundingBox()).width).toBeGreaterThan(ordinaryBounds.width)
  await expect(search).toHaveValue('PERSON_DISPLAY')
  await expect(dwd.frame.locator('.lineage-inspector strong')).toHaveText('person_display_name')
  const fullPosition = (await lineageCanvasText(canvas)).filter(row => row.text === 'person_display_name').at(-1)
  expect(fullPosition.x).toBeCloseTo(selectedPosition.x, 1)
  expect(fullPosition.y).toBeCloseTo(selectedPosition.y, 1)
  expect(fullPosition.fontSize).toBeCloseTo(selectedPosition.fontSize, 1)
  await dwd.frame.getByRole('button', { name: '退出全屏', exact: true }).click()
  await expect.poll(() => viewer.evaluate(element => element.ownerDocument.fullscreenElement === element)).toBe(false)
  await expect.poll(async () => (await canvas.boundingBox()).width).toBeCloseTo(ordinaryBounds.width, 1)
  await expect.poll(async () => (await canvas.boundingBox()).height).toBeCloseTo(ordinaryBounds.height, 1)
  await expect(dwd.frame.locator('.lineage-inspector strong')).toHaveText('person_display_name')
  await search.fill('')
  await showFieldOverview(dwd.frame)
  const beforeDrag = await lineageCanvasText(canvas)
  const rootTitle = dwd.graph.nodes.find(node => node.item_id === chain.target_item_id).name
  const header = beforeDrag.find(row => row.text === rootTitle || (row.text.endsWith('…') && rootTitle.startsWith(row.text.slice(0, -1))))
  expect(header).toBeTruthy()
  const display = beforeDrag.filter(row => row.text === 'person_display_name').at(-1)
  await dragLineageTable(page, canvas, header.text, -30, 30)
  await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === header.text)?.y).toBeCloseTo(header.y + 30, 1)
  const moved = (await lineageCanvasText(canvas)).filter(row => row.text === 'person_display_name').at(-1)
  expect(moved.x).toBeCloseTo(display.x - 30, 1)
  expect(moved.y).toBeCloseTo(display.y + 30, 1)
  let dragGeometry
  try {
    await expect.poll(async () => {
      dragGeometry = await lineageCanvasSnapshot(canvas)
      const target = dragGeometry.rows.filter(row => row.text === 'person_display_name').at(-1)
      if (!target) return false
      const scale = target.fontSize / FIELD_FONT_SIZE
      return dragGeometry.paths.some(points => {
        const endpoint = points.at(-1)
        return Math.abs(endpoint.x - target.x + 12 * scale) < 2 && Math.abs(endpoint.y - target.y) < 2
      })
    }).toBe(true)
  } finally {
    // Keep the observed geometry even if the Hosted endpoint assertion fails.
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'field-lineage-drag-geometry.json'), JSON.stringify({ before: display, after: moved, ...dragGeometry }, null, 2))
  }
  await dwd.frame.getByRole('button', { name: '自动布局', exact: true }).click()
  await expect(dwd.frame.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
  await expect.poll(async () => Math.abs((await lineageCanvasText(canvas)).find(row => row.text === header.text)?.x - header.x)).toBeLessThan(2)
  await expect.poll(async () => Math.abs((await lineageCanvasText(canvas)).find(row => row.text === header.text)?.y - header.y)).toBeLessThan(2)
  await expect.poll(async () => (await lineageCanvasText(canvas)).filter(isFieldLabel).length).toBe(dwd.graph.nodes.length)
  const box = await canvas.boundingBox()
  const fieldRows = (await lineageCanvasText(canvas)).filter(isFieldLabel)
  for (const row of fieldRows) {
    expect(row.fontSize).toBeGreaterThanOrEqual(11)
    expect(row.x).toBeGreaterThanOrEqual(0)
    expect(row.x + row.width).toBeLessThanOrEqual(box.width)
    expect(row.y).toBeGreaterThan(row.fontSize)
    expect(row.y).toBeLessThan(box.height)
  }
  writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-field-overview-layout.json'), JSON.stringify({ canvas_width_px: box.width, canvas_height_px: box.height, graph_nodes: dwd.graph.nodes.length, graph_edges: dwd.graph.edges.length, fields_painted: fieldRows.length, min_field_font_px: Math.min(...fieldRows.map(row => row.fontSize)), rows: fieldRows }, null, 2))
  await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-orchestrated-dwd-field-lineage.png'), fullPage: true })
}

test('browser executes SQL ETL and verifies native field lineage in Manager', async ({ page }) => {
  test.setTimeout(360_000)
  await observeLineageCanvas(page)
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
    expect(apiIdentity.permissions.has('catalog.entry.read')).toBe(false)

    const browserAccessToken = await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, "/")
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'read browser AuthContext'))
    expect(browserIdentity.principalID).toBe(apiIdentity.principalID)
    expect(browserIdentity.tenantID).toBe(apiIdentity.tenantID)
    await browserAPI.dispose()

    // Signed-out bootstrap precedes the verified User session; inspect every business request from here.
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
    await page.goto('/transfer/tasks/create')

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
        schema_version: 'addp.transfer-relational-sql-etl-browser/v5',
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
        query_field_lineage_verified: true,
        manager_mongodb_field_graph_verified: true,
        manager_orchestrated_field_graph_verified: true,
        manager_evolved_schema_verified: true
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
