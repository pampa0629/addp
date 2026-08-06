import { expect, test } from '@playwright/test'

const ENGINE = {
  id: 11,
  name: 'PostgreSQL Demo',
  engine_type: 'postgresql',
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

const EXECUTION_ID = '11111111-1111-4111-8111-111111111111'

test('renders the desktop workbench and a bounded table result without overlap', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table' })
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
  await expect(page.getByText('结果已截断，仅展示前 2 行', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '查看执行详情', exact: true })).toBeVisible()
  await expect(page.locator('.result-grid')).toHaveCSS('min-height', '160px')
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
    await expect(page.getByText('结果已截断，仅展示前 2 行', { exact: true })).toBeVisible()
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

test('defines a query parameter, inserts its reference, and submits an execution override', async ({ page }) => {
  const executionRequests = []
  await installMockBackend(page, { resultKind: 'table', executionRequests })
  await page.goto('/sql')

  const resourceTree = page.locator('.catalog-panel')
  await resourceTree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await resourceTree.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await resourceTree.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await resourceTree.getByRole('button', { name: '生成查询模板', exact: true }).click()

  const parameterButton = page.locator('.toolbar-actions').getByRole('button', { name: '查询参数', exact: true })
  await expect(parameterButton).toBeEnabled()
  await parameterButton.click()

  const parameterDrawer = page.getByRole('dialog', { name: '查询参数', exact: true })
  await parameterDrawer.getByRole('button', { name: '添加参数', exact: true }).click()
  await parameterDrawer.getByLabel('参数名', { exact: true }).fill('nickname')
  await parameterDrawer.getByLabel('默认值', { exact: true }).fill('Ada')
  await parameterDrawer.getByLabel('显示名称', { exact: true }).fill('昵称')
  await parameterDrawer.getByRole('button', { name: '插入参数引用', exact: true }).click()

  await expect(page.locator('.monaco-editor .view-lines')).toContainText(':nickname')
  await page.getByRole('button', { name: '执行', exact: true }).click()

  const executionDialog = page.getByRole('dialog', { name: '本次执行参数', exact: true })
  await executionDialog.getByText('昵称', { exact: true }).locator('..').getByText('执行时指定', { exact: true }).click()
  await executionDialog.getByRole('textbox').fill('Grace')
  await executionDialog.getByRole('button', { name: '执行', exact: true }).click()

  await expect.poll(() => executionRequests.length).toBe(1)
  expect(executionRequests[0].content.query).toContain(':nickname')
  expect(executionRequests[0].content.query_parameters).toEqual([{
    name: 'nickname',
    type: 'string',
    default: 'Ada',
    title: '昵称'
  }])
  expect(executionRequests[0].parameters).toEqual({ nickname: 'Grace' })
  await expect(page.getByText('Ada', { exact: true })).toBeVisible()
})

async function installMockBackend(page, { resultKind, engines = [ENGINE], executionRequests = [] }) {
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
    if (path === '/api/v1/develop/executions' && request.method() === 'POST') {
      executionRequests.push(request.postDataJSON())
      return fulfillJSON(route, { execution_id: EXECUTION_ID })
    }
    if (path === `/api/v1/develop/executions/${EXECUTION_ID}`) {
      return fulfillJSON(route, executionResult(resultKind))
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

function executionResult(resultKind) {
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

  return {
    execution_id: EXECUTION_ID,
    status: 'success',
    progress: 100,
    execution_time_ms: 18,
    metadata: {
      result: {
        columns: ['id', 'name'],
        rows_count: 3,
        rows_affected: 0,
        result_kind: resultKind,
        result_limit: 2,
        truncated: true,
        graph_data: graphData,
        summary: {
          preview_rows: [
            { id: 1, name: 'Ada' },
            { id: 2, name: 'Grace' }
          ]
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
