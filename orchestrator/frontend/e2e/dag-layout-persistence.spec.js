import { expect, test } from '@playwright/test'

const ORCHESTRATION_ID = 'dag-layout-e2e'
const NODE_ID = 'fixture-step'
const INTERACTION_ORCHESTRATION_ID = 'dag-interactions-e2e'
const SOURCE_NODE_ID = 'source-step'
const TARGET_NODE_ID = 'target-step'
const TASK_LIBRARY_ORCHESTRATION_ID = 'task-library-e2e'
const EXECUTION_ID = 31

test.describe('responsive orchestration dialogs', () => {
  test.use({ viewport: { width: 620, height: 560 }, colorScheme: 'dark' })

  test('keeps editor dialogs visible and visually stable in a narrow window', async ({ page }) => {
    await installMockBackend(page, createLayoutFixture())
    await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
    await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

    await page.getByRole('button', { name: '保存', exact: true }).click()
    const saveDialog = page.getByRole('dialog', { name: '保存编排信息', exact: true })
    const saveSurface = visibleDialogSurface(page)
    await expectDialogWithinViewport(page, saveSurface)
    await expect(saveSurface.locator('.el-dialog__body')).toHaveCSS('overflow', 'auto')
    await expect(saveSurface).toHaveScreenshot('orchestration-save-narrow.png', { animations: 'disabled' })
    await saveDialog.getByRole('button', { name: '取消', exact: true }).click()

    await page.getByRole('button', { name: '调度', exact: true }).click()
    const scheduleDialog = page.getByRole('dialog', { name: '设置定时调度', exact: true })
    const scheduleSurface = visibleDialogSurface(page)
    await expectDialogWithinViewport(page, scheduleSurface)
    await expect(scheduleSurface.locator('.el-dialog__body')).toHaveCSS('overflow', 'auto')
    await expect(scheduleSurface).toHaveScreenshot('orchestration-schedule-narrow.png', { animations: 'disabled' })

    await scheduleDialog.getByRole('button', { name: '自定义时间', exact: true }).click()
    const dialogSurfaces = page.locator('.el-dialog.addp-dialog:visible')
    await expect(dialogSurfaces).toHaveCount(2)
    const customScheduleSurface = dialogSurfaces.last()
    await expectDialogWithinViewport(page, customScheduleSurface)
    await expect(customScheduleSurface.locator('.el-dialog__body')).toHaveCSS('overflow', 'auto')
    await expect(customScheduleSurface).toHaveScreenshot('orchestration-custom-schedule-narrow.png', { animations: 'disabled' })
    await customScheduleSurface.getByRole('button', { name: '取消', exact: true }).click()
    await expect(dialogSurfaces).toHaveCount(1)
    await scheduleDialog.getByRole('button', { name: '取消', exact: true }).click()

    await page.getByRole('button', { name: '查看 JSON', exact: true }).click()
    const jsonDialog = page.getByRole('dialog', { name: '编排 JSON 配置', exact: true })
    const jsonSurface = visibleDialogSurface(page)
    await expectDialogWithinViewport(page, jsonSurface)
    await expect(jsonSurface.locator('.json-content')).toHaveCSS('overflow', 'auto')
    await expect(jsonSurface).toHaveScreenshot('orchestration-json-narrow.png', { animations: 'disabled' })
    await jsonDialog.getByRole('button', { name: '关闭', exact: true }).click()

    await page.getByRole('button', { name: '清空', exact: true }).click()
    const clearDialog = page.getByRole('dialog', { name: '清空', exact: true })
    const clearSurface = page.locator('.el-message-box.addp-message-box:visible')
    await expectDialogWithinViewport(page, clearSurface)
    await expect(clearDialog.getByRole('button', { name: '取消', exact: true })).toBeVisible()
    await expect(clearDialog.getByRole('button', { name: '清空', exact: true })).toHaveClass(/el-button--danger/)
    await expect(clearSurface).toHaveScreenshot('orchestration-clear-confirm-narrow.png', { animations: 'disabled' })
  })

  test('keeps execution details readable in a narrow window', async ({ page }) => {
    const execution = createExecutionFixture()
    await installMockBackend(page, createLayoutFixture(), { executions: [execution] })
    await page.goto(`/orchestrations/${ORCHESTRATION_ID}/executions`)
    await expect(page.getByRole('heading', { name: '执行记录', exact: true })).toBeVisible()

    await page.getByRole('button', { name: '详情', exact: true }).click()
    const detailDialog = page.getByRole('dialog', { name: '执行详情', exact: true })
    const detailSurface = visibleDialogSurface(page)
    await expect(detailDialog.getByText(execution.execution_id, { exact: true })).toBeVisible()
    await expectDialogWithinViewport(page, detailSurface)
    await expect(detailSurface.locator('.el-dialog__body')).toHaveCSS('overflow', 'auto')
    await expect(detailSurface).toHaveScreenshot('orchestration-execution-detail-narrow.png', { animations: 'disabled' })
  })
})

test('confirms orchestration execution and locks duplicate submissions', async ({ page }) => {
  const orchestration = createLayoutFixture()
  const backend = await installMockBackend(page, orchestration, { deferExecute: true })
  await page.goto('/orchestrations')
  await expect(page.getByRole('heading', { name: '任务编排', exact: true })).toBeVisible()

  const executeButton = page.locator('.orchestration-list').getByRole('button', { name: '执行', exact: true })
  await executeButton.click()
  let confirmDialog = page.getByRole('dialog', { name: '确认执行', exact: true })
  await expect(confirmDialog).toContainText(`确定要执行编排“${orchestration.name}”吗？`)
  await confirmDialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(confirmDialog).not.toBeVisible()
  expect(backend.getExecuteRequestCount()).toBe(0)

  await executeButton.click()
  confirmDialog = page.getByRole('dialog', { name: '确认执行', exact: true })
  await confirmDialog.getByRole('button', { name: '执行', exact: true }).click()
  await expect.poll(() => backend.getExecuteRequestCount()).toBe(1)
  await expect(executeButton).toBeDisabled()

  backend.releaseExecute()
  await expect(page.locator('.el-message').filter({ hasText: '编排已触发' })).toBeVisible()
  await expect(executeButton).toBeEnabled()
})

test('persists node position and viewport across reload', async ({ page }) => {
  const orchestration = createLayoutFixture()
  const backend = await installMockBackend(page, orchestration)
  const zoomWarnings = []
  page.on('console', message => {
    if (message.type() === 'warning' && message.text().includes('zoom failed')) {
      zoomWarnings.push(message.text())
    }
  })

  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  await expect(canvas).toHaveCount(1)
  const initialBox = await requiredBoundingBox(canvas)

  await drag(page, {
    x: initialBox.x + orchestration.editor_layout.nodes[NODE_ID].x,
    y: initialBox.y + orchestration.editor_layout.nodes[NODE_ID].y
  }, { x: 170, y: 100 })

  const zoomIn = page.getByRole('button', { name: '放大' })
  await zoomIn.click()
  await zoomIn.click()

  await drag(page, {
    x: initialBox.x + initialBox.width - 80,
    y: initialBox.y + initialBox.height - 80
  }, { x: -70, y: -45 })

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '保存编排信息' })).toBeVisible()
  await page.getByRole('button', { name: '确认保存' }).click()
  await expect(page.getByRole('heading', { name: '任务编排' })).toBeVisible()

  const persistedPayload = backend.getPersistedPayload()
  expect(persistedPayload).not.toBeNull()
  expect(persistedPayload.editor_layout.viewport.zoom).toBeCloseTo(1.2, 8)
  expect(persistedPayload.editor_layout.nodes[NODE_ID].x).not.toBe(220)
  expect(persistedPayload.editor_layout.nodes[NODE_ID].y).not.toBe(160)
  expect(Math.abs(persistedPayload.editor_layout.viewport.translate_x)).toBeGreaterThan(0)
  expect(Math.abs(persistedPayload.editor_layout.viewport.translate_y)).toBeGreaterThan(0)

  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()
  await expect(canvas).toHaveCount(1)

  const restoredBox = await requiredBoundingBox(canvas)
  const restoredLayout = persistedPayload.editor_layout
  const restoredPosition = restoredLayout.nodes[NODE_ID]
  await page.mouse.click(
    restoredBox.x + restoredPosition.x * restoredLayout.viewport.zoom + restoredLayout.viewport.translate_x,
    restoredBox.y + restoredPosition.y * restoredLayout.viewport.zoom + restoredLayout.viewport.translate_y
  )
  await expect(page.getByRole('button', { name: '复制节点', exact: true })).toBeEnabled()

  await page.getByRole('button', { name: '适应窗口', exact: true }).click()
  await expect(page.getByRole('button', { name: '放大', exact: true })).toBeDisabled()
  expect(zoomWarnings).toEqual([])
})

test('keeps orchestration dialog and canvas focus predictable', async ({ page }) => {
  const orchestration = createLayoutFixture()
  await installMockBackend(page, orchestration)
  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const saveTrigger = page.getByRole('button', { name: '保存', exact: true })
  await saveTrigger.click()
  const saveDialog = page.getByRole('dialog', { name: '保存编排信息', exact: true })
  await expect(saveDialog.getByPlaceholder('请输入编排名称', { exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(saveDialog).not.toBeVisible()
  await expect(saveTrigger).toBeFocused()

  const scheduleTrigger = page.getByRole('button', { name: '调度', exact: true })
  await scheduleTrigger.click()
  const scheduleDialog = page.getByRole('dialog', { name: '设置定时调度', exact: true })
  await expect(scheduleDialog.getByRole('switch')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(scheduleDialog).not.toBeVisible()
  await expect(scheduleTrigger).toBeFocused()

  const jsonTrigger = page.getByRole('button', { name: '查看 JSON', exact: true })
  await jsonTrigger.click()
  const jsonDialog = page.getByRole('dialog', { name: '编排 JSON 配置', exact: true })
  await expect(jsonDialog.getByRole('button', { name: '关闭', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(jsonDialog).not.toBeVisible()
  await expect(jsonTrigger).toBeFocused()

  const canvasRegion = page.getByRole('region', { name: '编排 DAG 画布', exact: true })
  const canvas = canvasRegion.locator('canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  await page.mouse.click(
    canvasBox.x + orchestration.editor_layout.nodes[NODE_ID].x,
    canvasBox.y + orchestration.editor_layout.nodes[NODE_ID].y
  )
  await expect(canvasRegion).toBeFocused()
  await expect(page.getByRole('button', { name: '删除选中项', exact: true })).toBeEnabled()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: '删除选中项', exact: true })).toBeDisabled()

  await expect(canvasRegion).toHaveAttribute('aria-keyshortcuts', 'ArrowLeft ArrowRight ArrowUp ArrowDown Enter Delete Escape')
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('status', { name: '编排画布选择状态' })).toHaveText('已选择节点“Fixture task”')
  await expect(page.getByRole('button', { name: '删除选中项', exact: true })).toBeEnabled()
  await expect(page.locator('.el-drawer')).not.toBeVisible()
  await page.keyboard.press('Enter')
  await expect(page.locator('.el-drawer')).toBeVisible()
})

test('announces orchestration save progress', async ({ page }) => {
  const orchestration = createLayoutFixture()
  const backend = await installMockBackend(page, orchestration, { deferPersist: true })
  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  const saveDialog = page.getByRole('dialog', { name: '保存编排信息', exact: true })
  await saveDialog.getByRole('button', { name: '确认保存', exact: true }).click()

  await expect.poll(() => backend.getPersistedPayload()).not.toBeNull()
  await expect(page.locator('.orchestration-form')).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('status', { name: '编排状态' })).toHaveText('正在保存编排')

  backend.releasePersist()
  await expect(page).toHaveURL(/\/orchestrations$/)
})

test('resizes the task library with the keyboard', async ({ page }) => {
  await installMockBackend(page, createLayoutFixture())
  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const splitter = page.getByRole('separator', { name: '调整任务库宽度', exact: true })
  const taskPanel = page.locator('#task-library-panel')
  await expect(splitter).toHaveAttribute('aria-valuenow', '320')
  await splitter.focus()

  await page.keyboard.press('ArrowRight')
  await expect(splitter).toHaveAttribute('aria-valuenow', '336')
  await expect(taskPanel).toHaveCSS('width', '336px')

  await page.keyboard.press('Home')
  await expect(splitter).toHaveAttribute('aria-valuenow', '240')
  await expect(taskPanel).toHaveCSS('width', '240px')

  await page.keyboard.press('End')
  await expect(splitter).toHaveAttribute('aria-valuenow', '560')
  await expect(taskPanel).toHaveCSS('width', '560px')
})

test('connects ports and preserves the redone edge without copying it', async ({ page }) => {
  const orchestration = createInteractionFixture()
  const backend = await installMockBackend(page, orchestration)

  await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  await expect(canvas).toHaveCount(1)
  const canvasBox = await requiredBoundingBox(canvas)
  const source = orchestration.editor_layout.nodes[SOURCE_NODE_ID]
  const target = orchestration.editor_layout.nodes[TARGET_NODE_ID]
  const copy = page.getByRole('button', { name: '复制节点', exact: true })
  const sourceFillBeforeSelection = await canvasPixel(canvas, source.x, source.y + 15)

  await page.mouse.click(canvasBox.x + source.x, canvasBox.y + source.y)
  await expect(copy).toBeEnabled()
  expect(colorDistance(
    await canvasPixel(canvas, source.x, source.y + 15),
    sourceFillBeforeSelection
  )).toBeLessThanOrEqual(3)

  await drag(page, {
    x: canvasBox.x + source.x + 60,
    y: canvasBox.y + source.y
  }, {
    x: target.x - source.x - 120,
    y: target.y - source.y
  })

  const undo = page.getByRole('button', { name: '撤销', exact: true })
  const redo = page.getByRole('button', { name: '重做', exact: true })
  await expect(undo).toBeEnabled()
  await undo.click()
  await expect(redo).toBeEnabled()
  await redo.click()
  await expect(redo).toBeDisabled()

  await page.mouse.click(canvasBox.x + source.x, canvasBox.y + source.y)
  const paste = page.getByRole('button', { name: '粘贴节点', exact: true })
  await expect(copy).toBeEnabled()
  await copy.click()
  await expect(paste).toBeEnabled()
  await paste.click()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByRole('button', { name: '确认保存' }).click()
  await expect(page.getByRole('heading', { name: '任务编排' })).toBeVisible()

  const persistedPayload = backend.getPersistedPayload()
  expect(persistedPayload).not.toBeNull()
  expect(persistedPayload.steps).toHaveLength(3)

  const targetStep = persistedPayload.steps.find(step => step.id === TARGET_NODE_ID)
  expect(targetStep.depends_on).toEqual([SOURCE_NODE_ID])

  const pastedStep = persistedPayload.steps.find(step => ![SOURCE_NODE_ID, TARGET_NODE_ID].includes(step.id))
  expect(pastedStep).toBeDefined()
  expect(pastedStep.depends_on).toEqual([])
})

test('edits predecessor steps in the drawer and disables circular candidates', async ({ page }) => {
  const orchestration = createInteractionFixture()
  const backend = await installMockBackend(page, orchestration)
  await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  const source = orchestration.editor_layout.nodes[SOURCE_NODE_ID]
  const target = orchestration.editor_layout.nodes[TARGET_NODE_ID]

  await page.mouse.dblclick(canvasBox.x + target.x, canvasBox.y + target.y)
  let drawer = page.getByRole('dialog', { name: '配置步骤', exact: true })
  await expect(drawer).toBeVisible()
  const predecessorField = drawer.locator('.el-form-item').filter({ hasText: '前置步骤' })
  await predecessorField.locator('.el-select__wrapper').click()
  const sourceOption = page.locator('.el-select-dropdown__item:visible').filter({ hasText: 'Source task' })
  await expect(sourceOption).toHaveCount(1)
  await sourceOption.click({ force: true })
  await expect(page.locator('.el-message').filter({ hasText: '依赖关系已更新' })).toBeVisible()
  await drawer.getByRole('button', { name: 'Close this dialog', exact: true }).click()
  await expect(drawer).not.toBeVisible()

  await page.mouse.dblclick(canvasBox.x + source.x, canvasBox.y + source.y)
  drawer = page.getByRole('dialog', { name: '配置步骤', exact: true })
  await expect(drawer).toBeVisible()
  await drawer.locator('.el-form-item').filter({ hasText: '前置步骤' }).locator('.el-select__wrapper').click()
  const circularOption = page.locator('.el-select-dropdown__item:visible').filter({ hasText: 'Target task' })
  await expect(circularOption).toHaveCount(1)
  await expect(circularOption).toHaveClass(/is-disabled/)
  await page.keyboard.press('Escape')
  await drawer.getByRole('button', { name: 'Close this dialog', exact: true }).click()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByRole('button', { name: '确认保存', exact: true }).click()
  await expect(page.getByRole('heading', { name: '任务编排' })).toBeVisible()
  const persistedTarget = backend.getPersistedPayload().steps.find(step => step.id === TARGET_NODE_ID)
  expect(persistedTarget.depends_on).toEqual([SOURCE_NODE_ID])
})

test('persists the last valid node draft without a separate config save', async ({ page }) => {
  const orchestration = createLayoutFixture()
  const backend = await installMockBackend(page, orchestration)

  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  const node = orchestration.editor_layout.nodes[NODE_ID]
  await page.mouse.dblclick(canvasBox.x + node.x, canvasBox.y + node.y)

  const drawer = page.getByRole('dialog', { name: '配置步骤', exact: true })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  await drawer.getByPlaceholder('例如: 数据传输', { exact: true }).fill('Updated fixture task')

  const parameters = drawer.locator('textarea')
  await expect(parameters).toHaveCount(1)
  await parameters.fill('{')
  await expect(drawer.getByText('参数 JSON 格式错误', { exact: true })).toBeVisible()
  await parameters.fill('{"limit": 25}')
  await expect(drawer.getByText('参数 JSON 格式错误', { exact: true })).not.toBeVisible()
  await expect(page.getByRole('button', { name: '撤销', exact: true })).toBeEnabled()
  await drawer.getByRole('button', { name: 'Close this dialog', exact: true }).click()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByRole('button', { name: '确认保存', exact: true }).click()
  await expect(page.getByRole('heading', { name: '任务编排' })).toBeVisible()

  const persistedPayload = backend.getPersistedPayload()
  expect(persistedPayload).not.toBeNull()
  expect(persistedPayload.steps[0].name).toBe('Updated fixture task')
  expect(persistedPayload.steps[0].parameters).toEqual({ limit: 25 })
})

test('keeps task types collapsed and expands only matching search paths', async ({ page }) => {
  const orchestration = {
    ...createLayoutFixture(),
    id: TASK_LIBRARY_ORCHESTRATION_ID,
    steps: [],
    editor_layout: {
      nodes: {},
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    }
  }
  const backend = await installMockBackend(page, orchestration, createTaskLibraryFixture())

  await page.goto(`/orchestrations/${TASK_LIBRARY_ORCHESTRATION_ID}/edit`)
  await expect(page.getByRole('heading', { name: '编辑编排' })).toBeVisible()

  const taskPanel = page.locator('.task-panel')
  const search = taskPanel.getByPlaceholder('搜索任务')
  const refreshTaskLibrary = taskPanel.getByRole('button', { name: '刷新任务库', exact: true })
  await expect(search).toBeVisible()
  try {
    await expect(taskPanel.getByText('开发中心', { exact: true })).toBeVisible()
    await expect(taskPanel.getByText('SQL 查询', { exact: true })).toBeVisible()
    await expect(taskPanel.getByText('Notebook', { exact: true })).toBeVisible()
    await expect(taskPanel.locator('.task-type-loading')).toHaveCount(2)
    await expect(taskPanel.locator('.el-loading-mask')).not.toBeVisible()
    await expect(refreshTaskLibrary).toBeDisabled()
  } finally {
    backend.releaseTaskRequests()
  }

  await expect(taskPanel.locator('.task-type-loading')).toHaveCount(0)
  await expect(refreshTaskLibrary).toBeEnabled()
  await expect(taskPanel.getByText('客户日报', { exact: true })).not.toBeVisible()
  await expect(taskPanel.getByText('月度预测', { exact: true })).not.toBeVisible()

  await search.fill('客户')
  await expect(taskPanel.getByText('客户日报', { exact: true })).toBeVisible()
  await expect(taskPanel.getByText('库存清单', { exact: true })).not.toBeVisible()
  await expect(taskPanel.getByText('月度预测', { exact: true })).not.toBeVisible()
  const addTaskToCanvas = taskPanel.getByRole('button', { name: '添加到画布', exact: true })
  await expect(addTaskToCanvas).toHaveCount(1)
  await addTaskToCanvas.click()

  await search.fill('不存在')
  await expect(taskPanel.getByText('未找到匹配任务', { exact: true })).toBeVisible()

  await search.fill('')
  await expect(taskPanel.getByText('SQL 查询', { exact: true })).toBeVisible()
  await expect(taskPanel.getByText('Notebook', { exact: true })).toBeVisible()
  await expect(taskPanel.getByText('客户日报', { exact: true })).not.toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  await page.mouse.click(canvasBox.x + canvasBox.width / 2, canvasBox.y + canvasBox.height / 2)
  await expect(page.getByRole('button', { name: '复制节点', exact: true })).toBeEnabled()
})

function createLayoutFixture() {
  return {
    id: ORCHESTRATION_ID,
    name: 'DAG layout persistence fixture',
    description: '',
    enabled: false,
    schedule: '',
    steps: [{
      id: NODE_ID,
      name: 'Fixture task',
      provider: 'develop',
      task_type: 'query',
      task_id: 7,
      parameters: {},
      depends_on: [],
      timeout: 300
    }],
    editor_layout: {
      nodes: { [NODE_ID]: { x: 220, y: 160 } },
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    }
  }
}

function createInteractionFixture() {
  return {
    id: INTERACTION_ORCHESTRATION_ID,
    name: 'DAG interaction fixture',
    description: '',
    enabled: false,
    schedule: '',
    steps: [
      createFixtureStep(SOURCE_NODE_ID, 'Source task', 7),
      createFixtureStep(TARGET_NODE_ID, 'Target task', 8)
    ],
    editor_layout: {
      nodes: {
        [SOURCE_NODE_ID]: { x: 180, y: 180 },
        [TARGET_NODE_ID]: { x: 480, y: 180 }
      },
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    }
  }
}

function createFixtureStep(id, name, taskId) {
  return {
    id,
    name,
    provider: 'develop',
    task_type: 'query',
    task_id: taskId,
    parameters: {},
    depends_on: [],
    timeout: 300
  }
}

function createTaskLibraryFixture() {
  return {
    deferTaskRequests: true,
    taskProviders: [{
      id: 1,
      module_name: 'develop',
      display_name: '开发中心',
      capabilities: {
        schema_version: 'task.capabilities/v1',
        task_capabilities: [
          { type: 'query', display_name: 'SQL 查询' },
          { type: 'notebook', display_name: 'Notebook' }
        ]
      }
    }],
    tasksByType: {
      query: [
        { id: 11, task_type: 'query', display_name: '客户日报' },
        { id: 12, task_type: 'query', display_name: '库存清单' }
      ],
      notebook: [
        { id: 21, task_type: 'notebook', display_name: '月度预测' }
      ]
    }
  }
}

function createExecutionFixture() {
  return {
    id: EXECUTION_ID,
    execution_id: 'orchestration-execution-31',
    status: 'failed',
    current_step: 'transform-step',
    started_at: null,
    completed_at: null,
    error_details: { message: 'Fixture execution failed during transform' },
    metadata: {
      step_results: {
        'transform-step': {
          status: 'failed',
          error: 'Fixture transform error'
        }
      }
    }
  }
}

async function installMockBackend(page, initialOrchestration, taskLibrary = {}) {
  let persistedPayload = null
  let executeRequestCount = 0
  let orchestration = initialOrchestration
  const taskProviders = taskLibrary.taskProviders || []
  const tasksByType = taskLibrary.tasksByType || {}
  const executions = taskLibrary.executions || []
  const pendingTaskRequests = []
  let releasePersist = () => {}
  let releaseExecute = () => {}
  const persistGate = taskLibrary.deferPersist
    ? new Promise(resolve => { releasePersist = resolve })
    : null
  const executeGate = taskLibrary.deferExecute
    ? new Promise(resolve => { releaseExecute = resolve })
    : null

  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const detailPath = `/api/v1/orchestrator/orchestrations/${orchestration.id}`

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'dag-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'dag-e2e' })
    }
    if (path === '/api/v1/orchestrator/task-providers') {
      return fulfillJSON(route, taskProviders)
    }
    if (path === '/api/v1/orchestrator/tasks') {
      if (taskLibrary.deferTaskRequests) {
        await new Promise(resolve => pendingTaskRequests.push(resolve))
      }
      const taskType = new URL(request.url()).searchParams.get('task_type')
      const items = tasksByType[taskType] || []
      return fulfillJSON(route, { items, total: items.length })
    }
    if (path === detailPath && request.method() === 'GET') {
      return fulfillJSON(route, orchestration)
    }
    if (path === detailPath && request.method() === 'PUT') {
      persistedPayload = request.postDataJSON()
      if (persistGate) await persistGate
      orchestration = { ...orchestration, ...persistedPayload }
      return fulfillJSON(route, orchestration)
    }
    if (path === `${detailPath}/execute` && request.method() === 'POST') {
      executeRequestCount += 1
      if (executeGate) await executeGate
      return fulfillJSON(route, { execution_id: 'orchestration-execution-e2e' })
    }
    if (path === '/api/v1/orchestrator/orchestrations' && request.method() === 'GET') {
      return fulfillJSON(route, [orchestration])
    }
    if (path === `${detailPath}/executions` && request.method() === 'GET') {
      return fulfillJSON(route, { data: executions, total: executions.length })
    }
    const executionDetailMatch = path.match(/^\/api\/v1\/orchestrator\/orch-executions\/(\d+)$/)
    if (executionDetailMatch && request.method() === 'GET') {
      const execution = executions.find(item => item.id === Number(executionDetailMatch[1]))
      return fulfillJSON(route, execution || {})
    }

    return fulfillJSON(route, {})
  })

  return {
    getPersistedPayload: () => persistedPayload,
    getExecuteRequestCount: () => executeRequestCount,
    releasePersist,
    releaseExecute,
    releaseTaskRequests: () => pendingTaskRequests.splice(0).forEach(resolve => resolve())
  }
}

async function fulfillJSON(route, body) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}

async function requiredBoundingBox(locator) {
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  return box
}

function visibleDialogSurface(page) {
  return page.locator('.el-dialog.addp-dialog:visible')
}

async function expectDialogWithinViewport(page, dialog) {
  await expect(dialog).toHaveCount(1)
  await expect(dialog).toBeVisible()
  await expect.poll(() => dialog.evaluate(element => {
    const animations = []
    let current = element
    while (current && current !== document.body) {
      animations.push(...current.getAnimations({ subtree: false }))
      current = current.parentElement
    }
    return animations.every(animation => animation.playState === 'finished')
  })).toBe(true)
  const box = await requiredBoundingBox(dialog)
  const viewport = page.viewportSize()
  expect(viewport).not.toBeNull()
  expect(box.x).toBeGreaterThanOrEqual(12)
  expect(box.y).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width - 12)
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false)
}

async function canvasPixel(canvas, x, y) {
  return canvas.evaluate((element, point) => {
    const rect = element.getBoundingClientRect()
    const context = element.getContext('2d')
    const pixel = context.getImageData(
      Math.round(point.x * element.width / rect.width),
      Math.round(point.y * element.height / rect.height),
      1,
      1
    ).data
    return Array.from(pixel)
  }, { x, y })
}

function colorDistance(left, right) {
  return left.reduce((sum, channel, index) => sum + Math.abs(channel - right[index]), 0)
}

async function drag(page, start, delta) {
  await page.mouse.move(start.x, start.y)
  await page.mouse.down()
  await page.mouse.move(start.x + delta.x, start.y + delta.y, { steps: 8 })
  await page.mouse.up()
}
