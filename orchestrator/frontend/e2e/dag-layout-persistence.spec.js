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
    await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

    await page.getByRole('button', { name: '编辑名称与描述', exact: true }).click()
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
    await page.mouse.move(0, 0)
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
  let confirmDialog = page.getByRole('dialog', { name: '执行完整流程', exact: true })
  await expect(confirmDialog).toContainText(`将执行“${orchestration.name}”的全部 ${orchestration.steps.length} 个步骤`)
  await confirmDialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(confirmDialog).not.toBeVisible()
  expect(backend.getExecuteRequestCount()).toBe(0)

  await executeButton.click()
  confirmDialog = page.getByRole('dialog', { name: '执行完整流程', exact: true })
  await confirmDialog.getByRole('button', { name: '执行', exact: true }).click()
  await expect.poll(() => backend.getExecuteRequestCount()).toBe(1)
  await expect(executeButton).toBeDisabled()

  backend.releaseExecute()
  await expect(page.locator('.el-message').filter({ hasText: '执行已提交' })).toBeVisible()
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
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

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
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()

  const persistedPayload = backend.getPersistedPayload()
  expect(persistedPayload).not.toBeNull()
  expect(persistedPayload.editor_layout.viewport.zoom).toBeCloseTo(1.2, 8)
  expect(persistedPayload.editor_layout.nodes[NODE_ID].x).not.toBe(220)
  expect(persistedPayload.editor_layout.nodes[NODE_ID].y).not.toBe(160)
  expect(Math.abs(persistedPayload.editor_layout.viewport.translate_x)).toBeGreaterThan(0)
  expect(Math.abs(persistedPayload.editor_layout.viewport.translate_y)).toBeGreaterThan(0)

  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()
  await page.reload()
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()
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

test('loads execution contracts only for referenced steps and new tasks on demand', async ({ page }) => {
  const orchestration = createLayoutFixture()
  const taskLibrary = createPortBindingTaskLibrary()
  taskLibrary.tasksByType.query.push({ id: 9, task_type: 'query', display_name: 'Unused task' })
  taskLibrary.taskDetails[9] = taskLibrary.taskDetails[7]
  const backend = await installMockBackend(page, orchestration, taskLibrary)

  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()
  await expect.poll(() => backend.getTaskDetailRequestIDs()).toEqual([7])

  await page.locator('.task-panel').getByPlaceholder('搜索任务').fill('Unused task')
  const unusedTask = page.locator('.tree-node.task-node').filter({ hasText: 'Unused task' })
  await expect(unusedTask).toBeVisible()
  await unusedTask.getByRole('button', { name: '添加到画布', exact: true }).click()
  await expect.poll(() => backend.getTaskDetailRequestIDs()).toEqual([7, 9])
})

test('keeps a selected custom node visually stable without duplicate titles', async ({ page }) => {
  const orchestration = createLayoutFixture()
  await installMockBackend(page, orchestration, createPortBindingTaskLibrary())
  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  const node = orchestration.editor_layout.nodes[NODE_ID]
  const before = await orchestrationNodeAppearance(page, NODE_ID, 'Fixture task')
  await page.mouse.click(canvasBox.x + node.x, canvasBox.y + node.y)
  const after = await orchestrationNodeAppearance(page, NODE_ID, 'Fixture task')

  expect(after.cardFill).toBe(before.cardFill)
  expect(after.titleShapes).toEqual(['orchestration-node-title'])
  expect(after.titles).toEqual(['Fixture task'])
})

test('keeps orchestration dialog and canvas focus predictable', async ({ page }) => {
  const orchestration = createLayoutFixture()
  await installMockBackend(page, orchestration)
  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const saveTrigger = page.getByRole('button', { name: '编辑名称与描述', exact: true })
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
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  await page.getByRole('button', { name: '保存', exact: true }).click()

  await expect.poll(() => backend.getPersistedPayload()).not.toBeNull()
  await expect(page.locator('.orchestration-form')).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('status', { name: '编排状态' })).toHaveText('正在保存编排')

  backend.releasePersist()
  await expect(page).toHaveURL(/\/orchestrations\/[^/]+\/edit$/)
})

test('resizes the task library with the keyboard', async ({ page }) => {
  await installMockBackend(page, createLayoutFixture())
  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const splitter = page.getByRole('separator', { name: '调整任务库宽度', exact: true })
  const taskPanel = page.locator('#task-library-panel')
  await expect(splitter).toHaveAttribute('aria-valuenow', '360')
  await splitter.focus()

  await page.keyboard.press('ArrowRight')
  await expect(splitter).toHaveAttribute('aria-valuenow', '376')
  await expect(taskPanel).toHaveCSS('width', '376px')

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
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

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

  await connectFixturePorts(page, 'control')

  const undo = page.getByRole('button', { name: '撤销', exact: true })
  const redo = page.getByRole('button', { name: '重做', exact: true })
  await expect(undo).toBeEnabled()
  await undo.click()
  await expect(redo).toBeEnabled()
  await redo.click()
  await expect(redo).toBeDisabled()
  const controlEdges = await page.locator('#dag-container').evaluate(element => {
    const graph = element.__vueParentComponent?.setupState?.graph
    return (graph?.getEdges?.() || []).map(edge => edge.getModel())
  })
  expect(controlEdges).toHaveLength(1)
  expect(controlEdges[0]).toMatchObject({
    edgeKind: 'control',
    style: {
      endArrow: {
        path: 'M 0,0 L 9,4 L 9,-4 Z',
        d: -10
      }
    }
  })

  await page.mouse.click(canvasBox.x + source.x, canvasBox.y + source.y)
  const paste = page.getByRole('button', { name: '粘贴节点', exact: true })
  await expect(copy).toBeEnabled()
  await copy.click()
  await expect(paste).toBeEnabled()
  await paste.click()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()

  const persistedPayload = backend.getPersistedPayload()
  expect(persistedPayload).not.toBeNull()
  expect(persistedPayload.steps).toHaveLength(3)

  const targetStep = persistedPayload.steps.find(step => step.id === TARGET_NODE_ID)
  expect(targetStep.depends_on).toEqual([SOURCE_NODE_ID])

  const pastedStep = persistedPayload.steps.find(step => ![SOURCE_NODE_ID, TARGET_NODE_ID].includes(step.id))
  expect(pastedStep).toBeDefined()
  expect(pastedStep.depends_on).toEqual([])
})

test('binds a stable output to an execution input without duplicating the dependency', async ({ page }) => {
  const orchestration = createInteractionFixture()
  orchestration.editor_layout.nodes[TARGET_NODE_ID].x = 650
  const taskLibrary = createPortBindingTaskLibrary()
  const backend = await installMockBackend(page, orchestration, taskLibrary)
  await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  const source = orchestration.editor_layout.nodes[SOURCE_NODE_ID]
  const target = orchestration.editor_layout.nodes[TARGET_NODE_ID]

  await connectFixturePorts(page, 'control')
  await connectFixturePorts(page, 'parameter')

  const edges = await page.locator('#dag-container').evaluate(element => {
    const graph = element.__vueParentComponent?.setupState?.graph
    return (graph?.getEdges?.() || []).map(edge => edge.getModel())
  })
  expect(edges).toHaveLength(1)
  expect(edges[0].edgeKind).toBe('parameter')
  expect(edges[0].sourceOutput).toBe('result.resource.locator')
  expect(edges[0].targetInput).toBe('load.source')
  expect(edges[0].style.endArrow).toMatchObject({
    path: 'M 0,0 L 9,4 L 9,-4 Z',
    d: -10
  })
  const targetAppearance = await orchestrationNodeAppearance(page, TARGET_NODE_ID, 'Target task')
  expect(targetAppearance.titleShapes).toEqual(['orchestration-node-title'])
  expect(targetAppearance.titles).toEqual(['Target task'])

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()

  const targetStep = backend.getPersistedPayload().steps.find(step => step.id === TARGET_NODE_ID)
  expect(targetStep.depends_on).toEqual([SOURCE_NODE_ID])
  expect(targetStep.parameters).toEqual({
    load: {
      source: {
        locator: `{{${SOURCE_NODE_ID}.outputs.result.resource.locator}}`,
        geometry_column: 'geometry'
      }
    }
  })
})

test('deleting the final parameter edge clears its binding and implicit dependency', async ({ page }) => {
  const orchestration = createInteractionFixture()
  orchestration.editor_layout.nodes[TARGET_NODE_ID].x = 650
  const backend = await installMockBackend(page, orchestration, createPortBindingTaskLibrary())
  await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const { canvasBox, source, target } = await bindStableResourceOutput(page, orchestration)
  await page.mouse.click(
    canvasBox.x + (source.x + target.x) / 2,
    canvasBox.y + source.y + 31
  )
  const deleteButton = page.getByRole('button', { name: '删除选中项', exact: true })
  await expect(deleteButton).toBeEnabled()
  expect(await selectedGraphItemModel(page)).toMatchObject({
    edgeKind: 'parameter',
    target: TARGET_NODE_ID,
    targetInput: 'load.source'
  })
  await deleteButton.click()
  await expect.poll(() => canvasEdgeModels(page)).toEqual([])
  await expect.poll(() => orchestrationNodeParameters(page, TARGET_NODE_ID)).toEqual({})

  await saveOrchestration(page)
  const targetStep = backend.getPersistedPayload().steps.find(step => step.id === TARGET_NODE_ID)
  expect(targetStep.parameters).toEqual({})
  expect(targetStep.depends_on).toEqual([])
})

test('switching a bound parameter back to workflow configuration removes the edge', async ({ page }) => {
  const orchestration = createInteractionFixture()
  orchestration.editor_layout.nodes[TARGET_NODE_ID].x = 650
  const backend = await installMockBackend(page, orchestration, createPortBindingTaskLibrary())
  await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const { canvasBox, target } = await bindStableResourceOutput(page, orchestration)
  await page.mouse.dblclick(canvasBox.x + target.x, canvasBox.y + target.y)
  const drawer = page.getByRole('dialog', { name: '配置步骤', exact: true })
  const sourceParameter = drawer.locator('.parameter-field').filter({ hasText: '数据源' })
  await sourceParameter.getByText('任务默认值', { exact: true }).click()
  await expect(sourceParameter.locator('.el-radio-button').filter({ hasText: '任务默认值' })).toHaveClass(/is-active/)
  await expect.poll(() => canvasEdgeModels(page)).toEqual([])
  await expect.poll(() => orchestrationNodeParameters(page, TARGET_NODE_ID)).toEqual({})
  await drawer.getByRole('button', { name: 'Close this dialog', exact: true }).click()

  await saveOrchestration(page)
  const targetStep = backend.getPersistedPayload().steps.find(step => step.id === TARGET_NODE_ID)
  expect(targetStep.parameters).toEqual({})
  expect(targetStep.depends_on).toEqual([])
})

test('edits predecessor steps in the drawer and disables circular candidates', async ({ page }) => {
  const orchestration = createInteractionFixture()
  const backend = await installMockBackend(page, orchestration)
  await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

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
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()
  const persistedTarget = backend.getPersistedPayload().steps.find(step => step.id === TARGET_NODE_ID)
  expect(persistedTarget.depends_on).toEqual([SOURCE_NODE_ID])
})

test('persists a structured node parameter without a separate config save', async ({ page }) => {
  const orchestration = createLayoutFixture()
  const taskLibrary = createPortBindingTaskLibrary()
  taskLibrary.taskDetails[7].execution_contract = {
    input_schema: {
      type: 'object',
      properties: {
        limit: { type: 'integer', title: '数量限制', minimum: 1 }
      },
      additionalProperties: false
    },
    input_defaults: { limit: 10 },
    input_ui_schema: { limit: { order: 0 } },
    output_schema: { type: 'object', properties: {}, additionalProperties: false }
  }
  const backend = await installMockBackend(page, orchestration, taskLibrary)

  await page.goto(`/orchestrations/${ORCHESTRATION_ID}/edit`)
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  const node = orchestration.editor_layout.nodes[NODE_ID]
  await page.mouse.dblclick(canvasBox.x + node.x, canvasBox.y + node.y)

  const drawer = page.getByRole('dialog', { name: '配置步骤', exact: true })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  await drawer.getByPlaceholder('例如: 数据传输', { exact: true }).fill('Updated fixture task')

  const parameter = drawer.locator('.parameter-field').filter({ hasText: '数量限制' })
  await parameter.getByText('执行时指定', { exact: true }).click()
  await parameter.getByRole('spinbutton').fill('25')
  await expect(page.getByRole('button', { name: '撤销', exact: true })).toBeEnabled()
  await drawer.getByRole('button', { name: 'Close this dialog', exact: true }).click()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()

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
  await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()

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
      available: true,
      enabled: true,
      display_name: '开发中心',
      capabilities: {
        schema_version: 'task.capabilities/v2',
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
    },
    taskDetails: {
      11: { execution_contract: createEmptyExecutionContract() },
      12: { execution_contract: createEmptyExecutionContract() },
      21: { execution_contract: createEmptyExecutionContract() }
    }
  }
}

function createEmptyExecutionContract() {
  return {
    input_schema: { type: 'object', properties: {}, additionalProperties: false },
    input_defaults: {},
    input_ui_schema: {},
    output_schema: { type: 'object', properties: {}, additionalProperties: false }
  }
}

function createPortBindingTaskLibrary() {
  return {
    taskProviders: [{
      id: 1,
      module_name: 'develop',
      available: true,
      enabled: true,
      display_name: '开发中心',
      capabilities: {
        schema_version: 'task.capabilities/v2',
        task_capabilities: [{ type: 'query', display_name: 'SQL 查询' }]
      }
    }],
    tasksByType: {
      query: [
        { id: 7, task_type: 'query', display_name: 'Source task' },
        { id: 8, task_type: 'query', display_name: 'Target task' }
      ]
    },
    taskDetails: {
      7: {
        execution_contract: {
          input_schema: { type: 'object', properties: {}, additionalProperties: false },
          input_defaults: {},
          input_ui_schema: {},
          output_schema: {
            type: 'object',
            properties: {
              result: {
                type: 'object',
                title: '处理结果',
                properties: {
                  resource: {
                    type: 'object',
                    title: '资源',
                    properties: {
                      locator: { type: 'string', format: 'resource-locator' },
                      type: { type: 'string' }
                    }
                  }
                }
              }
            },
            additionalProperties: false
          }
        }
      },
      8: {
        execution_contract: {
          input_schema: {
            type: 'object',
            properties: {
              load: {
                type: 'object',
                properties: {
                  source: {
                    type: 'object',
                    properties: {
                      locator: { type: 'string' },
                      geometry_column: { type: 'string' }
                    }
                  }
                }
              }
            },
            additionalProperties: false
          },
          input_defaults: {
            load: { source: { locator: 'addp://configured', geometry_column: 'geometry' } }
          },
          input_ui_schema: {
            load: {
              control: 'group',
              title: '数据加载',
              order: 0,
              fields: {
                source: {
                  control: 'resource_tree_picker',
                  display_name: '数据源',
                  order: 0,
                  resource_binding: { mode: 'existing' }
                }
              }
            }
          },
          output_schema: { type: 'object', properties: {}, additionalProperties: false }
        }
      }
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
  const taskDetails = taskLibrary.taskDetails || Object.fromEntries(
    (initialOrchestration.steps || [])
      .filter(step => step.task_id != null)
      .map(step => [step.task_id, { execution_contract: createEmptyExecutionContract() }])
  )
  const executions = taskLibrary.executions || []
  const pendingTaskRequests = []
  const taskDetailRequestIDs = []
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
    if (path === '/api/v1/system/auth/context') return fulfillJSON(route, { context: { type: 'tenant' }, authorization: { role_assignments: [{ permissions: ['orchestrator.workflow.read', 'orchestrator.workflow.execute'] }] } })
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
    const taskDetailMatch = path.match(/^\/api\/v1\/orchestrator\/task-providers\/[^/]+\/tasks\/[^/]+\/(\d+)$/)
    if (taskDetailMatch) {
      const taskID = Number(taskDetailMatch[1])
      taskDetailRequestIDs.push(taskID)
      return fulfillJSON(route, taskDetails[taskID] || {})
    }
    if (path === detailPath && request.method() === 'GET') {
      return fulfillJSON(route, orchestration)
    }
    if ((path === detailPath && request.method() === 'PUT') ||
        (path === '/api/v1/orchestrator/orchestrations' && request.method() === 'POST')) {
      persistedPayload = request.postDataJSON()
      if (persistGate) await persistGate
      orchestration = { ...orchestration, ...persistedPayload }
      return fulfillJSON(route, orchestration)
    }
    if (path === `${detailPath}/execute` && request.method() === 'POST') {
      executeRequestCount += 1
      if (executeGate) await executeGate
      return fulfillJSON(route, { id: EXECUTION_ID, execution_id: 'orchestration-execution-e2e', status: 'pending' })
    }
    if (path === '/api/v1/orchestrator/orchestrations' && request.method() === 'GET') {
      return fulfillJSON(route, taskLibrary.orchestrations || [orchestration])
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
    getTaskDetailRequestIDs: () => [...taskDetailRequestIDs],
    releasePersist,
    releaseExecute,
    releaseTaskRequests: () => pendingTaskRequests.splice(0).forEach(resolve => resolve())
  }
}

async function orchestrationNodeAppearance(page, nodeID, label) {
  return page.locator('#dag-container').evaluate((element, context) => {
    const graph = element.__vueParentComponent?.setupState?.graph
    const children = graph?.findById?.(context.nodeID)?.getContainer?.()?.get?.('children') || []
    const card = children.find(shape => shape.get?.('name') === 'orchestration-node-card')
    const titleShapes = children.filter(shape => String(shape.attr?.('text') || '').includes(context.label))
    return {
      cardFill: card?.attr?.('fill'),
      titleShapes: titleShapes.map(shape => shape.get?.('name')),
      titles: titleShapes.map(shape => shape.attr?.('text'))
    }
  }, { nodeID, label })
}

async function connectFixturePorts(page, kind) {
  const positions = await page.locator('#dag-container').evaluate((element, kind) => {
    const graph = element.__vueParentComponent.setupState.graph
    return [['source-step', 'output'], ['target-step', 'input']].map(([id, direction]) => {
      const node = graph.findById(id)
      const shape = node.getContainer().get('children').find(shape => shape.get('portDirection') === direction && shape.get('portKind') === kind)
      const box = shape.getBBox()
      const model = node.getModel()
      return graph.getClientByPoint(model.x + (box.minX + box.maxX) / 2, model.y + (box.minY + box.maxY) / 2)
    })
  }, kind)
  await drag(page, positions[0], { x: positions[1].x - positions[0].x, y: positions[1].y - positions[0].y })
}

async function bindStableResourceOutput(page, orchestration) {
  const canvas = page.locator('#dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  const source = orchestration.editor_layout.nodes[SOURCE_NODE_ID]
  const target = orchestration.editor_layout.nodes[TARGET_NODE_ID]
  await connectFixturePorts(page, 'parameter')
  await expect.poll(async () => (await canvasEdgeModels(page)).length).toBe(1)
  return { canvas, canvasBox, source, target }
}

async function canvasEdgeModels(page) {
  return page.locator('#dag-container').evaluate(element => {
    const graph = element.__vueParentComponent?.setupState?.graph
    return (graph?.getEdges?.() || []).map(edge => edge.getModel())
  })
}

async function orchestrationNodeParameters(page, nodeID) {
  return page.locator('#dag-container').evaluate((element, id) => {
    const graph = element.__vueParentComponent?.setupState?.graph
    return graph?.findById?.(id)?.getModel?.()?.parameters || {}
  }, nodeID)
}

async function selectedGraphItemModel(page) {
  return page.locator('#dag-container').evaluate(element => {
    const selectedItem = element.__vueParentComponent?.setupState?.selectedItem
    return selectedItem?.getModel?.() || null
  })
}

async function saveOrchestration(page) {
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()
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


test('related orchestration filter matches complete task identities and survives navigation', async ({ page }) => {
  const orchestration = createLayoutFixture()
  orchestration.steps[0].provider = 'quality'
  orchestration.steps[0].task_type = 'data_validation'
  orchestration.steps[0].task_id = 17
  const others = [
    { ...orchestration, id: 'different-provider', name: '其他模块', steps: [{ provider: 'develop', task_type: 'data_validation', task_id: 17 }] },
    { ...orchestration, id: 'different-type', name: '其他任务类型', steps: [{ provider: 'quality', task_type: 'check', task_id: 17 }] },
    { ...orchestration, id: 'different-id', name: '其他校验任务', steps: [{ provider: 'quality', task_type: 'data_validation', task_id: 18 }] }
  ]
  await installMockBackend(page, orchestration, { orchestrations: [orchestration, ...others] })
  const filter = 'module=quality&task_type=data_validation&task_id=17'
  await page.goto(`/orchestrations?${filter}`)
  await expect(page.getByRole('cell', { name: orchestration.name, exact: true })).toBeVisible()
  for (const other of others) await expect(page.getByRole('cell', { name: other.name, exact: true })).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole('button', { name: '执行', exact: true })).toHaveCount(1)
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/edit\\?${filter}$`))
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/orchestrations\\?${filter}$`))
  await page.getByRole('button', { name: '查看全部编排', exact: true }).click()
  await expect(page.getByRole('button', { name: '执行', exact: true })).toHaveCount(4)
})

test('related orchestration filter shows empty and invalid contexts explicitly', async ({ page }) => {
  await installMockBackend(page, createLayoutFixture())
  await page.goto('/orchestrations?module=quality&task_type=data_validation&task_id=999')
  await expect(page.getByText('暂无关联编排，请先创建包含该任务的编排。', { exact: true })).toBeVisible()
  await page.goto('/orchestrations?module=quality&task_id=999')
  await expect(page.getByRole('alert').filter({ hasText: '任务筛选无效' })).toBeVisible()
  await expect(page.getByRole('button', { name: '执行', exact: true })).toHaveCount(0)
})

test('related orchestration preserves scalar resource locators when binding upstream', async ({ page }) => {
 const orchestration=createInteractionFixture()
 orchestration.steps[1].depends_on=[SOURCE_NODE_ID]
 const library=createPortBindingTaskLibrary()
 library.taskDetails[8].execution_contract={
  input_schema:{type:'object',properties:{target_locator:{type:'string',title:'正式输出表'}},required:['target_locator'],additionalProperties:false},
  input_defaults:{target_locator:'addp://engine/2/path/public/result?type=table'},
  input_ui_schema:{target_locator:{control:'resource_tree_picker',order:0}},
  output_schema:{type:'object',properties:{},additionalProperties:false}
 }
 const backend=await installMockBackend(page,orchestration,library)
 await page.goto(`/orchestrations/${INTERACTION_ORCHESTRATION_ID}/edit`)
 await expect(page.locator('.orchestration-form .orchestration-name')).toBeVisible()
 const box=await requiredBoundingBox(page.locator('#dag-container canvas'))
 const node=orchestration.editor_layout.nodes[TARGET_NODE_ID]
 await page.mouse.dblclick(box.x+node.x,box.y+node.y)
 const drawer=page.getByRole('dialog',{name:'配置步骤',exact:true})
 const field=drawer.locator('.parameter-field').filter({hasText:'正式输出表'})
 await expect(field).toContainText('public.result')
 await field.getByText('上游输出', { exact: true }).click()
 await expect.poll(()=>orchestrationNodeParameters(page,TARGET_NODE_ID)).toEqual({target_locator:`{{${SOURCE_NODE_ID}.outputs.result.resource.locator}}`})
 await drawer.getByRole('button',{name:'Close this dialog',exact:true}).click()
 await saveOrchestration(page)
 expect(backend.getPersistedPayload().steps.find(s=>s.id===TARGET_NODE_ID).parameters.target_locator).toBe(`{{${SOURCE_NODE_ID}.outputs.result.resource.locator}}`)
})

test('editor usability: identifies the orchestration and saves before manual execution', async ({ page }) => {
  const fixture = createLayoutFixture()
  fixture.schedule = '0 2 * * *'
  const backend = await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await expect(page.locator('.header > .header-title')).toContainText(fixture.name)
  await expect(page.locator('.header-summary')).toContainText('定时调度已关闭')
  const execute = page.locator('.header-actions').getByRole('button', { name: '执行', exact: true })
  await expect(execute).toBeEnabled()
  await page.getByRole('button', { name: '调度', exact: true }).click()
  await page.getByRole('dialog').locator('.el-switch').click()
  await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
  await expect(execute).toBeDisabled()
  await expect(page.locator('.header-summary')).toContainText('有未保存的修改')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()
  await expect(execute).toBeEnabled()
  await execute.click()
  await page.getByRole('dialog', { name: '执行完整流程', exact: true }).getByRole('button', { name: '执行', exact: true }).click()
  await expect.poll(() => backend.getExecuteRequestCount()).toBe(1)
  expect(backend.getPersistedPayload().enabled).toBe(true)
})

test('editor usability: wraps task names and traces selected connections', async ({ page }) => {
  const fixture = createInteractionFixture()
  fixture.steps[1].depends_on = [SOURCE_NODE_ID]
  const library = createTaskLibraryFixture()
  library.taskDetails[7] = { execution_contract: createEmptyExecutionContract() }
  library.taskDetails[8] = { execution_contract: createEmptyExecutionContract() }
  library.deferTaskRequests = false
  library.taskProviders[0].available = true
  library.taskProviders[0].enabled = true
  const taskName = '验证户外人员活动明细与组织成员关系数据完整性任务'
  library.tasksByType.query[0].display_name = taskName
  library.tasksByType.query[0].last_execution_status = 'success'
  await installMockBackend(page, fixture, library)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await page.locator('.task-panel').getByPlaceholder('搜索任务').fill(taskName)
  const task = page.locator('.task-node').filter({ hasText: taskName })
  await expect(task).toContainText('最近执行：成功')
  await expect(task.locator('.task-name')).toHaveCSS('white-space', 'normal')
  expect(await task.locator('.task-name').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  await page.locator('#dag-container').evaluate(el => {
    const state = el.__vueParentComponent.setupState
    state.selectItem(state.graph.getEdges()[0])
  })
  await expect(page.locator('.connection-inspector')).toContainText('Source task')
  await expect(page.locator('.connection-inspector')).toContainText('Target task')
  await expect(page.locator('.connection-inspector')).toContainText('执行依赖')
  await page.getByRole('button', { name: '自动布局', exact: true }).click()
  await expect.poll(async () => {
    const nodes = await page.locator('#dag-container').evaluate(el => el.__vueParentComponent.setupState.graph.getNodes().map(node => node.getBBox()))
    return nodes[1].minX - nodes[0].maxX
  }).toBeGreaterThan(60)
  await expect(task.getByRole('button', { name: '添加到画布' })).toBeVisible()
  await expect(task.getByRole('button', { name: '添加到画布' })).toBeInViewport({ ratio: 1 })
  const bounds = await page.locator('#dag-container').evaluate(el => {
    const graph = el.__vueParentComponent.setupState.graph
    return graph.getNodes().map(node => {
      const box = node.getBBox()
      return { left: graph.getCanvasByPoint(box.minX, box.minY).x, right: graph.getCanvasByPoint(box.maxX, box.maxY).x, width: el.clientWidth }
    })
  })
  expect(bounds.every(box => box.left >= 0 && box.right <= box.width)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('editor-usability.png') })
})

test('editor refinements: existing orchestration saves directly under its own name', async ({ page }) => {
  const fixture = createLayoutFixture()
  const backend = await installMockBackend(page, fixture, { deferPersist: true })
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await expect(page.locator('.orchestration-form .header').getByRole('heading')).toHaveText(fixture.name)
  const save = page.getByRole('button', { name: '保存', exact: true })
  await save.click()
  await expect.poll(() => backend.getPersistedPayload()?.name).toBe(fixture.name)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(save).toBeDisabled()
  backend.releasePersist()
  await expect(page.locator('.el-message').filter({ hasText: '更新成功' })).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`/orchestrations/${fixture.id}/edit$`))
})

test('editor refinements: hovering never changes the clicked connection focus', async ({ page }) => {
  const fixture = createInteractionFixture()
  fixture.steps[1].depends_on = [SOURCE_NODE_ID]
  await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  const canvas = page.locator('#dag-container canvas')
  await expect(canvas).toBeVisible()
  const box = await requiredBoundingBox(canvas)
  const source = fixture.editor_layout.nodes[SOURCE_NODE_ID]
  const target = fixture.editor_layout.nodes[TARGET_NODE_ID]
  await page.mouse.move(box.x + source.x, box.y + source.y)
  await expect(page.locator('.connection-inspector')).not.toBeVisible()
  await page.mouse.click(box.x + source.x, box.y + source.y)
  const title = page.locator('.connection-inspector strong')
  await expect(title).toHaveText(fixture.steps[0].name)
  await page.mouse.move(box.x + target.x, box.y + target.y)
  await expect(title).toHaveText(fixture.steps[0].name)
  // Exercise edge hover too, regardless of the curve's layout.
  await page.locator('#dag-container').evaluate(el => {
    const graph = el.__vueParentComponent.setupState.graph
    const item = graph.getEdges()[0]
    graph.emit('edge:mouseenter', { item })
    graph.emit('edge:mouseleave', { item })
  })
  await expect(title).toHaveText(fixture.steps[0].name)
  await page.mouse.click(box.x + target.x, box.y + target.y)
  await expect(title).toHaveText(fixture.steps[1].name)
})

test('editor refinements: blank canvas clears node and edge focus', async ({ page }) => {
  const fixture = createInteractionFixture()
  fixture.steps[1].depends_on = [SOURCE_NODE_ID]
  await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  const container = page.locator('#dag-container')
  for (const kind of ['node', 'edge']) {
    await container.evaluate((el, kind) => {
      const state = el.__vueParentComponent.setupState
      state.selectItem(kind === 'node' ? state.graph.getNodes()[0] : state.graph.getEdges()[0])
    }, kind)
    await expect(page.locator('.connection-inspector')).toBeVisible()
    const box = await requiredBoundingBox(container)
    await page.mouse.click(box.x + box.width - 30, box.y + 30)
    await expect(page.locator('.connection-inspector')).not.toBeVisible()
    await expect(page.getByRole('button', { name: '删除选中项', exact: true })).toBeDisabled()
    expect(await container.evaluate(el => {
      const graph = el.__vueParentComponent.setupState.graph
      return [...graph.getNodes(), ...graph.getEdges()].every(item => !item.hasState('selected') && item.getContainer().attr('opacity') === 1)
    })).toBe(true)
  }
})

test('editor refinements: metadata editing remains explicit and validates the name', async ({ page }) => {
  const fixture = createLayoutFixture()
  const backend = await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await page.getByRole('button', { name: '编辑名称与描述', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '保存编排信息', exact: true })
  await dialog.getByPlaceholder('请输入编排名称').fill('')
  await dialog.getByRole('button', { name: '确认保存', exact: true }).click()
  await expect(dialog).toBeVisible()
  expect(backend.getPersistedPayload()).toBeNull()
  await dialog.getByPlaceholder('请输入编排名称').fill('更新后的编排名称')
  await dialog.getByPlaceholder('请输入描述信息').fill('更新后的描述')
  await dialog.getByRole('button', { name: '确认保存', exact: true }).click()
  await expect(page.getByRole('heading', { name: '更新后的编排名称', exact: true })).toBeVisible()
  await expect.poll(() => backend.getPersistedPayload()?.description).toBe('更新后的描述')
})

test('editor refinements: a new orchestration requests its name on first save', async ({ page }) => {
  const backend = await installMockBackend(page, createLayoutFixture(), createPortBindingTaskLibrary())
  await page.goto('/orchestrations/new')
  await expect(page.getByRole('heading', { name: '创建编排', exact: true })).toBeVisible()
  await page.getByPlaceholder('搜索任务').fill('Source task')
  await page.locator('.task-node').filter({ hasText: 'Source task' }).getByRole('button', { name: '添加到画布' }).click()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '保存编排信息', exact: true })
  await expect(dialog.getByPlaceholder('请输入编排名称')).toBeFocused()
  expect(backend.getPersistedPayload()).toBeNull()
  await dialog.getByPlaceholder('请输入编排名称').fill('新建编排')
  await dialog.getByRole('button', { name: '确认保存', exact: true }).click()
  await expect.poll(() => backend.getPersistedPayload()?.name).toBe('新建编排')
  await expect(page).toHaveURL(/\/orchestrations$/)
})

test('rejected creation shows the owner validation error and retains the draft', async ({ page }) => {
  await installMockBackend(page, createLayoutFixture(), createPortBindingTaskLibrary())
  await page.route('**/api/v1/orchestrator/orchestrations', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({ status: 400, json: { error: '上游输出类型与质量方案输入不匹配' } })
    } else await route.fallback()
  })
  await page.goto('/orchestrations/new')
  await page.getByPlaceholder('搜索任务').fill('Source task')
  await page.locator('.task-node').filter({ hasText: 'Source task' }).getByRole('button', { name: '添加到画布' }).click()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '保存编排信息', exact: true })
  await dialog.getByPlaceholder('请输入编排名称').fill('保留失败草稿')
  await dialog.getByRole('button', { name: '确认保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: '上游输出类型与质量方案输入不匹配' })).toBeVisible()
  await expect(page).toHaveURL(/\/orchestrations\/new$/)
  await expect(page.locator('.unsaved-status')).toBeVisible()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog.getByPlaceholder('请输入编排名称')).toHaveValue('保留失败草稿')
})

test('unsaved changes: moving a node protects cancellation, saving releases it', async ({ page }) => {
  const fixture = createLayoutFixture()
  await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await expect(page.locator('.unsaved-status')).toHaveCount(0)
  await page.locator('#dag-container').evaluate(el => {
    const state = el.__vueParentComponent.setupState
    const node = state.graph.getNodes()[0]
    state.graph.updateItem(node, { x: node.getModel().x + 70 })
    state.graph.emit('node:dragend', { item: node })
  })
  await expect(page.locator('.unsaved-status')).toBeVisible()
  await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
  const warning = page.getByRole('dialog', { name: '有未保存的修改', exact: true })
  await expect(warning).toBeVisible()
  await warning.getByRole('button', { name: '继续编辑' }).click()
  await expect(page).toHaveURL(new RegExp(`/${fixture.id}/edit$`))
  await expect(page.locator('.unsaved-status')).toBeVisible()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.unsaved-status')).toHaveCount(0)
  await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page).toHaveURL(/\/orchestrations$/)
  await expect(warning).toHaveCount(0)
})

test('unsaved changes: viewport changes are clean and failed saves keep protection', async ({ page }) => {
  const fixture = createLayoutFixture()
  await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await page.locator('#dag-container').evaluate(el => {
    const state = el.__vueParentComponent.setupState
    state.graph.zoomTo(0.8)
    state.graph.translate(10, 20)
    state.graph.emit('canvas:dragend', {})
  })
  await expect(page.locator('.unsaved-status')).toHaveCount(0)
  await page.getByRole('button', { name: '调度', exact: true }).click()
  const schedule = page.getByRole('dialog', { name: '设置定时调度', exact: true })
  await schedule.getByRole('button', { name: '每小时', exact: true }).click()
  await schedule.getByRole('button', { name: '确认', exact: true }).click()
  await page.route('**/api/v1/orchestrator/orchestrations/' + fixture.id, async route => {
    if (route.request().method() === 'PUT') await route.fulfill({ status: 500, json: { error: 'test failure' } })
    else await route.fallback()
  })
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message').filter({ hasText: 'test failure' })).toBeVisible()
  await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
  const warning = page.getByRole('dialog', { name: '有未保存的修改', exact: true })
  await warning.getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/\/orchestrations$/)
})

test('unsaved changes: metadata drafts protect refresh and cancel discards only the draft', async ({ page }) => {
  const fixture = createLayoutFixture()
  await installMockBackend(page, fixture)
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await page.getByRole('button', { name: '编辑名称与描述', exact: true }).click()
  const metadata = page.getByRole('dialog', { name: '保存编排信息', exact: true })
  await metadata.getByPlaceholder('请输入编排名称').fill('尚未确认的名称')
  const prompt = page.waitForEvent('dialog')
  await page.evaluate(() => { setTimeout(() => location.reload(), 0) })
  const unload = await prompt
  expect(unload.type()).toBe('beforeunload')
  await unload.dismiss()
  await expect(metadata.getByPlaceholder('请输入编排名称')).toHaveValue('尚未确认的名称')
  await metadata.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.locator('.unsaved-status')).toHaveCount(0)
  await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page).toHaveURL(/\/orchestrations$/)
})

async function startLiveExecution(page) {
  await page.locator('.header-actions').getByRole('button', { name: '执行', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '执行', exact: true }).click()
  await expect(page.locator('.execution-summary')).toBeVisible()
}
async function executionLabels(page) {
  return page.locator('#dag-container').evaluate(el => Object.fromEntries(
    el.__vueParentComponent.setupState.graph.getNodes().map(node => [node.getID(),
      node.getContainer().find(shape => shape.get('name') === 'execution-label')?.attr('text') || ''])
  ))
}

async function executionPulses(page) {
  return page.locator('#dag-container').evaluate(el => Object.fromEntries(
    el.__vueParentComponent.setupState.graph.getNodes().flatMap(node => {
      const pulse = node.getContainer().find(shape => shape.get('name') === 'execution-pulse')
      return pulse ? [[node.getID(), { opacity: pulse.attr('opacity'), animations: pulse.get('animations')?.length || 0 }]] : []
    })
  ))
}

test('live execution displays running and completed nodes, exposes failure, and stops polling at terminal state', async ({ page }) => {
  const fixture = createInteractionFixture()
  fixture.steps[1].depends_on = [SOURCE_NODE_ID]
  await installMockBackend(page, fixture)
  let requests = 0
  let state = { id: EXECUTION_ID, execution_id: 'orchestration-execution-e2e', status: 'running', current_step: SOURCE_NODE_ID, metadata: { step_results: {} } }
  await page.route('**/api/v1/orchestrator/orch-executions/' + EXECUTION_ID, route => { requests++; return fulfillJSON(route, state) })
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await startLiveExecution(page)
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '运行中', [TARGET_NODE_ID]: '等待执行' })
  await expect.poll(async () => Object.keys(await executionPulses(page))).toEqual([SOURCE_NODE_ID])
  const initialOpacity = (await executionPulses(page))[SOURCE_NODE_ID].opacity
  await expect.poll(async () => Math.abs((await executionPulses(page))[SOURCE_NODE_ID].opacity - initialOpacity)).toBeGreaterThan(0.1)
  state = { ...state, current_step: TARGET_NODE_ID, metadata: { step_results: { [SOURCE_NODE_ID]: { status: 'success', duration: 25 } } } }
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '成功', [TARGET_NODE_ID]: '运行中' })
  await expect.poll(async () => Object.keys(await executionPulses(page))).toEqual([TARGET_NODE_ID])
  await expect(page.locator('.execution-summary')).toContainText('已结束 1/2 个步骤')
  await expect(page.locator('.header-actions').getByRole('button', { name: '执行', exact: true })).toBeDisabled()
  state = { ...state, status: 'failed', error_details: { message: 'Fixture input failed' }, metadata: { step_results: {
    ...state.metadata.step_results, [TARGET_NODE_ID]: { status: 'failed', error: 'Fixture input failed', duration: 100 }
  } } }
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '成功', [TARGET_NODE_ID]: '失败' })
  await page.locator('#dag-container').evaluate((el, id) => {
    const setup = el.__vueParentComponent.setupState
    setup.selectItem(setup.graph.findById(id))
  }, TARGET_NODE_ID)
  await expect(page.locator('.node-execution-detail')).toContainText('Fixture input failed')
  await expect(page.locator('.node-execution-detail')).toContainText('100 毫秒')
  await page.screenshot({ path: test.info().outputPath('live-execution-failure.png') })
  await page.setViewportSize({ width: 620, height: 560 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false)
  await expect(page.locator('.execution-summary')).toBeInViewport()
  await page.screenshot({ path: test.info().outputPath('live-execution-narrow.png') })
  await page.setViewportSize({ width: 1280, height: 800 })
  await expect(page.locator('.unsaved-status')).toHaveCount(0)
  expect(await executionPulses(page)).toEqual({})
  const terminalCount = requests
  await page.waitForTimeout(2300)
  expect(requests).toBe(terminalCount)
  const modelJSON = await page.locator('#dag-container').evaluate(el => JSON.stringify(el.__vueParentComponent.setupState.graph.save()))
  expect(modelJSON).not.toContain('Fixture input failed')
  expect(modelJSON).not.toContain('execution-badge')
  expect(modelJSON).not.toContain('execution-pulse')
  await page.getByRole('button', { name: '收起状态', exact: true }).click()
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '', [TARGET_NODE_ID]: '' })
})

test('live execution retains last state on refresh failure, retries, and hides stale labels when the definition changes', async ({ page }) => {
  const fixture = createInteractionFixture()
  await installMockBackend(page, fixture)
  let fail = false
  let requests = 0
  const state = { id: EXECUTION_ID, execution_id: 'orchestration-execution-e2e', status: 'running', current_step: SOURCE_NODE_ID, metadata: { step_results: {} } }
  await page.route('**/api/v1/orchestrator/orch-executions/' + EXECUTION_ID, route => {
    requests++
    return fail ? route.fulfill({ status: 503, json: { error: 'temporary failure' } }) : fulfillJSON(route, state)
  })
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await startLiveExecution(page)
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '运行中', [TARGET_NODE_ID]: '等待执行' })
  fail = true
  await expect(page.locator('.execution-refresh-error')).toBeVisible()
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '运行中', [TARGET_NODE_ID]: '等待执行' })
  fail = false
  await page.getByRole('button', { name: '重试刷新' }).click()
  await expect(page.locator('.execution-refresh-error')).toHaveCount(0)
  await page.getByRole('button', { name: '调度', exact: true }).click()
  const schedule = page.getByRole('dialog', { name: '设置定时调度', exact: true })
  await schedule.getByRole('button', { name: '每小时', exact: true }).click()
  await schedule.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page.locator('.execution-summary')).toContainText('编排配置已修改')
  await expect.poll(() => executionLabels(page)).toEqual({ [SOURCE_NODE_ID]: '', [TARGET_NODE_ID]: '' })
  await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('dialog', { name: '有未保存的修改' }).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/\/orchestrations$/)
  const leftCount = requests
  await page.waitForTimeout(2300)
  expect(requests).toBe(leftCount)
})

test('live execution does not attach a delayed submission to another orchestration page', async ({ page }) => {
  const fixture = createInteractionFixture()
  const backend = await installMockBackend(page, fixture, { deferExecute: true })
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await page.locator('.header-actions').getByRole('button', { name: '执行', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '执行', exact: true }).click()
  await expect.poll(() => backend.getExecuteRequestCount()).toBe(1)
  await page.locator('.orchestration-form').evaluate(el => { void el.__vueParentComponent.proxy.$router.push('/orchestrations/new') })
  await expect(page.getByRole('heading', { name: '创建编排', exact: true })).toBeVisible()
  backend.releaseExecute()
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await expect(page.locator('.execution-summary')).toHaveCount(0)
})

test('live execution uses a static running outline for reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const fixture = createInteractionFixture()
  await installMockBackend(page, fixture)
  await page.route('**/api/v1/orchestrator/orch-executions/' + EXECUTION_ID, route => fulfillJSON(route, {
    id: EXECUTION_ID, execution_id: 'orchestration-execution-e2e', status: 'running',
    current_step: SOURCE_NODE_ID, metadata: { step_results: {} }
  }))
  await page.goto(`/orchestrations/${fixture.id}/edit`)
  await startLiveExecution(page)
  await expect.poll(() => executionPulses(page)).toEqual({ [SOURCE_NODE_ID]: { opacity: 1, animations: 0 } })
  await page.getByRole('button', { name: '调度', exact: true }).click()
  const schedule = page.getByRole('dialog', { name: '设置定时调度', exact: true })
  await schedule.getByRole('button', { name: '每小时', exact: true }).click()
  await schedule.getByRole('button', { name: '确认', exact: true }).click()
  await expect.poll(() => executionPulses(page)).toEqual({})
})
