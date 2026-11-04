import { expect, test } from '@playwright/test'
import { developAuthContext } from './developAuthContext.js'
import { readFileSync } from 'node:fs'

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
        identifier_quotes: { sql: '"' },
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
        identifier_quotes: { sql: '"' },
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

test('query editor follows platform themes without replacing the model or undo history', async ({ page }, testInfo) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.emulateMedia({ colorScheme: 'light' })
  await page.addInitScript(() => localStorage.setItem('theme-mode', 'system'))
  await installMockBackend(page, { resultKind: 'table' })
  await page.goto('/sql')
  const container = page.locator('.monaco-editor-container')
  await expect(container.locator('.monaco-editor')).toBeVisible()
  const colorsMatch = () => container.evaluate(element => {
    const css = getComputedStyle(element)
    const color = variable => {
      const probe = document.createElement('span')
      probe.style.color = css.getPropertyValue(variable).trim()
      element.appendChild(probe)
      const value = getComputedStyle(probe).color
      probe.remove()
      return value
    }
    const lineNumber = element.querySelector('.line-numbers')
    return getComputedStyle(element.querySelector('.monaco-editor-background')).backgroundColor === color('--addp-bg-primary') &&
      getComputedStyle(element.querySelector('.margin')).backgroundColor === color('--addp-bg-primary') &&
      getComputedStyle(lineNumber).color === color(lineNumber.classList.contains('active-line-number') ? '--addp-text-secondary' : '--addp-text-tertiary')
  })
  await expect.poll(colorsMatch).toBe(true)
  await container.locator('.monaco-editor').click({ position: { x: 80, y: 18 } })
  const original = 'SELECT 1 -- 保留查询'
  await page.keyboard.insertText(original)
  await container.evaluate(element => element.__vueParentComponent.setupState.editor.pushUndoStop())
  await page.keyboard.insertText(' theme-edit')
  await page.keyboard.press('Home')
  await page.keyboard.press('Shift+ArrowRight')
  const snapshot = () => container.evaluate(element => {
    const editor = element.__vueParentComponent.setupState.editor
    return { model: editor.getModel().id, value: editor.getValue(), selection: editor.getSelection(), view: editor.saveViewState() }
  })
  const before = await snapshot()
  for (const scheme of ['dark', 'light']) {
    await page.emulateMedia({ colorScheme: scheme })
    await expect.poll(() => page.locator('html').evaluate(element => element.classList.contains('dark'))).toBe(scheme === 'dark')
    await expect.poll(colorsMatch).toBe(true)
    expect(await snapshot()).toEqual(before)
  }
  for (const theme of ['light', 'dark', 'blue', 'purple']) {
    await page.evaluate(mode => {
      document.documentElement.className = mode === 'light' ? '' : `dark${mode === 'dark' ? '' : ` ${mode}`}`
    }, theme)
    await expect.poll(colorsMatch).toBe(true)
    expect(await snapshot()).toEqual(before)
    await page.getByRole('button', { name: '显示补全', exact: true }).click()
    await expect(page.locator('.suggest-widget.visible')).toBeVisible()
    await expect.poll(() => container.evaluate(element => {
      const probe = document.createElement('span')
      probe.style.color = 'var(--el-color-primary-light-8)'
      element.appendChild(probe)
      const selected = getComputedStyle(probe).color
      probe.remove()
      return getComputedStyle(element.querySelector('.suggest-widget .monaco-list-row.focused')).backgroundColor === selected
    })).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`query-editor-${theme}.png`) })
    await page.keyboard.press('Escape')
  }
  await page.keyboard.press('ControlOrMeta+z')
  await expect.poll(() => container.evaluate(element => element.__vueParentComponent.exposed.getValue())).toBe(original)
  await page.keyboard.press('ControlOrMeta+Shift+z')
  await expect.poll(() => container.evaluate(element => element.__vueParentComponent.exposed.getValue())).toBe(before.value)
  expect(errors).toEqual([])
})

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
  ['zh-cn', '当前计算或组合输出涉及受保护字段', '请改为直接读取受保护字段'],
  ['en', 'computed or combined output uses protected fields', 'Read the protected fields directly']
]) for (const stage of ['preparation', 'result']) test(`explains protection refusal in ${locale} at ${stage} without runtime values`, async ({ page }) => {
  await installMockBackend(page, { resultKind: 'table', locale, errorDetails: {
    category: 'execution_failed', code: 'develop.query.protection_required', message: 'private-value SELECT phone',
    diagnostic: { schema_version: 'addp.execution-failure/v3', stage, reason: 'protection_computed_output_unsupported' }
  } })
  await page.goto(`/executions/${EXECUTION_ID}`)
  const diagnostic = page.locator('.execution-failure')
  await expect(diagnostic).toContainText(explanation)
  await expect(diagnostic).toContainText(action)
  await expect(diagnostic).toContainText('develop.query.protection_required')
  await expect(diagnostic).not.toContainText('private-value')
  await expect(diagnostic).not.toContainText('SELECT phone')
  await expect(diagnostic).not.toContainText(/访问资格|access eligibility/i)
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

for (const readSession of [false, true]) test(`full export follows the engine read-session capability (${readSession})`, async ({ page }) => {
  const engine = { ...ENGINE, capabilities: { compute: { query: { ...ENGINE.capabilities.compute.query, read_session: readSession } } } }
  await installMockBackend(page, { resultKind: 'table', engines: [engine] })
  await page.goto('/sql', { waitUntil: 'networkidle' })
  const catalog = page.locator('.catalog-panel')
  await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
  await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
  await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
  await catalog.getByRole('button', { name: '生成查询模板', exact: true }).click()
  await page.getByRole('button', { name: '执行', exact: true }).click()
  await expect(page.getByText('Ada', { exact: true })).toBeVisible()
  const exportButton = page.getByRole('button', { name: '导出全部结果', exact: true })
  if (readSession) {
    await expect(exportButton).toBeEnabled()
    await exportButton.click()
    await expect(page.getByRole('dialog', { name: '导出全部结果', exact: true })).toBeVisible()
  } else {
    await expect(exportButton).toBeDisabled()
    await exportButton.locator('..').hover()
    await expect(page.getByRole('tooltip', { name: '当前引擎暂不支持导出完整查询结果；普通查询仍可使用。', exact: true })).toBeVisible()
    await expect(page.getByRole('dialog', { name: '导出全部结果', exact: true })).toHaveCount(0)
  }
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
  await expect.poll(() => page.locator('.graph-result-view').evaluate(element => {
    const graph = element.__vueParentComponent.setupState.graphInstance
    const fontFamily = getComputedStyle(element).fontFamily
    return [...graph.getNodes(), ...graph.getEdges()].every(item =>
      item.getContainer().find(shape => shape.get('type') === 'text')?.attr('fontFamily') === fontFamily)
  })).toBe(true)
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

for (const engineType of ['postgresql', 'spark']) {
  test(`PG table item count never requests directory children and fields still complete in ${engineType}`, async ({ page }) => {
    const target = structuredClone(engineType === 'postgresql' ? ENGINE : DUCKDB_RUNTIME)
    if (engineType === 'spark') {
      Object.assign(target, { id: 19, engine_type: 'spark', name: 'Spark field regression' })
      target.capabilities.compute.query.identifier_quotes.sql = '`'
      target.capabilities.compute.query.federation.source_engine_types = ['postgresql', 'mysql']
    }
    await installMockBackend(page, { resultKind: 'table', engines: [target], metaEngines: [ENGINE] })
    const root = resourceTree()
    root.locator += '&node_id=103'
    root.id = root.locator
    const schema = root.children[0]
    schema.locator += '&node_id=104'
    schema.id = schema.locator
    schema.hasChildren = true
    const table = schema.children[0]
    table.hasChildren = false
    table.metadata = { item_id: 1101, item_count: 1, has_children: false }
    const invalidDirectoryReads = []
    const fieldReads = []
    await page.route(`**/api/v1/meta/resource-tree/${ENGINE.id}**`, route => {
      const url = new URL(route.request().url())
      if (url.pathname.endsWith('/node')) {
        const locator = url.searchParams.get('locator')
        if (locator === root.locator) return fulfillJSON(route, { children: [{ ...schema, children: [] }] })
        if (locator === schema.locator) return fulfillJSON(route, schema)
        invalidDirectoryReads.push(locator)
        return fulfillJSON(route, { error: 'resource tree node requires locator node_id' }, 400)
      }
      return fulfillJSON(route, { ...root, children: [{ ...schema, children: [] }] })
    })
    await page.route('**/api/v1/meta/items/1101/fields', route => {
      fieldReads.push(route.request().method())
      return fulfillJSON(route, [{ name: 'id', type: { name: 'bigint' } }, { name: '客户 名', type: { name: 'string' } }])
    })
    await page.goto('/sql')
    const catalog = page.locator('.catalog-panel')
    await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
    await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
    const leaf = catalog.getByRole('treeitem', { name: 'customers', exact: true })
    const expansion = leaf.locator('.el-tree-node__expand-icon')
    // Exercise the erroneous control if it reappears; the real Meta API rejects item locators here.
    if (await expansion.isVisible()) await expansion.click()
    await leaf.dblclick()
    await expect.poll(() => fieldReads.length).toBe(1)
    const quote = engineType === 'postgresql' ? '"' : '`'
    const parts = engineType === 'postgresql' ? ['public', 'customers'] : ['PostgreSQL_Demo', 'public', 'customers']
    await expect(page.locator('.monaco-editor .view-lines')).toHaveText(parts.map(part => quote + part + quote).join('.'))
    await page.getByRole('button', { name: '显示补全', exact: true }).click()
    await expect(page.getByRole('option', { name: /客户 名/ })).toBeVisible()
    expect(invalidDirectoryReads).toEqual([])
    await expect(expansion).not.toBeVisible()
    expect(fieldReads).toEqual(['GET'])
    await expect(page.getByText('Request failed with status code 400', { exact: true })).toHaveCount(0)
  })
}

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
  await expect(page.locator('.monaco-editor .view-lines')).toContainText('"PostgreSQL_Demo"."public"."customers"')
  expect(resourceTreeRequests).not.toContain(`/api/v1/meta/resource-tree/${DUCKDB_RUNTIME.id}`)
})

for (const [engineType, quote] of [['spark', '`'], ['duckdb', '"']]) {
  test(`generates the selected PG source template for ${engineType} and keeps the compute target`, async ({ page }) => {
    const runtime = structuredClone(DUCKDB_RUNTIME)
    Object.assign(runtime, { id: 19, engine_type: engineType, name: `${engineType} query cluster` })
    runtime.capabilities.compute.query.identifier_quotes.sql = quote
    const executionRequests = []
    await installMockBackend(page, { resultKind: 'table', engines: [runtime], metaEngines: [ENGINE], executionRequests })
    const samples = []
    page.on('request', request => { if (request.url().includes('/sample-query')) samples.push(new URL(request.url())) })
    await page.goto('/sql')
    const tree = page.locator('.catalog-panel')
    await tree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
    await tree.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
    await tree.getByRole('treeitem', { name: 'customers', exact: true }).click()
    await tree.getByRole('button', { name: '生成查询模板', exact: true }).click()
    await expect.poll(() => samples.length).toBe(1)
    expect(samples[0].pathname).toBe('/api/v1/develop/engines/19/sample-query')
    expect(samples[0].searchParams.get('locator')).toBe(resourceTree().children[0].children[0].locator)
    const reference = ['PostgreSQL_Demo', 'public', 'customers'].map(part => quote + part + quote).join('.')
    const expected = `SELECT id, name FROM ${reference} LIMIT 10`
    await expect.poll(() => page.locator('.monaco-editor-container').evaluate(el => el.__vueParentComponent.setupState.editor.getValue())).toBe(expected)
    await expect(page.locator('.engine-select')).toContainText(runtime.name)
    await page.getByRole('button', { name: '执行', exact: true }).click()
    await expect.poll(() => executionRequests.length).toBe(1)
    expect(executionRequests[0].execution_config.engine_id).toBe(runtime.id)
    expect(executionRequests[0].content.query).toBe(expected)
    expect(executionRequests[0].content.target_locator).toBeUndefined()
    await expect(page.getByText('Ada', { exact: true })).toBeVisible()
  })
}

for (const [engineType, quote] of [['spark', '`'], ['duckdb', '"']]) {
  test(`inserts exact owner segments for ${engineType} without a display-path fallback`, async ({ page }) => {
    const runtime = structuredClone(DUCKDB_RUNTIME)
    Object.assign(runtime, { id: 19, engine_type: engineType, name: `${engineType} query cluster` })
    runtime.capabilities.compute.query.identifier_quotes.sql = quote
    await installMockBackend(page, { resultKind: 'table', engines: [runtime], metaEngines: [ENGINE] })
    const locator = resourceTree().children[0].children[0].locator
    const parts = ['PostgreSQL_Demo', ' schema.with dot ', ' 中文"table` ']
    const calls = []
    const executionRequests = []
    await page.route('**/api/v1/develop/executions', route => {
      executionRequests.push(route.request().postDataJSON())
      return fulfillJSON(route, { execution_id: EXECUTION_ID })
    })
    await page.route('**/api/v1/manager/resource-facts?*', route => {
      calls.push(new URL(route.request().url()).searchParams.get('locator'))
      return fulfillJSON(route, { engine_id: ENGINE.id, item_id: 1101, query_references: { federated_sql: { parts } } })
    })
    await page.goto('/sql')
    await expect(page.locator('.engine-select')).toContainText(runtime.name)
    const tree = page.locator('.catalog-panel')
    await tree.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
    await tree.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
    await tree.getByRole('treeitem', { name: 'customers', exact: true }).dblclick()
    const expected = parts.map(part => quote + part.replaceAll(quote, quote + quote) + quote).join('.')
    await expect.poll(() => page.locator('.monaco-editor-container').evaluate(el => el.__vueParentComponent.setupState.editor.getValue())).toBe(expected)
    expect(calls).toContain(locator)
    const editor = page.locator('.monaco-editor-container .inputarea')
    await editor.press('ControlOrMeta+A')
    await page.keyboard.insertText(`SELECT id FROM ${expected}`)
    await page.getByRole('button', { name: '执行', exact: true }).click()
    await expect.poll(() => executionRequests.length).toBe(1)
    expect(executionRequests[0].execution_config.engine_id).toBe(runtime.id)
    expect(executionRequests[0].content.target_locator).toBeUndefined()
    expect(executionRequests[0].content.query).toBe(`SELECT id FROM ${expected}`)
    await expect(page.getByText('Ada', { exact: true })).toBeVisible()
  })
}

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
    if (path === '/api/v1/manager/resource-facts') {
      const locator = new URL(request.url()).searchParams.get('locator')
      if (locator === resourceTree().children[0].children[0].locator) return fulfillJSON(route, {
        engine_id: ENGINE.id, item_id: 1101, source_engine_type: 'postgresql',
        query_references: { sql: { parts: ['public', 'customers'] }, federated_sql: { parts: ['PostgreSQL_Demo', 'public', 'customers'] } }
      })
      return fulfillJSON(route, { error: 'resource reference unavailable' }, 404)
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
      const engineID = Number(path.split('/')[5])
      const engine = engines.find(item => item.id === engineID) || ENGINE
      if (requestURL.searchParams.has('locator')) {
        const locator = requestURL.searchParams.get('locator')
        const federation = engine.capabilities?.compute?.query?.federation?.supported === true
        if (!federation && !locator.startsWith(`addp://engine/${engineID}/`)) {
          return fulfillJSON(route, { error_code: 'query_template_resource_invalid' }, 400)
        }
        if (federation) {
          const quote = engine.capabilities.compute.query.identifier_quotes.sql
          const reference = ['PostgreSQL_Demo', 'public', 'customers'].map(part => quote + part + quote).join('.')
          return fulfillJSON(route, { query: `SELECT id, name FROM ${reference} LIMIT 10`, language: 'sql' })
        }
        return fulfillJSON(route, { query: 'SELECT id, name FROM public.customers LIMIT 10', language: 'sql' })
      }
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
      const submitted = request.postDataJSON()
      if (submitted.target_locator && !submitted.target_locator.startsWith(`addp://engine/${submitted.engine_id}/`)) {
        return fulfillJSON(route, { error: '所选资源不是当前引擎的可查询数据项', error_code: 'query_target_invalid' }, 400)
      }
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

for (const locale of ['zh-cn', 'en']) for (const cause of ['roles', 'choice', 'rules']) for (const boundary of (cause === 'rules' ? ['status', 'download'] : ['create', 'status', 'download'])) {
  test(`full export displays precise ${cause} refusal at ${boundary} (${locale})`, async ({ page }, testInfo) => {
    const engine = { ...ENGINE, capabilities: { compute: { query: { ...ENGINE.capabilities.compute.query, read_session: true } } } }
    await installMockBackend(page, { resultKind: 'table', engines: [engine], locale })
    const chinese = locale === 'zh-cn'
    const catalogText = readFileSync(new URL(cause === 'rules' ? `../../../common/middleware/i18n/locales/${locale}.toml` : `../../backend/i18n/locales/${locale}.toml`, import.meta.url), 'utf8')
    const section = cause === 'rules' ? /\[export\.protection_changed\]\s*other = ("[^\n]*")/ : cause === 'roles' ? /\[develop\.query\.export_roles_unavailable\]\s*other = ("[^\n]*")/ : /\[develop\.query\.export_choice_invalid\]\s*other = ("[^\n]*")/
    const message = JSON.parse(catalogText.match(section)[1])
    const payload = { error: message, error_code: cause === 'rules' ? 'export_protection_changed' : cause === 'roles' ? 'query_protection_roles_unavailable' : 'query_protection_choice_invalid' }
    const calls = [], downloads = [], errors = []
    page.on('download', item => downloads.push(item))
    page.on('pageerror', error => errors.push(error.message))
    const refusal = route => route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify(payload) })
    await page.route(`**/api/v1/develop/executions/${EXECUTION_ID}/exports`, route => {
      calls.push('create')
      if (boundary === 'create') return refusal(route)
      return fulfillJSON(route, { id: 91, status: 'pending', requires_current_user: cause !== 'rules' })
    })
    await page.route('**/api/v1/develop/exports/**', route => {
      const file = new URL(route.request().url()).pathname.endsWith('/file')
      calls.push(file ? 'download' : 'status')
      if (boundary === 'status' || file) return refusal(route)
      return fulfillJSON(route, { id: 91, status: 'success', requires_current_user: cause !== 'rules', file_name: 'protected.csv', download_url: '/api/v1/develop/exports/91/file' })
    })
    await page.goto('/sql', { waitUntil: 'networkidle' })
    const catalog = page.locator('.catalog-panel')
    await catalog.getByRole('treeitem', { name: ENGINE.name, exact: true }).click()
    await catalog.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').click()
    await catalog.getByRole('treeitem', { name: 'customers', exact: true }).click()
    await catalog.getByRole('button', { name: chinese ? '生成查询模板' : 'Generate Query Template', exact: true }).click()
    await page.getByRole('button', { name: chinese ? '执行' : 'Execute', exact: true }).click()
    await expect(page.getByText('Ada', { exact: true })).toBeVisible()
    const exportLabel = chinese ? '导出全部结果' : 'Export All Results'
    await page.getByRole('button', { name: exportLabel, exact: true }).click()
    const dialog = page.getByRole('dialog', { name: exportLabel, exact: true })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: chinese ? '开始导出' : 'Start Export', exact: true }).click()
    await expect(page.locator('.el-message--error')).toHaveText(message)
    expect(calls).toEqual(boundary === 'create' ? ['create'] : boundary === 'status' ? ['create', 'status'] : ['create', 'status', 'download'])
    expect(downloads).toEqual([])
    expect(errors).toEqual([])
    if (chinese && boundary === 'download') await page.screenshot({ path: testInfo.outputPath(`export-${cause}-refusal.png`) })
  })
}


for (const locale of ['zh-cn','en']) {
  for (const [operator_id,reason,parameter,field,name] of [
    ...[['envelope','外包矩形','Axis-aligned envelope'],['representative_point','面内点','Interior point'],['centroid','计算质心','Calculate centroid'],['convex_hull','凸包','Convex hull'],['minimum_rotated_rectangle','最小外接矩形','Minimum-area bounding rectangle']].flatMap(([operator_id,zh,en])=>[['operator_input_invalid','input_gdf'],['operator_crs_invalid','']].map(([reason,parameter])=>[operator_id,reason,parameter,'',locale==='zh-cn'?zh:en])),
    ...[['operator_input_invalid','input_gdf'],['operator_crs_invalid',''],['operator_nonnegative_number_required','tolerance'],['operator_boolean_required','preserve_topology']].map(([reason,parameter])=>['simplify',reason,parameter,'',locale==='zh-cn'?'简化几何':'Simplify geometry']),
    ...[['operator_input_invalid','input_gdf'],['operator_crs_invalid',''],['operator_number_invalid','distance'],['operator_positive_integer_required','resolution']].map(([reason,parameter])=>['buffer',reason,parameter,'',locale==='zh-cn'?'缓冲区分析':'Buffer analysis']),
    ['batch_buffer','operator_crs_invalid','','',locale==='zh-cn'?'多距缓冲':'Multi-distance buffer'],
    ...[['operator_input_invalid','input_gdf'],['operator_mask_invalid','mask_gdf'],['operator_crs_invalid','']].map(([reason,parameter])=>['voronoi',reason,parameter,'',locale==='zh-cn'?'泰森多边形':'Voronoi polygons']),
    ['extract_line_nodes','operator_field_conflict','input_gdf','node_id',locale==='zh-cn'?'节点提取':'Extract line nodes'],
    ['extract_line_nodes','operator_endpoint_network_invalid','input_gdf','',locale==='zh-cn'?'节点提取':'Extract line nodes'],
    ['batch_buffer','operator_field_conflict','input_gdf','distance', locale === 'zh-cn' ? '多距缓冲' : 'Multi-distance buffer'],
    ['distance_to','operator_geometry_field_conflict','input_gdf','distance', locale === 'zh-cn' ? '距离计算' : 'Distance calculation'],
    ['check_line_proximity','operator_positive_number_required','tolerance','', locale === 'zh-cn' ? '近接检查' : 'Line proximity check'],
    ...['source_id','part_no','ring_no','ring_id','is_hole'].map(field => ['extract_polygon_rings','operator_field_conflict','input_gdf',field,locale === 'zh-cn' ? '面界提取' : 'Extract polygon rings']),
    ['extract_polygon_rings','operator_geometry_invalid','input_gdf','',locale === 'zh-cn' ? '面界提取' : 'Extract polygon rings'],
    ...[['operator_distance_limit_invalid','max_distance'],['operator_source_topology_invalid','input_gdf'],['operator_trim_conflict','']].map(([reason,parameter])=>['trim_line_endpoints',reason,parameter,'',locale==='zh-cn'?'线端修剪':'Line endpoint trimming'])
  ]) test(`operator input diagnostics show controlled context and copy it (${operator_id}, ${reason}, ${field}, ${locale})`, async ({ page }) => {
    await page.addInitScript(() => { window.copiedDiagnostic=''; Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true}) })
    await installMockBackend(page,{ resultKind:'table',executionType:'workflow',locale,errorDetails:{
      category:'execution_failed',code:'develop.workflow.invalid_params',message:'private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}
    }})
    await page.goto(`/executions/${EXECUTION_ID}`)
    const panel=page.locator('.execution-failure')
    const construction=['envelope','representative_point','centroid','convex_hull','minimum_rotated_rectangle'].includes(operator_id)
    const constructionMarker=reason==='operator_input_invalid'?(locale==='zh-cn'?'几何':'geometry'):(locale==='zh-cn'?'投影坐标系':'projected CRS')
    const simplifyMarker={operator_input_invalid:locale==='zh-cn'?'几何':'geometry',operator_crs_invalid:locale==='zh-cn'?'投影坐标系':'projected CRS',operator_nonnegative_number_required:locale==='zh-cn'?'非负':'non-negative',operator_boolean_required:locale==='zh-cn'?'布尔值':'boolean'}[reason]
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    await expect(panel).toContainText(locale==='zh-cn'?'执行操作':'Execute operation')
    if(field) await expect(panel).toContainText(field)
    else if(reason==='operator_geometry_invalid') {
      await expect(panel).toContainText(locale==='zh-cn'?'未通过有效性检查':'failed the validity check')
      await expect(panel).toContainText(locale==='zh-cn'?'显式修复':'explicitly repair')
    } else if(reason==='operator_endpoint_network_invalid') await expect(panel).toContainText(locale==='zh-cn'?'双方端点':'both endpoints')
    else if(reason==='operator_source_topology_invalid') await expect(panel).toContainText(locale==='zh-cn'?'内部自交':'internal self-intersections')
    else if(reason==='operator_trim_conflict') await expect(panel).toContainText(locale==='zh-cn'?'交错或相遇':'cross or meet')
    else if(construction) await expect(panel).toContainText(constructionMarker)
    else if(operator_id==='simplify') await expect(panel).toContainText(simplifyMarker)
    else if(operator_id==='voronoi') await expect(panel).toContainText(reason==='operator_input_invalid'?(locale==='zh-cn'?'重复 XY':'duplicate XY'):reason==='operator_mask_invalid'?'Polygon':locale==='zh-cn'?'投影坐标系':'projected CRS')
    else if(operator_id==='buffer'||operator_id==='batch_buffer'&&reason==='operator_crs_invalid') await expect(panel).toContainText(reason==='operator_number_invalid'?(locale==='zh-cn'?'正数、零或负数':'positive, zero or negative'):reason==='operator_positive_integer_required'?(locale==='zh-cn'?'大于零的整数':'positive integer'):reason==='operator_crs_invalid'?(locale==='zh-cn'?'投影坐标系':'projected CRS'):locale==='zh-cn'?'几何':'geometry')
    else await expect(panel).toContainText(locale==='zh-cn'?'有限正数':'finite positive')
    await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name)
    expect(copied).toContain(EXECUTION_ID)
    if(field) expect(copied).toContain(field)
    if(reason==='operator_geometry_invalid') expect(copied).toContain(locale==='zh-cn'?'未通过有效性检查':'failed the validity check')
    if(construction) {
      expect(copied).toContain(constructionMarker)
      if(parameter) { await expect(panel).toContainText(parameter); expect(copied).toContain(parameter) }
      if(reason==='operator_input_invalid') {
        await expect(panel).toContainText(locale==='zh-cn'?'唯一活动几何列':'one active geometry column')
        expect(copied).toContain('Z/M')
        if(operator_id==='representative_point') {
          await expect(panel).toContainText('Polygon')
          await expect(panel).toContainText('MultiPolygon')
          expect(copied).toContain('Polygon')
          expect(copied).toContain('MultiPolygon')
        }
      } else {
        await expect(panel).toContainText(locale==='zh-cn'?'线性单位':'horizontal linear units')
        expect(copied).toContain(locale==='zh-cn'?'不会自动投影':'no automatic projection')
      }
      await expect(panel).not.toContainText('/private/input.gpkg')
      expect(copied).not.toContain('/private/input.gpkg')
      expect(copied).not.toMatch(/两个图层|Both layers|标识|identifier|buffer|缓冲|tolerance|容差/i)
    }
    if(operator_id==='simplify') {
      await expect(panel).toContainText(simplifyMarker)
      expect(copied).toContain(simplifyMarker)
      if(parameter) { await expect(panel).toContainText(parameter); expect(copied).toContain(parameter) }
      if(reason==='operator_nonnegative_number_required') {
        await expect(panel).toContainText(locale==='zh-cn'?'零':'zero')
        expect(copied).toContain(locale==='zh-cn'?'零':'zero')
      }
      if(reason==='operator_boolean_required') {
        await expect(panel).toContainText(locale==='zh-cn'?'保留单几何拓扑':'Preserve single-geometry topology')
        expect(copied).toContain('true');expect(copied).toContain('false')
      }
      expect(copied).not.toMatch(/两个图层|Both layers|标识|identifier|buffer|缓冲/i)
      await expect(panel).not.toContainText('/private/input.gpkg')
      expect(copied).not.toContain('/private/input.gpkg')
    }
    if(operator_id==='buffer'||operator_id==='batch_buffer'&&reason==='operator_crs_invalid') {
      if(parameter) { await expect(panel).toContainText(parameter); expect(copied).toContain(parameter) }
      expect(copied).toContain(reason==='operator_number_invalid'?(locale==='zh-cn'?'正数、零或负数':'positive, zero or negative'):reason==='operator_positive_integer_required'?(locale==='zh-cn'?'大于零的整数':'positive integer'):reason==='operator_crs_invalid'?(locale==='zh-cn'?'投影坐标系':'projected CRS'):locale==='zh-cn'?'几何':'geometry')
      if(reason==='operator_crs_invalid') {
        await expect(panel).not.toContainText('两个图层')
        await expect(panel).not.toContainText('Both layers')
        expect(copied).not.toMatch(/两个图层|Both layers/)
      }
      await expect(panel).not.toContainText('/private/input.gpkg')
      expect(copied).not.toContain('/private/input.gpkg')
    }
    expect(copied).not.toContain('private-password')
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
  })
}

test('an unproven operator diagnostic remains generic without leaking input names',async({page})=>{
  await installMockBackend(page,{resultKind:'table',executionType:'workflow',errorDetails:{category:'execution_failed',code:'develop.workflow.execution_failed',
    message:'private-password',diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason:'operator_field_conflict',
      operator:{node_index:1,operator_id:'batch_buffer',parameter:'input_gdf',field:'private-password'}}}})
  await page.goto(`/executions/${EXECUTION_ID}`)
  const panel=page.locator('.execution-failure')
  await expect(panel).toContainText('未记录')
  await expect(panel).not.toContainText('private-password')
  await expect(panel).not.toContainText('多距缓冲')
})


for(const locale of ['zh-cn','en'])for(const [operator_id,reason,parameter,field]of [["get_area", "operator_input_invalid", "input_gdf", ""], ["get_area", "operator_crs_invalid", "", ""], ["get_area", "operator_geometry_field_conflict", "input_gdf", "area"], ["get_length", "operator_input_invalid", "input_gdf", ""], ["get_length", "operator_crs_invalid", "", ""], ["get_length", "operator_geometry_field_conflict", "input_gdf", "length"], ["split_by_area", "operator_input_invalid", "input_gdf", ""], ["split_by_area", "operator_crs_invalid", "", ""], ["split_by_area", "operator_geometry_field_conflict", "input_gdf", "area"], ["split_by_area", "operator_nonnegative_number_required", "threshold", ""]])
 test(`measurement input diagnostics in develop (${operator_id}, ${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id,...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installMockBackend(page,{resultKind:'table',executionType:'workflow',locale,errorDetails:details})
    await page.goto(`/executions/${EXECUTION_ID}`)
    const panel=page.locator('.execution-failure')
    const names=locale==='zh-cn'?{get_area:'计算面积',get_length:'计算长度',split_by_area:'按面积分割'}:{get_area:'Calculate area',get_length:'Calculate length',split_by_area:'Split by area'}
    await expect(panel).toContainText(names[operator_id])
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){await expect(panel).toContainText('EMPTY');await expect(panel).toContainText('NULL');await expect(panel).toContainText('Z/M')}
    if(reason==='operator_crs_invalid')await expect(panel).toContainText(locale==='zh-cn'?'原单位平方':'their square')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Manually rename')}
    if(parameter==='threshold'){await expect(panel).toContainText(locale==='zh-cn'?'非负':'non-negative');await expect(panel).toContainText('large');await expect(panel).toContainText('small')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(names[operator_id]);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })


for(const locale of ['zh-cn','en'])for(const [reason,parameter,field]of [["operator_input_invalid", "input_gdf", ""], ["operator_reference_invalid", "target_geom", ""], ["operator_geometry_field_conflict", "input_gdf", "distance"], ["operator_projected_crs_required", "", ""], ["operator_crs_mismatch", "", ""]])
 test(`distance admission diagnostics in develop (${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'distance_to',...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installMockBackend(page,{resultKind:'table',executionType:'workflow',locale,errorDetails:details})
    await page.goto(`/executions/${EXECUTION_ID}`)
    const panel=page.locator('.execution-failure')
    const name=locale==='zh-cn'?'距离计算':'Distance calculation'
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'||reason==='operator_reference_invalid'){
      await expect(panel).toContainText('NULL');await expect(panel).toContainText('EMPTY');await expect(panel).toContainText('Z/M')
    }
    if(reason==='operator_reference_invalid')await expect(panel).toContainText(locale==='zh-cn'?'空源仍验证目标':'even for an empty source')
    if(reason==='operator_projected_crs_required')await expect(panel).toContainText(locale==='zh-cn'?'原水平单位':'original horizontal CRS units')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })

for(const locale of ['zh-cn','en'])for(const [reason,parameter,field]of [["operator_input_invalid", "input_gdf", ""], ["operator_crs_invalid", "", ""], ["operator_geometry_field_conflict", "input_gdf", "minx"], ["operator_geometry_field_conflict", "input_gdf", "miny"], ["operator_geometry_field_conflict", "input_gdf", "maxx"], ["operator_geometry_field_conflict", "input_gdf", "maxy"]])
 test(`bounds admission diagnostics in develop (${reason}, ${field}, ${locale})`,async({page})=>{
    await page.addInitScript(()=>{window.copiedDiagnostic='';Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}},configurable:true})})
    const details={category:'execution_failed',code:'develop.workflow.invalid_params',message:'PRIVATE_BIZ_FIELD private-password /private/input.gpkg',
      diagnostic:{schema_version:'addp.execution-failure/v3',stage:'execution',reason,operator:{node_index:2,operator_id:'get_bounds',...(parameter?{parameter}:{}),...(field?{field}:{})}}}
    await installMockBackend(page,{resultKind:'table',executionType:'workflow',locale,errorDetails:details})
    await page.goto(`/executions/${EXECUTION_ID}`)
    const panel=page.locator('.execution-failure')
    const name=locale==='zh-cn'?'获取边界框':'Get bounds'
    await expect(panel).toContainText(name)
    await expect(panel).toContainText(locale==='zh-cn'?'定义中的第 2 个节点':'Node 2 in the workflow definition')
    if(parameter)await expect(panel).toContainText(parameter)
    if(reason==='operator_input_invalid'){await expect(panel).toContainText('NULL');await expect(panel).toContainText('EMPTY');await expect(panel).toContainText('Z/M')}
    if(reason==='operator_crs_invalid')await expect(panel).toContainText(locale==='zh-cn'?'地理或投影CRS':'geographic or projected CRS')
    if(field){await expect(panel).toContainText(field);await expect(panel).toContainText(locale==='zh-cn'?'手动重命名':'Rename')}
    await expect(panel).not.toContainText('PRIVATE_BIZ_FIELD');await expect(panel).not.toContainText('private-password')
    await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
    const copied=await page.evaluate(()=>window.copiedDiagnostic)
    expect(copied).toContain(name);if(parameter)expect(copied).toContain(parameter)
    expect(copied).not.toMatch(/PRIVATE_BIZ_FIELD|private-password|\/private\/input.gpkg/)
    await page.setViewportSize({width:390,height:844})
    expect(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy()
 })
