import { expect, test } from '@playwright/test'

const ENGINE = {
  id: 11,
  name: 'PostgreSQL Demo',
  engine_type: 'postgresql',
  lifecycle_state: 'active',
  connection_status: 'online',
  capabilities: {
    compute: {
      query: {
        supported: true,
        languages: ['sql'],
        default_language: 'sql',
        result_kinds: ['table', 'graph'],
        parameters: {
          supported: true,
          languages: ['sql'],
          types: ['string', 'integer', 'number', 'boolean']
        }
      }
    }
  }
}

const MONGO_ENGINE = {
  id: 12,
  name: 'MongoDB Demo',
  engine_type: 'mongodb',
  lifecycle_state: 'active',
  connection_status: 'online',
  capabilities: {
    compute: {
      query: {
        supported: true,
        languages: ['mql'],
        default_language: 'mql',
        result_kinds: ['table'],
        parameters: {
          supported: true,
          languages: ['mql'],
          types: ['string', 'integer', 'number', 'boolean']
        }
      }
    }
  }
}

const DUCKDB_RUNTIME = {
  id: 18,
  name: 'DuckDB Federation',
  engine_type: 'duckdb',
  lifecycle_state: 'active',
  connection_status: 'online',
  capabilities: {
    compute: {
      query: {
        supported: true,
        languages: ['sql'],
        default_language: 'sql',
        result_kinds: ['table'],
        federation: {
          supported: true,
          source_engine_types: ['postgresql', 'mysql', 'minio', 's3'],
          object_formats: ['parquet']
        }
      }
    }
  }
}

const EXECUTION_ID = '11111111-1111-4111-8111-111111111111'

test('renders the desktop workbench and a bounded table result without overlap', async ({ page }) => {
  const resultRows = Array.from({ length: 25 }, (_, index) => ({
    id: index + 1,
    name: index === 0 ? 'Ada' : index === 1 ? 'Grace' : `Person ${index + 1}`
  }))
  await installMockBackend(page, { resultKind: 'table', resultRows })
  await page.goto('/sql')

  await expect(page.getByRole('heading', { name: '查询开发', exact: true })).toBeVisible()
  await expect(page.locator('.catalog-panel').getByRole('treeitem', { name: ENGINE.name, exact: true })).toBeVisible()

  const catalogBox = await requiredBox(page.locator('.catalog-panel'))
  const querySurfaceBox = await requiredBox(page.locator('.query-surface'))
  const editorBox = await requiredBox(page.locator('.editor-panel'))
  const resultBox = await requiredBox(page.locator('.result-panel'))
  const workbenchBox = await requiredBox(page.locator('.query-workbench'))
  expect(catalogBox.x + catalogBox.width).toBeLessThanOrEqual(querySurfaceBox.x)
  expect(editorBox.y + editorBox.height).toBeLessThanOrEqual(resultBox.y)
  expect(workbenchBox.y + workbenchBox.height).toBeLessThanOrEqual(800)

  const resourceTree = page.locator('.catalog-panel')
  await resourceTree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await resourceTree.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await resourceTree.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await resourceTree.getByRole('button', { name: '生成查询模板', exact: true }).click()

  const executeButton = page.getByRole('button', { name: '执行', exact: true })
  await expect(executeButton).toBeEnabled()
  await executeButton.click()

  await expect(page.getByText('Ada', { exact: true })).toBeVisible()
  const truncationStatus = page.locator('.result-summary').getByText('结果已截断，仅加载前 25 行', { exact: true })
  await expect(truncationStatus).toBeVisible()
  await expect(page.locator('.result-alert').getByText('结果已截断，仅加载前 25 行', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '查看执行详情', exact: true })).toBeVisible()
  const summaryBox = await requiredBox(page.locator('.result-summary'))
  const truncationBox = await requiredBox(page.locator('.truncated-status'))
  const summaryActionsBox = await requiredBox(page.locator('.summary-actions'))
  expect(truncationBox.y).toBeGreaterThanOrEqual(summaryBox.y)
  expect(truncationBox.y + truncationBox.height).toBeLessThanOrEqual(summaryBox.y + summaryBox.height)
  expect(truncationBox.x + truncationBox.width).toBeLessThanOrEqual(summaryActionsBox.x)
  await expect(page.locator('.result-grid')).toHaveCSS('min-height', '160px')
  const tableBox = await requiredBox(page.locator('.result-grid > .result-table'))
  const paginationBox = await requiredBox(page.locator('.result-pagination'))
  expect(tableBox.y + tableBox.height).toBeLessThanOrEqual(paginationBox.y)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByText('Person 21', { exact: true })).toBeVisible()
  await expect(page.getByText('Ada', { exact: true })).toHaveCount(0)
  await expectNoDocumentOverflow(page)
})

test.describe('narrow query workbench', () => {
  test.use({ viewport: { width: 760, height: 700 } })

  test('moves data resources into a drawer and keeps execution context in graph mode', async ({ page }) => {
    await installMockBackend(page, { resultKind: 'graph' })
    await page.goto('/sql')

    await expect(page.locator('.catalog-panel')).toHaveCount(0)
    const catalogButton = page.getByRole('button', { name: '数据资源', exact: true })
    await expect(catalogButton).toHaveCount(1)
    await catalogButton.click()
    const drawer = page.getByRole('dialog', { name: '数据资源', exact: true })
    await expect(drawer).toBeVisible()
    await expect(drawer.getByRole('treeitem', { name: ENGINE.name, exact: true })).toBeVisible()
    await drawer.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
    await drawer.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
    await drawer.getByRole('treeitem', { name: 'customers', exact: true }).click()
    await drawer.getByRole('button', { name: '生成查询模板', exact: true }).click()

    await page.getByRole('button', { name: '执行', exact: true }).click()
    await expect(page.getByText('节点: 2', { exact: true })).toBeVisible()
    await expect(page.getByText('关系: 1', { exact: true })).toBeVisible()
    await expect(page.getByText('结果已截断，仅加载前 2 行', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '查看执行详情', exact: true })).toBeVisible()
    await expect(page.locator('.graph-canvas canvas')).toBeVisible()
    const workbenchBox = await requiredBox(page.locator('.query-workbench'))
    expect(workbenchBox.y + workbenchBox.height).toBeLessThanOrEqual(700)
    await expectNoDocumentOverflow(page)
  })
})

test('generates a query template for the selected data item and confirms engine switches', async ({ page }) => {
  const sampleRequests = []
  await installMockBackend(page, { resultKind: 'table', engines: [ENGINE, MONGO_ENGINE] })
  page.on('request', request => {
    if (request.url().includes('/sample-query')) sampleRequests.push(new URL(request.url()))
  })
  await page.goto('/sql')

  const resourceTree = page.locator('.catalog-panel')
  await resourceTree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  const publicNode = resourceTree.getByRole('treeitem', { name: 'public', exact: true })
  await publicNode.locator('.el-tree-node__expand-icon').click()
  await resourceTree.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await resourceTree.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await expect.poll(() => sampleRequests.at(-1)?.searchParams.get('locator')).toContain('addp://engine/11/path/public/customers')

  await page.locator('.engine-select').click()
  await page.getByRole('option', { name: /MongoDB Demo/ }).click()
  await page.getByRole('dialog', { name: '切换查询引擎', exact: true }).getByRole('button', { name: '清空并切换', exact: true }).click()
  await expect(page.getByText('MQL', { exact: true })).toBeVisible()
  await expect(page.getByText('SELECT * FROM', { exact: false })).toHaveCount(0)
  const toolbarTemplateButton = page.locator('.toolbar-actions').getByRole('button', { name: '生成查询模板', exact: true })
  await expect(toolbarTemplateButton).toBeEnabled()
  await toolbarTemplateButton.click()
  await expect(page.getByText('请先选择一个可查询的数据项', { exact: true })).toBeVisible()
  await expect.poll(() => sampleRequests.at(-1)?.searchParams.get('locator')).toContain('addp://engine/11/path/public/customers')
})

test('uses tenant source-engine catalogs for the DuckDB federated runtime', async ({ page }) => {
  const resourceTreeRequests = []
  await installMockBackend(page, {
    resultKind: 'table',
    engines: [DUCKDB_RUNTIME, ENGINE],
    metaEngines: [ENGINE],
    resourceTreeRequests
  })
  await page.goto('/sql')

  await expect(page.locator('.engine-select')).toContainText(DUCKDB_RUNTIME.name)
  const resourceTree = page.locator('.catalog-panel')
  await expect(resourceTree.getByRole('treeitem', { name: ENGINE.name, exact: true })).toBeVisible()
  await expect.poll(() => resourceTreeRequests).toContain(`/api/v1/meta/resource-tree/${ENGINE.id}`)
  expect(resourceTreeRequests).not.toContain(`/api/v1/meta/resource-tree/${DUCKDB_RUNTIME.id}`)

  await resourceTree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await resourceTree.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await resourceTree.getByRole('treeitem', { name: 'customers', exact: true }).dblclick()
  await expect(page.locator('.monaco-editor .view-lines')).toContainText('PostgreSQL_Demo.public.customers')
  expect(resourceTreeRequests).not.toContain(`/api/v1/meta/resource-tree/${DUCKDB_RUNTIME.id}`)
})

test('generates a natural-language query with current-engine resource confirmation', async ({ page }) => {
  const copilotRequests = []
  await installMockBackend(page, { resultKind: 'table', copilotRequests })
  await page.goto('/sql')

  await page.getByRole('button', { name: 'AI 查询助手', exact: true }).click()
  await page.getByPlaceholder('描述你要查询的内容，例如：计算铁路两边宽度50米所占用的耕地面积')
    .fill('计算铁路两边宽度50米所占用的耕地面积')
  await page.getByRole('button', { name: '生成查询', exact: true }).click()

  const confirmation = page.getByRole('dialog', { name: '需要确认查询含义', exact: true })
  await expect(confirmation).toBeVisible()
  await expect(confirmation.getByText('railway', { exact: true }).first()).toBeVisible()
  await expect(confirmation.getByText('farmland_b', { exact: true }).first()).toBeVisible()
  await expect(confirmation.getByText(
    '所在数据库：analytics · 资源类型：数据表 · 空间字段：shape · 坐标系：EPSG:32650',
    { exact: true }
  ).first()).toBeVisible()
  await expect(confirmation).not.toContainText('addp://')
  await confirmation.getByText('farmland_b', { exact: true }).click()
  await confirmation.getByRole('button', { name: '确认并生成', exact: true }).click()

  await expect.poll(() => copilotRequests.length).toBe(2)
  await expect(confirmation).toBeHidden()
  expect(copilotRequests[0].engine_id).toBe(ENGINE.id)
  expect(copilotRequests[0].resources).toEqual([])
  expect(copilotRequests[1].resources).toHaveLength(2)
  expect(copilotRequests[1].resources.every(resource => resource.engine_id === ENGINE.id)).toBe(true)
  await expect(page.locator('.monaco-editor .view-lines')).toContainText('ST_Intersection')
  await expect(page.getByRole('button', { name: '执行', exact: true })).toBeEnabled()
  expect(copilotRequests.every(request => request.engine_id === ENGINE.id)).toBe(true)
})

test('continues generation with a structured calculation-rule clarification', async ({ page }) => {
  const copilotRequests = []
  await installMockBackend(page, { resultKind: 'table', copilotRequests })
  await page.goto('/sql')

  await page.getByRole('button', { name: 'AI 查询助手', exact: true }).click()
  await page.getByPlaceholder('描述你要查询的内容，例如：计算铁路两边宽度50米所占用的耕地面积')
    .fill('计算两个集合的重叠度')
  await page.getByRole('button', { name: '生成查询', exact: true }).click()

  const clarification = page.getByRole('dialog', { name: '需要确认查询含义', exact: true })
  await clarification.getByText('farmland_b', { exact: true }).click()
  await clarification.getByRole('button', { name: '确认并生成', exact: true }).click()
  await expect(clarification.getByText('请选择本次指标的计算规则', { exact: true })).toBeVisible()
  await clarification.getByText('Jaccard 相似度', { exact: true }).click()
  await clarification.getByRole('button', { name: '确认并生成', exact: true }).click()

  await expect.poll(() => copilotRequests.length).toBe(3)
  expect(copilotRequests[2].clarification_answers).toEqual({ 'metric.definition': 'jaccard' })
  await expect(clarification).toBeHidden()
  await expect(page.locator('.monaco-editor .view-lines')).toContainText('ST_Intersection')
})

test('defines a query parameter, inserts its reference, and submits an execution override', async ({ page }) => {
  const executionRequests = []
  await installMockBackend(page, { resultKind: 'table', executionRequests })
  await page.goto('/sql')

  const resourceTree = page.locator('.catalog-panel')
  await resourceTree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await resourceTree.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await resourceTree.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await resourceTree.getByRole('button', { name: '生成查询模板', exact: true }).click()

  const parameterButton = page.locator('.editor-panel').getByRole('button', { name: /查询参数/ })
  await expect(parameterButton).toBeEnabled()
  await parameterButton.click()

  const parameterPanel = page.locator('.query-parameter-panel')
  const querySurfaceBox = await requiredBox(page.locator('.query-surface'))
  const parameterPanelBox = await requiredBox(parameterPanel)
  expect(parameterPanelBox.x).toBeGreaterThanOrEqual(querySurfaceBox.x + querySurfaceBox.width - 1)
  expect(parameterPanelBox.y).toBeGreaterThanOrEqual(querySurfaceBox.y)
  await parameterPanel.getByRole('button', { name: '添加参数', exact: true }).click()
  await parameterPanel.getByLabel('参数名', { exact: true }).fill('nickname')
  await parameterPanel.locator('.scalar-default-value .el-checkbox').click()
  await parameterPanel.locator('.scalar-default-value .el-input__inner').fill('Ada')
  await parameterPanel.getByRole('button', { name: '插入到查询', exact: true }).click()

  await expect(page.locator('.monaco-editor .view-lines')).toContainText(':nickname')
  await page.getByRole('button', { name: '执行', exact: true }).click()

  const executionDialog = page.getByRole('dialog', { name: '本次执行参数', exact: true })
  await executionDialog.getByText('nickname', { exact: true }).locator('..').getByText('执行时指定', { exact: true }).click()
  await executionDialog.getByRole('textbox').fill('Grace')
  await executionDialog.getByRole('button', { name: '执行', exact: true }).click()

  await expect.poll(() => executionRequests.length).toBe(1)
  expect(executionRequests[0].content.query).toContain(':nickname')
  expect(executionRequests[0].content.query_parameters).toEqual([{
    name: 'nickname',
    type: 'string',
    default: 'Ada'
  }])
  expect(executionRequests[0].parameters).toEqual({ nickname: 'Grace' })
  await expect(page.getByText('Ada', { exact: true })).toBeVisible()
})

async function installMockBackend(page, {
  resultKind,
  engines = [ENGINE],
  metaEngines = [ENGINE],
  executionRequests = [],
  copilotRequests = [],
  resourceTreeRequests = [],
  resultRows
}) {
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'develop-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'develop-e2e' })
    }
    if (path === '/api/v1/develop/engines') {
      return fulfillJSON(route, engines)
    }
    if (path === '/api/v1/meta/engines') {
      return fulfillJSON(route, metaEngines)
    }
    if (path === '/api/v1/copilot/query/generate' && request.method() === 'POST') {
      const body = request.postDataJSON()
      copilotRequests.push(body)
      if (!body.resources?.length) {
        return fulfillJSON(route, {
          status: 'need_clarification',
          query_language: 'sql',
          resources: [],
          clarifications: [{
            key: 'query.resources',
            category: 'resource_selection',
            prompt: '请选择查询资源',
            control: 'resource_choice',
            required: true,
            options: [],
            resource_candidates: [
            {
              role: 'railway',
              name: 'railway',
              full_name: 'public.railway',
              locator: 'addp://engine/11/path/public/railway?type=table&item_id=1102',
              engine_id: 11,
              asset_type: 'table',
              data_type: 'table',
              ancestors: [{ label: 'analytics', type: 'database' }],
              geometry_column: 'shape',
              crs: 'EPSG:32650'
            },
            {
              role: 'farmland',
              name: 'farmland_a',
              locator: 'addp://engine/11/path/public/farmland_a?type=table&item_id=1103',
              engine_id: 11,
              asset_type: 'table',
              data_type: 'table',
              ancestors: [{ label: 'analytics', type: 'database' }],
              geometry_column: 'shape',
              crs: 'EPSG:32650'
            },
            {
              role: 'farmland',
              name: 'farmland_b',
              locator: 'addp://engine/11/path/public/farmland_b?type=table&item_id=1104',
              engine_id: 11,
              asset_type: 'table',
              data_type: 'table',
              ancestors: [{ label: 'analytics', type: 'database' }],
              geometry_column: 'shape',
              crs: 'EPSG:32650'
            }
            ]
          }]
        })
      }
      if (body.query.includes('重叠度') && !body.clarification_answers?.['metric.definition']) {
        return fulfillJSON(route, {
          status: 'need_clarification',
          query_language: 'sql',
          resources: body.resources,
          clarifications: [{
            key: 'metric.definition',
            category: 'calculation_rule',
            prompt: '请选择本次指标的计算规则',
            control: 'single_choice',
            required: true,
            options: [
              { value: 'count', label: '共同元素数量', description: '统计交集数量' },
              { value: 'jaccard', label: 'Jaccard 相似度', description: '交集除以并集' }
            ],
            resource_candidates: []
          }]
        })
      }
      return fulfillJSON(route, {
        status: 'success',
        query_language: 'sql',
        query: 'SELECT ST_Intersection(railway.shape, farmland_b.shape) FROM public.railway JOIN public.farmland_b ON true',
        query_parameters: [],
        resources: body.resources
      })
    }
    if (path.endsWith('/sample-query')) {
      const requestURL = new URL(request.url())
      if (requestURL.searchParams.has('locator')) {
        return fulfillJSON(route, { query: 'SELECT id, name FROM public.customers LIMIT 10', language: 'sql' })
      }
      const engineID = Number(path.split('/')[5])
      const engine = engines.find(item => item.id === engineID) || ENGINE
      return fulfillJSON(route, engine.id === MONGO_ENGINE.id
        ? { query: '{"find":"orders","filter":{},"limit":10}', language: 'mql' }
        : { query: 'SELECT id, name FROM public.customers', language: 'sql' })
    }
    if (path.startsWith('/api/v1/meta/resource-tree/')) {
      resourceTreeRequests.push(path)
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}`) {
      return fulfillJSON(route, resourceTree())
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}/node`) {
      const locator = new URL(request.url()).searchParams.get('locator') || ''
      if (locator.includes('/path/public?type=schema')) {
        return fulfillJSON(route, { children: resourceTree().children[0].children })
      }
      return fulfillJSON(route, { children: resourceTree().children })
    }
    if (path === '/api/v1/develop/query-preflight' && request.method() === 'POST') {
      return fulfillJSON(route, {
        allowed: true,
        effect: 'read',
        statement: 'select',
        classification_confidence: 'high',
        target_objects: ['public.customers'],
        risk_level: 'low',
        requires_confirmation: false,
        required_permission: 'develop.data_read.execute',
        fingerprint: 'query-preflight-e2e'
      })
    }
    if (path === '/api/v1/develop/executions' && request.method() === 'POST') {
      executionRequests.push(request.postDataJSON())
      return fulfillJSON(route, { execution_id: EXECUTION_ID })
    }
    if (path === `/api/v1/develop/executions/${EXECUTION_ID}`) {
      return fulfillJSON(route, executionResult(resultKind, resultRows))
    }

    return fulfillJSON(route, {})
  })
}

function resourceTree() {
  return {
    id: `addp://engine/${ENGINE.id}/path?type=database`,
    locator: `addp://engine/${ENGINE.id}/path?type=database`,
    label: ENGINE.name,
    type: 'database',
    children: [{
      id: `addp://engine/${ENGINE.id}/path/public?type=schema`,
      locator: `addp://engine/${ENGINE.id}/path/public?type=schema`,
      label: 'public',
      type: 'schema',
      children: [{
        id: `addp://engine/${ENGINE.id}/path/public/customers?type=table&item_id=1101`,
        locator: `addp://engine/${ENGINE.id}/path/public/customers?type=table&item_id=1101`,
        label: 'customers',
        type: 'table',
        children: []
      }]
    }]
  }
}

function executionResult(resultKind, resultRows) {
  const graphData = resultKind === 'graph'
    ? {
        nodes: [
          { element_id: 'n1', labels: ['Person'], properties: { name: 'Ada' } },
          { element_id: 'n2', labels: ['Person'], properties: { name: 'Grace' } }
        ],
        relationships: [{
          element_id: 'r1',
          type: 'KNOWS',
          start_node_id: 'n1',
          end_node_id: 'n2',
          properties: {}
        }]
      }
    : null

  const previewRows = resultRows || [
    { id: 1, name: 'Ada' },
    { id: 2, name: 'Grace' }
  ]

  return {
    execution_id: EXECUTION_ID,
    status: 'success',
    progress: 100,
    execution_time_ms: 18,
    metadata: {
      result: {
        columns: ['id', 'name'],
        rows_count: resultRows ? previewRows.length : 3,
        rows_affected: 0,
        result_kind: resultKind,
        result_limit: previewRows.length,
        truncated: true,
        graph_data: graphData,
        summary: {
          preview_rows: previewRows
        }
      }
    }
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}

async function requiredBox(locator) {
  await expect(locator).toBeVisible()
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  return box
}

async function expectNoDocumentOverflow(page) {
  const overflow = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth
  }))
  expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth)
  expect(overflow.bodyScrollWidth).toBeLessThanOrEqual(overflow.clientWidth)
}
