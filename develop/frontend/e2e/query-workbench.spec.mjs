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
        result_kinds: ['table', 'graph']
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

  test('moves Catalog into a drawer and keeps execution context in graph mode', async ({ page }) => {
    await installMockBackend(page, { resultKind: 'graph' })
    await page.goto('/sql')

    await expect(page.locator('.catalog-panel')).toHaveCount(0)
    const catalogButton = page.getByRole('button', { name: 'Catalog', exact: true })
    await expect(catalogButton).toHaveCount(1)
    await catalogButton.click()
    const drawer = page.getByRole('dialog', { name: 'Catalog', exact: true })
    await expect(drawer).toBeVisible()
    await expect(drawer.getByRole('treeitem', { name: ENGINE.name, exact: true })).toBeVisible()
    await drawer.getByRole('button', { name: 'Close this dialog', exact: true }).click()

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

async function installMockBackend(page, { resultKind }) {
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
      return fulfillJSON(route, [ENGINE])
    }
    if (path === `/api/v1/develop/engines/${ENGINE.id}/sample-query`) {
      return fulfillJSON(route, { query: 'SELECT id, name FROM public.customers', language: 'sql' })
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}`) {
      return fulfillJSON(route, resourceTree())
    }
    if (path === '/api/v1/develop/executions' && request.method() === 'POST') {
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
