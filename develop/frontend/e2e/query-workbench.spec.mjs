import { expect, test } from '@playwright/test'
import { developAuthContext } from './developAuthContext.js'

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

test('requests explicit Develop plaintext access through the shared dialog', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table' })
  const auth = structuredClone(developAuthContext)
  auth.authorization.role_assignments[0].permissions.push('security.protection_access_request.create', 'security.protection_access_request.read')
  await page.route('**/api/v1/system/auth/context', route => fulfillJSON(route, auth))
  await page.route('**/api/v1/meta/items/*', route => fulfillJSON(route, { fingerprint: 'customers-fingerprint' }))
  const submitted = []
  await page.route('**/api/v1/security/protection-access-request-targets?*', route => fulfillJSON(route, { data: [{
    assessment_id: 'phone-assessment', requestable: true, component: { key: 'phone' }, projection_acknowledged: true
  }] }))
  await page.route('**/api/v1/security/protection-access-requests', route => {
    submitted.push(route.request().postDataJSON())
    return fulfillJSON(route, { id: 'develop-request' })
  })
  await page.goto('/sql')
  const catalog = page.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await page.getByRole('button', { name: '申请查看字段原值', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '申请查看字段原值', exact: true })
  await expect(dialog.getByRole('checkbox', { name: 'Develop 开发查询', exact: true })).toBeChecked()
  await expect(dialog.getByRole('checkbox', { name: 'Manager 数据预览', exact: true })).not.toBeChecked()
  await dialog.locator('textarea').fill('在开发查询中核验联系方式')
  await dialog.getByRole('button', { name: '提交申请', exact: true }).click()
  await expect.poll(() => submitted.length).toBe(1)
  expect(submitted[0].scopes).toEqual([{ consumer_owner: 'develop', action: 'query' }])
  expect(submitted[0]).not.toHaveProperty('subject_id')
  expect(submitted[0]).not.toHaveProperty('consumer_owner')
})

test('shows the actual missing-table preparation failure with an actionable correction in the workbench', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table', errorDetails: { category: 'execution_failed', code: 'table_not_found', diagnostic: {
    schema_version: 'addp.execution-failure/v3', stage: 'preparation', reason: 'table_not_found',
    sql: { object: { kind: 'table', name: 'public.farmla1nd' }, position: { line: 1, column: 15 } }
  } } })
  await page.goto('/sql')
  const catalog = page.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await catalog.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await page.getByRole('button', { name: '执行', exact: true }).click()
  const diagnostic = page.locator('.execution-failure')
  await expect(diagnostic).toContainText('查询引用的数据表不存在或当前连接不可见。')
  await expect(diagnostic).toContainText('准备执行')
  await expect(diagnostic).toContainText('核对数据表参数、Schema 及表的读取权限后重新执行。')
  await expect(diagnostic).not.toContainText('交给管理员')
  await expect(diagnostic).toContainText('数据表 public.farmla1nd')
  await expect(diagnostic).toContainText('第 1 行，第 15 列')
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await diagnostic.getByRole('button', { name: '复制诊断信息', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('public.farmla1nd')
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('第 1 行，第 15 列')
  await page.setViewportSize({ width: 760, height: 700 })
  await expectNoDocumentOverflow(page)
  await expect(diagnostic.getByRole('link', { name: '查看监控错误详情', exact: true })).toBeVisible()
})

for (const [code, reason, explanation, action] of [
  ['sqlstate_42702', 'ambiguous_column', '字段名存在歧义', '数据表名或别名前缀'],
  ['sqlstate_42883', 'function_or_operator_not_found', '函数或运算符不存在', '参数数量和参数类型'],
  ['sqlstate_22012', 'division_by_zero', '除零错误', 'NULLIF(除数, 0)']
]) test(`shows useful SQL execution guidance for ${reason}`, async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table', errorDetails: { category: 'execution_failed', code,
    diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'execution', reason } } })
  await page.goto(`/executions/${EXECUTION_ID}`)
  const diagnostic = page.locator('.execution-failure')
  await expect(diagnostic).toContainText(explanation)
  await expect(diagnostic).toContainText(action)
  await expect(diagnostic).toContainText(code)
})

for (const [locale, explanation, action] of [
  ['zh-cn', '执行已被阻止', '请核对当前账号的数据访问资格'],
  ['en', 'Execution was blocked', "Check the current account's data access eligibility"]
]) for (const stage of ['preparation', 'result']) test(`explains protection refusal in ${locale} at ${stage} without runtime values`, async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table', locale, errorDetails: {
    category: 'execution_failed', code: 'develop.query.protection_required', message: 'private-value SELECT phone',
    diagnostic: { schema_version: 'addp.execution-failure/v3', stage, reason: 'data_protection_required' }
  } })
  await page.goto(`/executions/${EXECUTION_ID}`)
  const diagnostic = page.locator('.execution-failure')
  await expect(diagnostic).toContainText(explanation)
  await expect(diagnostic).toContainText(action)
  await expect(diagnostic).toContainText('develop.query.protection_required')
  await expect(diagnostic).not.toContainText('private-value')
  await expect(diagnostic).not.toContainText('SELECT phone')
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(diagnostic).toBeVisible()
  const width = await diagnostic.evaluate(element => ({ scroll: element.scrollWidth, client: element.clientWidth }))
  expect(width.scroll).toBeLessThanOrEqual(width.client + 1)
})

for (const embedded of [false, true]) test(`keeps the query draft and opens the same failure in a new Monitor tab (${embedded ? 'Console iframe' : 'standalone'})`, async ({ page }) => {
  const errorDetails = { category: 'execution_failed', code: 'develop.query.snapshot_invalid', diagnostic: {
    schema_version: 'addp.execution-failure/v3', stage: 'validation', reason: 'query_template_invalid'
  } }
  await installMockBackend(page, { resultKind: 'table', errorDetails })
  if (embedded) {
    await page.route('**/failure-navigation-harness', route => route.fulfill({
      contentType: 'text/html', body: `<style>body{margin:0}iframe{display:block;width:100%;height:900px;border:0}</style>
        <main>Console fixture</main><output id="navigation-count">0</output>
        <script type="module">
          import { createIframeAuthCoordinator } from '/@fs${new URL('../../../common-frontend/basic/src/auth/authSession.js', import.meta.url).pathname}'
          import { registerConsoleBridgeHandler } from '/@fs${new URL('../../../common-frontend/basic/src/utils/consoleBridge.js', import.meta.url).pathname}'
          createIframeAuthCoordinator({
            allowedOrigins: [location.origin], getToken: () => 'develop-e2e-token',
            getExpiresAt: () => Date.now() + 300000,
            refreshToken: async () => 'develop-e2e-token', logout: async () => {}
          })
          registerConsoleBridgeHandler('console-navigation', request => {
            if (!request.synchronized) {
              const count = document.querySelector('#navigation-count')
              count.textContent = String(Number(count.textContent) + 1)
            }
            return { cancelled: true }
          })
          const frame = document.createElement('iframe')
          frame.src = '/sql'
          document.body.appendChild(frame)
        </script>`
    }))
  }
  await page.goto(embedded ? '/failure-navigation-harness' : '/sql')
  const workbench = embedded ? page.frameLocator('iframe') : page
  const catalog = workbench.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await catalog.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await workbench.getByRole('button', { name: '执行', exact: true }).click()
  const diagnostic = workbench.locator('.execution-failure')
  await expect(diagnostic).toContainText('查询内容与参数定义不一致')
  await expect(diagnostic).toContainText('校验执行配置')
  await expect(diagnostic).toContainText('develop.query.snapshot_invalid')
  await expect(diagnostic).toContainText(EXECUTION_ID)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await diagnostic.getByRole('button', { name: '复制诊断信息', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain(EXECUTION_ID)
  await page.setViewportSize({ width: 760, height: 700 })
  await expectNoDocumentOverflow(page)
  await expect(page.getByRole('button', { name: '查看执行详情', exact: true })).toHaveCount(0)
  await page.context().route(`**/monitor/executions?execution_id=${EXECUTION_ID}`, route => route.fulfill({
    contentType: 'text/html', body: '<main>Monitor execution destination</main>'
  }))
  const originalUrl = page.url()
  const queryBefore = await workbench.locator('.view-lines').innerText()
  const monitorLink = diagnostic.getByRole('link', { name: '查看监控错误详情', exact: true })
  await expect(monitorLink).toHaveAttribute('target', '_blank')
  await expect(monitorLink).toHaveAttribute('rel', 'noopener noreferrer')
  const [monitorPage] = await Promise.all([page.waitForEvent('popup'), monitorLink.click()])
  await expect(monitorPage).toHaveURL(new RegExp(`/monitor/executions\\?execution_id=${EXECUTION_ID}$`))
  await expect(monitorPage.getByText('Monitor execution destination')).toBeVisible()
  expect(await monitorPage.evaluate(() => window.opener === null)).toBe(true)
  expect(page.url()).toBe(originalUrl)
  if (embedded) await expect(page.locator('#navigation-count')).toHaveText('0')
  await expect(workbench.locator('.view-lines')).toHaveText(queryBefore)
  await expect(diagnostic).toContainText(EXECUTION_ID)
  await expect(page.getByText('有未保存的修改', { exact: true })).toHaveCount(0)
  await monitorPage.close()
  await page.goto(`/executions/${EXECUTION_ID}`)
  await expect(page.locator('.execution-failure')).toContainText('develop.query.snapshot_invalid')
  await expect(page.locator('.execution-failure')).toContainText('查询内容与参数定义不一致')
  await expect(page.getByRole('tab', { name: '错误信息', exact: true })).toHaveCount(0)
  const ownerUrl = page.url()
  const [ownerMonitorPage] = await Promise.all([
    page.waitForEvent('popup'),
    page.locator('.execution-failure').getByRole('link', { name: '查看监控错误详情', exact: true }).click()
  ])
  await expect(ownerMonitorPage).toHaveURL(new RegExp(`/monitor/executions\\?execution_id=${EXECUTION_ID}$`))
  expect(page.url()).toBe(ownerUrl)
  await ownerMonitorPage.close()
})

test('keeps a Notebook runtime traceback in Owner logs after removing the generic error tab', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table', errorDetails: { category: 'execution_failed', code: 'develop.script.execution_failed' },
    executionType: 'script', executionMetadata: { result: { traceback: 'Cell 4: controlled runtime traceback fixture' } } })
  await page.goto(`/executions/${EXECUTION_ID}`)
  await page.getByRole('tab', { name: '执行日志', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '执行日志', exact: true }).locator('.json-result')).toHaveText('Cell 4: controlled runtime traceback fixture')
  await expect(page.locator('.execution-failure')).toHaveCount(1)
})

test('keeps current query delivery separate from the masked execution history', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table', resultRows: [{ id: 1, name: '***' }], currentResultRows: [{ id: 1, name: 'current-only' }] })
  await page.goto('/sql')
  const catalog = page.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await catalog.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await page.getByRole('button', { name: '执行', exact: true }).click()
  await expect(page.locator('.query-result')).toContainText('current-only')
  await page.goto(`/executions/${EXECUTION_ID}`)
  await expect(page.locator('.query-result')).toContainText('***')
  await expect(page.locator('.query-result')).not.toContainText('current-only')
})

test('submits a complete multiline JSON query and observes its browser execution result', async ({ page }) => {
  const engine = { ...ENGINE, engine_type: 'elasticsearch', capabilities: {
    compute: { query: { supported: true, languages: ['es_dsl'], default_language: 'es_dsl', result_kinds: ['table'] } }
  } }
  await installMockBackend(page, { resultKind: 'table', engines: [engine], resultRows: [{ id: '9007199254740993', name: 'sample' }] })
  await page.goto('/sql')
  const catalog = page.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  const query = { query: { term: { tags: 'sample' } }, size: 25, sort: [{ order_id: 'asc' }], _source: ['order_id', 'customer'] }
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.evaluate(text => navigator.clipboard.writeText(text), JSON.stringify(query, null, 2))
  await page.locator('.monaco-editor').click({ position: { x: 70, y: 20 } })
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('ControlOrMeta+V')
  const preflight = page.waitForRequest(request => new URL(request.url()).pathname === '/api/v1/develop/query-preflight')
  const created = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/executions' && response.request().method() === 'POST')
  const finished = page.waitForResponse(async response => {
    if (!/^\/api\/v1\/develop\/executions\/[^/]+\/result$/.test(new URL(response.url()).pathname) ||
      response.request().method() !== 'GET' || !response.ok()) return false
    return ['success', 'failed', 'timeout', 'cancelled'].includes((await response.json()).status)
  })
  await page.getByRole('button', { name: '执行', exact: true }).click()
  const submitted = (await preflight).postDataJSON()
  expect(JSON.parse(submitted.query)).toEqual(query)
  expect(submitted.query_type).toBe('es_dsl')
  const execution = await (await created).json()
  const resultResponse = await finished
  expect(new URL(resultResponse.url()).pathname).toBe(`/api/v1/develop/executions/${execution.execution_id}/result`)
  const completed = await resultResponse.json()
  expect(completed.execution_id).toBe(execution.execution_id)
  expect(completed.status).toBe('success')
  const resultTable = page.locator('.query-result .result-table.el-table')
  await expect(resultTable).toHaveCount(1)
  await expect(resultTable).toContainText('9007199254740993')
})

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

test('graph results follow every theme without replacing the canvas or clearing selection', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'graph' })
  await page.goto('/sql')
  const catalog = page.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await catalog.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await page.getByRole('button', { name: '执行', exact: true }).click()
  const canvas = page.locator('.graph-canvas canvas')
  await expect(canvas).toBeVisible()
  const originalCanvas = await canvas.elementHandle()
  // Select a rendered node using its actual fill pixels, independent of force-layout positions.
  await expect(async () => {
    const position = await canvas.evaluate(element => {
      const style = getComputedStyle(element)
      const palette = Array.from({ length: 12 }, (_, i) => style.getPropertyValue(`--addp-graph-category-${i + 1}`).trim().toLowerCase())
      const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
      const bounds = element.getBoundingClientRect()
      for (let i = 0; i < pixels.length; i += 4) {
        const hex = '#' + [...pixels.slice(i, i + 3)].map(value => value.toString(16).padStart(2, '0')).join('')
        if (pixels[i + 3] === 255 && palette.includes(hex)) {
          return { x: (i / 4 % element.width) * bounds.width / element.width, y: Math.floor(i / 4 / element.width) * bounds.height / element.height + 8 }
        }
      }
      return null
    })
    expect(position).not.toBeNull()
    await canvas.click({ position })
    await expect(page.locator('.props-panel')).toBeVisible()
  }).toPass()
  const selectedProperties = await page.locator('.props-panel').innerText()

  for (const theme of ['dark', 'blue', 'purple', 'light']) {
    await page.evaluate(theme => {
      document.documentElement.classList.remove('dark', 'blue', 'purple')
      if (theme !== 'light') document.documentElement.classList.add('dark')
      if (theme === 'blue' || theme === 'purple') document.documentElement.classList.add(theme)
    }, theme)
    const colors = await page.locator('.graph-result-view').evaluate(element => {
      const style = getComputedStyle(element)
      const resolve = variable => {
        const probe = document.createElement('span')
        probe.style.color = style.getPropertyValue(variable)
        element.append(probe)
        const color = getComputedStyle(probe).color
        probe.remove()
        return color
      }
      return { background: resolve('--addp-bg-primary'), toolbar: resolve('--addp-bg-secondary') }
    })
    await expect(page.locator('.graph-canvas')).toHaveCSS('background-color', colors.background)
    await expect(page.locator('.graph-toolbar')).toHaveCSS('background-color', colors.toolbar)
    await expect(page.locator('.props-panel')).toHaveCSS('background-color', colors.background)
    await expect(page.locator('.props-panel')).toHaveText(selectedProperties, { useInnerText: true })
    // Check actual canvas pixels: CSS alone cannot recolor the G6 edges.
    await expect.poll(() => canvas.evaluate(element => {
      const color = getComputedStyle(element).getPropertyValue('--addp-graph-edge-default').trim()
      const rgb = color.slice(1).match(/../g).map(value => parseInt(value, 16))
      const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
      for (let i = 0; i < pixels.length; i += 4) {
        if (pixels[i + 3] === 255 && rgb.every((value, index) => pixels[i + index] === value)) return true
      }
      return false
    })).toBe(true)
    expect(await canvas.evaluate((element, original) => element === original, originalCanvas)).toBe(true)
  }
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

test('formats ES DSL without rounding a long and submits the selected index', async ({ page }) => {
  const engine = {
    ...ENGINE, id: 14, name: 'Elasticsearch Demo', engine_type: 'elasticsearch',
    capabilities: { compute: { query: { supported: true, languages: ['es_dsl'], default_language: 'es_dsl', result_kinds: ['table'], read_only: true } } }
  }
  const executionRequests = []
  await installMockBackend(page, { resultKind: 'table', engines: [engine], metaEngines: [engine], executionRequests })
  const indexLocator = 'addp://engine/14/path/orders.v1?type=index&item_id=1401'
  await page.route('**/api/v1/meta/resource-tree/14**', route => fulfillJSON(route, {
    id: 'addp://engine/14/path?type=service', locator: 'addp://engine/14/path?type=service', type: 'service', label: engine.name,
    children: [{ id: indexLocator, locator: indexLocator, label: 'orders.v1', type: 'index', children: [] }]
  }))
  await page.route('**/sample-query**', route => fulfillJSON(route, {
    query: '{"query":{"term":{"order_id":9007199254740993}},"size":10}', language: 'es_dsl'
  }))
  await page.goto('/sql')
  const resources = page.locator('.catalog-panel')
  await resources.getByRole('treeitem', { name: engine.name, exact: true }).click()
  await resources.getByRole('treeitem', { name: 'orders.v1', exact: true }).click()
  await resources.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await expect(page.locator('.toolbar-primary').getByText('ES_DSL', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '格式化', exact: true }).click()
  await page.getByRole('button', { name: '执行', exact: true }).click()
  await expect.poll(() => executionRequests.length).toBe(1)
  expect(executionRequests[0].content.query_type).toBe('es_dsl')
  expect(executionRequests[0].content.target_locator).toBe(indexLocator)
  expect(executionRequests[0].content.query).toContain('9007199254740993')
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

test('extracted parameters immediately show and hide empty text and zero integer defaults', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table' })
  await page.goto('/sql')
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.evaluate(query => navigator.clipboard.writeText(query), 'SELECT :text AS bound_text FROM public.customers WHERE id >= :minimum')
  await page.locator('.monaco-editor').click()
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('ControlOrMeta+V')
  await page.locator('.editor-panel').getByRole('button', { name: /查询参数/ }).click()
  const panel = page.locator('.query-parameter-panel')
  await panel.getByRole('button', { name: '从查询提取参数', exact: true }).click()
  const text = panel.locator('.parameter-item').nth(0)
  const integer = panel.locator('.parameter-item').nth(1)
  await integer.locator('.el-select').click()
  await page.getByRole('option', { name: '整数', exact: true }).click()
  await text.locator('.scalar-default-value .el-checkbox').click()
  await expect(text.locator('.scalar-default-value .el-input__inner')).toBeVisible()
  await expect(text.locator('.scalar-default-value .el-input__inner')).toHaveValue('')
  await integer.locator('.scalar-default-value .el-checkbox').click()
  await expect(integer.locator('.scalar-default-value .el-input__inner')).toHaveValue('0')
  await expect(panel.getByText('2 个参数未设置默认值；任务可以保存，执行时再填写。', { exact: true })).toHaveCount(0)
  await text.locator('.scalar-default-value .el-checkbox').click()
  await expect(text.locator('.scalar-default-value .el-input__inner')).toHaveCount(0)
  await integer.locator('.scalar-default-value .el-checkbox').click()
  await expect(integer.locator('.scalar-default-value .el-input__inner')).toHaveCount(0)
  await expect(panel.getByText('2 个参数未设置默认值；任务可以保存，执行时再填写。', { exact: true })).toBeVisible()
})

test('clears cached professional results when execution access is revoked', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table' })
  let revoked = false
  await page.route(`**/api/v1/develop/executions/${EXECUTION_ID}`, route => {
    if (revoked) return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: '执行记录不可见', error_code: 'execution_not_found' }) })
    return fulfillJSON(route, { ...executionResult('table'), task_type: 'query', execution_config: { inputs: { private_parameter: 'private-result-sentinel' } } })
  })
  await page.goto(`/executions/${EXECUTION_ID}?tab=inputs`)
  await expect(page.getByText('private-result-sentinel', { exact: false })).toBeVisible()
  revoked = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('private-result-sentinel', { exact: false })).toHaveCount(0)
  await expect(page.getByText('Ada', { exact: true })).toHaveCount(0)
})

async function installMockBackend(page, {
  resultKind,
  engines = [ENGINE],
  metaEngines = [ENGINE],
  executionRequests = [],
  copilotRequests = [],
  resourceTreeRequests = [],
  resultRows,
  currentResultRows = resultRows,
  errorDetails,
  executionType = 'query',
  locale = 'zh-cn',
  executionMetadata = {}
}) {
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), locale)
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'develop-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'develop-e2e' })
    }
    if (path === '/api/v1/system/auth/context') return fulfillJSON(route, developAuthContext)
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
    if (path === `/api/v1/develop/executions/${EXECUTION_ID}` || path === `/api/v1/develop/executions/${EXECUTION_ID}/result`) {
      if (errorDetails) return fulfillJSON(route, { execution_id: EXECUTION_ID, module: 'develop', task_type: executionType, status: 'failed', progress: 100, execution_time_ms: 0, metadata: executionMetadata, error_details: errorDetails })
      return fulfillJSON(route, { ...executionResult(resultKind, path.endsWith("/result") ? currentResultRows : resultRows), module: "develop", task_type: executionType })
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

test('query editor uses shared protection for route updates and leaving the page', async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table' })
  await page.goto('/sql?action=create')
  await expect(page.getByRole('heading', { name: '查询开发', exact: true })).toBeVisible()
  await expect(page.locator('.catalog-panel').getByRole('treeitem', { name: ENGINE.name, exact: true })).toBeVisible()
  await page.locator('.query-workbench').evaluate(el => {
    el.__vueParentComponent.setupState.queryContent = 'SELECT 42'
    void el.__vueParentComponent.proxy.$router.push('/sql?action=edit&id=999')
  })
  const dialog = page.getByRole('dialog', { name: '有未保存的修改', exact: true })
  await dialog.getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page).toHaveURL(/\/sql\?action=create$/)
  await page.locator('.query-workbench').evaluate(el => { void el.__vueParentComponent.proxy.$router.push('/tasks') })
  await dialog.getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/\/tasks$/)
})
