import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, identity, json, login } from './transfer-browser-support.js'

test('栅格 Console 执行状态、持久成果血缘及分析 JSON 一致', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_RASTER_EVIDENCE || '{}')
  const analysis = evidence.result_kind === 'json'
  const required = ['run_id', 'principal_id', 'tenant_id', 'execution_id', 'source_item_id', 'source_locator', 'case_name', 'source_name']
  required.push(...(analysis ? ['expected_result'] : ['target_item_id', 'target_locator', 'target_name', 'source_locators']))
  for (const field of required) {
    expect(evidence[field], `missing raster evidence ${field}`).toBeTruthy()
  }
  const path = `/monitor/executions?execution_id=${encodeURIComponent(evidence.execution_id)}`
  const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, path)
  const api = await request.newContext({
    baseURL: process.env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` }
  })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'browser AuthContext'))
    expect(auth.principalID).toBe(evidence.principal_id)
    expect(auth.principalType).toBe('user')
    expect(auth.tenantID).toBe(evidence.tenant_id)
    expect(auth.contextType).toBe('tenant')
    const execution = await json(await api.get(`/api/v1/monitor/executions/by-execution-id/${evidence.execution_id}`), 'browser Monitor execution')
    expect(execution.status).toBe('success')
    expect(execution.module).toBe('develop')

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const detail = frame.locator('.execution-detail-content')
    await expect(detail).toBeVisible()
    await expect(detail).toContainText(evidence.execution_id)
    await expect(detail.locator('.el-tag').filter({ hasText: /成功|Success/i }).first()).toBeVisible()
    if (analysis) {
      expect(execution.metadata?.lineage_facts).toBeUndefined()
      await expect(detail.locator('.execution-lineage')).toHaveCount(0)
      await detail.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-monitor.png`) })
      const professional = await json(await api.get(`/api/v1/develop/executions/${evidence.execution_id}`), 'browser Develop analysis')
      expect(professional.status).toBe('success')
      expect(professional.outputs || {}).toEqual({})
      expect(professional.metadata.result.final_result).toEqual(evidence.expected_result)
      expect(professional.metadata.result.produced_targets || []).toEqual([])
      expect(professional.metadata.result.meta_scan_runs || []).toEqual([])
      await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${encodeURIComponent(evidence.execution_id)}`)
      const resultPage = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.execution-detail-page')
      await expect(resultPage).toBeVisible()
      await expect(resultPage).toContainText(evidence.execution_id)
      await expect(resultPage.locator('.toolbar .el-tag').filter({ hasText: /成功|Success/i })).toBeVisible()
      const preview = resultPage.locator('.workflow-final-result-json')
      await expect(preview).toBeVisible()
      await expect.poll(async () => JSON.parse(await preview.innerText())).toEqual(evidence.expected_result)
      await expect(resultPage.locator('.workflow-final-result-value')).toHaveCount(0)
      await preview.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-json.png`) })
    } else {
      expect(evidence.source_locators).toContain(evidence.source_locator)
      expect(execution.metadata.lineage_facts.inputs.map(input => input.locator).sort()).toEqual([...evidence.source_locators].sort())
      const targetLocators = evidence.target_locators || [evidence.target_locator]
      expect(targetLocators).toContain(evidence.target_locator)
      expect(execution.metadata.lineage_facts.outputs.map(output => output.locator).sort()).toEqual([...targetLocators].sort())
      const lineage = frame.locator('.execution-lineage')
      await expect(lineage).toBeVisible()
      const groups = lineage.locator('.execution-lineage__group')
      await expect(groups).toHaveCount(2)
      await expect(groups.nth(0).locator('.execution-lineage__card')).toHaveCount(evidence.source_locators.length)
      await expect(groups.nth(1).locator('.execution-lineage__card')).toHaveCount(targetLocators.length)
      await expect(groups.nth(0)).toContainText(evidence.source_name)
      await expect(groups.nth(1)).toContainText(evidence.target_name)
      await expect(groups.nth(0).locator('.execution-lineage__resource-action')).toHaveCount(evidence.source_locators.length)
      await expect(groups.nth(1).locator('.execution-lineage__resource-action')).toHaveCount(0)

      const query = new URLSearchParams({ subject_kind: 'data_item', item_id: String(evidence.target_item_id), direction: 'upstream' })
      const graph = await json(await api.get(`/api/v1/meta/lineage/graph?${query}`), 'browser raster lineage graph')
      expect(graph.truncated).toBe(false)
      expect(graph.edges.some(edge => edge.source.item_id === evidence.source_item_id &&
        edge.target.item_id === evidence.target_item_id && edge.status === 'active' && edge.relation_kind === 'derive' &&
        edge.evidence?.execution_id === evidence.execution_id)).toBe(true)
      await detail.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-monitor.png`) })
    }
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-browser.json`), JSON.stringify({
      ...evidence, schema_version: 'addp.raster-workflow-browser/v1', result: 'passed'
    }, null, 2))
  } finally {
    await api.dispose()
  }
})

test.describe('栅格真实编辑器', () => {
  test.use({ locale: 'zh-CN' })

  test('栅格页面多选、首次保存、重载与真实执行', async ({ page }) => {
    expect(await page.evaluate(() => navigator.language)).toBe('zh-CN')
    const evidence = JSON.parse(process.env.ADDP_ONLINE_RASTER_EVIDENCE || '{}')
    expect(evidence.source_names).toEqual(['mosaic-c.tif', 'mosaic-a.tif', 'mosaic-b.tif'])
    expect(evidence.source_locators).toHaveLength(3)
    expect(new Set(evidence.source_item_ids).size).toBe(3)
    const counts = { create: 0, update: 0, execute: 0 }
    const requestErrors = []
    let pageErrors = 0
    let businessStarted = false
    page.on('pageerror', () => { pageErrors += 1 })
    page.on('request', event => {
      const path = new URL(event.url()).pathname
      if (event.method() === 'POST' && path === '/api/v1/develop/task-definitions') counts.create += 1
      if (event.method() === 'PUT' && /^\/api\/v1\/develop\/task-definitions\/\d+$/.test(path)) counts.update += 1
      if (event.method() === 'POST' && /^\/api\/v1\/develop\/task-definitions\/\d+\/execute$/.test(path)) counts.execute += 1
    })
    page.on('response', response => {
      const path = new URL(response.url()).pathname
      if (businessStarted && path.startsWith('/api/v1/') && response.status() >= 400) {
        requestErrors.push({ path, status: response.status() })
      }
    })
    const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME,
      process.env.ADDP_ONLINE_TEST_USER_PASSWORD, '/develop/workflow?action=create')
    businessStarted = true
    let api = await request.newContext({ baseURL: process.env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    let taskId
    let report
    try {
      const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'editor AuthContext'))
      expect(auth.principalType).toBe('user')
      expect(auth.contextType).toBe('tenant')
      expect(auth.principalID).toBe(evidence.principal_id)
      expect(auth.tenantID).toBe(evidence.tenant_id)
      expect([...auth.permissions].sort()).toEqual(evidence.permissions_verified)
      expect(auth.permissions.has('develop.task.update')).toBe(false)
      const engines = await json(await api.get('/api/v1/meta/engines'), 'editor engines')
      const targetEngine = engines.find(engine => engine.id === evidence.target_engine_id)
      expect(targetEngine).toBeTruthy()
      const specs = await json(await api.get(`/api/v1/develop/workflow-engines/${evidence.workflow_engine_id}/operators`), 'editor operators')
      const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
      await expect(frame.locator('.engine-select')).toBeVisible()
      const palette = frame.getByPlaceholder('搜索算子...')
      const addOperator = async name => {
        const spec = specs.operators.find(operator => operator.name === name)
        expect(spec).toBeTruthy()
        await palette.fill(name)
        await frame.getByRole('button', { name: `添加“${spec.display_name}”算子`, exact: true }).click()
        await palette.clear()
      }
      const selectNode = async name => {
        const position = await frame.locator('#workflow-dag-container').evaluate((element, operator) => {
          const graph = element.__vueParentComponent.setupState.graph
          const nodes = graph.getNodes().filter(node => node.getModel().operator === operator)
          if (nodes.length !== 1) throw new Error('expected one visible operator node')
          const model = nodes[0].getModel()
          return graph.getClientByPoint(model.x, model.y)
        }, name)
        const iframe = await page.locator('iframe[data-testid="module-iframe"]').boundingBox()
        expect(iframe).toBeTruthy()
        await page.mouse.click(iframe.x + position.x, iframe.y + position.y)
        await expect(frame.locator('.node-summary')).toContainText(name)
      }
      const setText = async (name, value) => {
        const input = frame.locator(`[data-param-name="${name}"]`).getByRole('textbox')
        await input.fill(value)
        await input.press('Tab')
      }
      const selectValue = async (name, value) => {
        await frame.locator(`[data-param-name="${name}"] .el-select__wrapper`).click()
        await frame.getByRole('option', { name: value, exact: true }).click()
      }
      const expectSourceOrder = async () => {
        const labels = frame.locator('.ordered-input-label')
        await expect(labels).toHaveCount(3)
        for (const [index, name] of evidence.source_names.entries()) {
          await expect(labels.nth(index)).toContainText(name)
        }
      }
      await addOperator('raster_mosaic')
      await selectNode('raster_mosaic')
      await frame.getByRole('button', { name: '选择影像', exact: true }).click()
      const sources = frame.getByRole('dialog', { name: '选择影像', exact: true })
      for (const name of evidence.source_names) {
        await sources.locator('.picker-search input').fill(name)
        const result = sources.locator('.search-result').filter({ hasText: name })
        await expect(result).toHaveCount(1)
        await result.click()
        await expect(result).toHaveAttribute('aria-pressed', 'true')
      }
      await expect(sources.locator('.selected-resources li')).toHaveCount(3)
      await sources.getByRole('button', { name: '确定', exact: true }).click()
      await expectSourceOrder()
      await setText('target_crs', 'EPSG:3857')
      await setText('resolution', '[1,1]')
      await selectValue('overlap', 'first')
      await addOperator('raster_save')
      await frame.getByRole('button', { name: '自动布局', exact: true }).click()
      await frame.getByRole('button', { name: '适应窗口', exact: true }).click()
      await selectNode('raster_save')
      await frame.locator('.input-connection-field .el-select__wrapper').click()
      const mosaicName = specs.operators.find(operator => operator.name === 'raster_mosaic').display_name
      await frame.getByRole('option').filter({ hasText: mosaicName }).click()
      await frame.locator('[data-param-name="target_resource"] .resource-selection-card').getByRole('button').click()
      const target = frame.locator('.workflow-resource-picker-dialog:visible')
      await target.locator('.el-select__wrapper').first().click()
      await frame.getByRole('option').filter({ hasText: targetEngine.name }).click()
      await target.getByText('raster-target', { exact: true }).click()
      await target.getByRole('button', { name: '确定', exact: true }).click()
      await setText('target_name', evidence.target_name)
      await selectValue('profile', 'cog')
      const blocksize = frame.locator('[data-param-name="blocksize"]').getByRole('spinbutton')
      await blocksize.fill('128')
      await blocksize.press('Tab')
      const createdResponse = page.waitForResponse(response => response.request().method() === 'POST' &&
        new URL(response.url()).pathname === '/api/v1/develop/task-definitions')
      await frame.locator('.primary-actions').getByRole('button', { name: '保存', exact: true }).click()
      const saveDialog = frame.getByRole('dialog', { name: '保存工作流', exact: true })
      await saveDialog.getByPlaceholder('请输入工作流名称').fill(evidence.task_name)
      await saveDialog.getByRole('button', { name: '保存', exact: true }).click()
      const created = await json(await createdResponse, 'editor save')
      if (Number.isSafeInteger(created.id) && created.id > 0) taskId = created.id
      expect(taskId).toBeGreaterThan(0)
      expect(created.name).toBe(evidence.task_name)
      expect(created.execution_config.engine_id).toBe(evidence.workflow_engine_id)
      await expect.poll(() => new URL(page.url()).searchParams.get('id')).toBe(String(taskId))
      const reloadedTask = page.waitForResponse(response => response.ok() && response.request().method() === 'GET' &&
        new URL(response.url()).pathname === `/api/v1/develop/task-definitions/${taskId}`)
      await page.reload()
      await expect(frame.locator('.primary-actions').getByRole('button', { name: '执行', exact: true })).toBeEnabled()
      const reloadedResponse = await reloadedTask
      const saved = await json(reloadedResponse, 'reloaded editor task')
      const reloadedAPI = await browserAPIFromResponse(reloadedResponse, process.env.GATEWAY_URL, auth)
      await api.dispose()
      api = reloadedAPI
      expect(saved.name).toBe(evidence.task_name)
      expect(saved.content.workflow_definition).toEqual(created.content.workflow_definition)
      expect(saved.execution_config.engine_id).toBe(evidence.workflow_engine_id)
      await frame.getByRole('button', { name: '适应窗口', exact: true }).click()
      await selectNode('raster_mosaic')
      await expectSourceOrder()
      const saveNode = saved.content.workflow_definition.tasks.find(task => task.operator === 'raster_save')
      expect(saveNode).toBeTruthy()
      await frame.locator('.workflow-editor-page').screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-editor.png`) })
      await frame.locator('.primary-actions').getByRole('button', { name: '执行', exact: true }).click()
      const executeDialog = frame.getByRole('dialog', { name: '执行工作流', exact: true })
      const executedResponse = page.waitForResponse(response => response.request().method() === 'POST' &&
        new URL(response.url()).pathname === `/api/v1/develop/task-definitions/${taskId}/execute`)
      await executeDialog.getByRole('button', { name: '执行', exact: true }).click()
      const receipt = await json(await executedResponse, 'editor execute')
      expect(receipt.execution_id).toMatch(/^[0-9a-f-]{36}$/)
      let execution
      await expect.poll(async () => {
        execution = await json(await api.get(`/api/v1/develop/executions/${receipt.execution_id}`), 'editor execution')
        if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error('editor raster execution failed')
        return execution.status
      }, { timeout: 90_000 }).toBe('success')
      expect(execution.source_task_id).toBe(String(taskId))
      expect(execution.module).toBe('develop')
      expect(execution.metadata.lineage_facts.inputs.map(input => input.locator).sort()).toEqual([...evidence.source_locators].sort())
      const monitorRead = page.waitForResponse(response => response.ok() && response.request().method() === 'GET' &&
        new URL(response.url()).pathname === `/api/v1/monitor/executions/by-execution-id/${receipt.execution_id}`)
      await page.goto(`${process.env.CONSOLE_URL}/monitor/executions?execution_id=${encodeURIComponent(receipt.execution_id)}`)
      const detail = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.execution-detail-content')
      await expect(detail).toBeVisible()
      await expect(detail).toContainText(receipt.execution_id)
      await expect(detail.locator('.el-tag').filter({ hasText: /成功|Success/i }).first()).toBeVisible()
      const groups = detail.locator('.execution-lineage__group')
      await expect(groups).toHaveCount(2)
      await expect(groups.nth(0).locator('.execution-lineage__card')).toHaveCount(3)
      await expect(groups.nth(1).locator('.execution-lineage__card')).toHaveCount(1)
      for (const name of evidence.source_names) await expect(groups.nth(0)).toContainText(name)
      await expect(groups.nth(1)).toContainText(evidence.target_name)
      await detail.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-editor-monitor.png`) })
      const monitorAPI = await browserAPIFromResponse(await monitorRead, process.env.GATEWAY_URL, auth)
      await api.dispose()
      api = monitorAPI
      expect(counts).toEqual({ create: 1, update: 0, execute: 1 })
      expect(pageErrors).toBe(0)
      expect(requestErrors).toEqual([])
      report = { ...evidence, schema_version: 'addp.raster-workflow-editor/v1', result: 'passed',
        task_id: taskId, execution_id: receipt.execution_id, source_task_id: execution.source_task_id,
        workflow_definition: saved.content.workflow_definition, save_node_id: saveNode.id,
        reloaded: true, deleted: false, page_requests: counts }
    } finally {
      try {
        if (taskId) {
          await json(await api.delete(`/api/v1/develop/task-definitions/${taskId}`), 'editor task cleanup')
          expect((await api.get(`/api/v1/develop/task-definitions/${taskId}`)).status()).toBe(404)
          if (report) report.deleted = true
        }
      } finally {
        await api.dispose()
      }
    }
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-editor.json`), JSON.stringify(report, null, 2))
  })
})
