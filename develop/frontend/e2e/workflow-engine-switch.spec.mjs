import { expect, test } from '@playwright/test'

const SAVED_TASK_ID = 42
const ENGINE_A = {
  id: 1,
  name: 'Engine A',
  engine_type: 'engine_a',
  lifecycle_state: 'active',
  connection_status: 'online'
}
const ENGINE_B = {
  id: 2,
  name: 'Engine B',
  engine_type: 'engine_b',
  lifecycle_state: 'active',
  connection_status: 'online'
}
const RESOURCE_ENGINE = {
  id: 7,
  name: 'Fixture PostgreSQL',
  engine_type: 'postgresql',
  engine_family: 'tabular',
  lifecycle_state: 'active',
  connection_status: 'online'
}
const RESOURCE_LOCATOR = `addp://engine/${RESOURCE_ENGINE.id}/path/public/roads?type=table&item_id=701`

test.describe('responsive workflow dialogs', () => {
  test.use({ viewport: { width: 620, height: 560 }, colorScheme: 'dark' })

  test('keeps workflow dialogs visible and visually stable in a narrow window', async ({ page }) => {
    await installMockBackend(page)
    await openSavedWorkflow(page)

    await requestEngineSwitch(page, ENGINE_B)
    const switchDialog = engineSwitchDialog(page)
    const switchSurface = visibleDialogSurface(page)
    await expectDialogWithinViewport(page, switchSurface)
    await expect(switchSurface).toHaveScreenshot('workflow-engine-switch-narrow.png', { animations: 'disabled' })
    await switchDialog.getByRole('button', { name: '取消', exact: true }).click()
    await expect(switchDialog).not.toBeVisible()

    await primaryExecuteButton(page).click()
    const executeDialog = page.getByRole('dialog', { name: '执行工作流', exact: true })
    const executeSurface = visibleDialogSurface(page)
    await expectDialogWithinViewport(page, executeSurface)
    await expect(executeSurface.locator('.el-dialog__body')).toHaveCSS('overflow', 'auto')
    await expect(executeSurface).toHaveScreenshot('workflow-execute-narrow.png', { animations: 'disabled' })
    await executeDialog.getByRole('button', { name: '取消', exact: true }).click()
    await expect(executeDialog).not.toBeVisible()

    await page.getByRole('button', { name: '更多', exact: true }).click()
    await page.getByRole('menuitem', { name: '清空', exact: true }).click()
    const clearDialog = page.getByRole('dialog', { name: '确认清空', exact: true })
    const clearSurface = page.locator('.el-message-box.addp-message-box:visible')
    await expectDialogWithinViewport(page, clearSurface)
    await expect(clearDialog.getByRole('button', { name: '取消', exact: true })).toBeVisible()
    await expect(clearDialog.getByRole('button', { name: '确定', exact: true })).toHaveClass(/el-button--danger/)
    await expect(clearSurface).toHaveScreenshot('workflow-clear-confirm-narrow.png', { animations: 'disabled' })
  })

  test('keeps the resource picker stable after focusing a validation issue', async ({ page }) => {
    const validationError = {
      code: 'REQUIRED_PARAMETER',
      path: 'tasks[0].params.source_locator',
      message: 'Source locator is required'
    }
    await installMockBackend(page, {
      validationErrors: [validationError],
      includeResourcePicker: true
    })
    await openSavedWorkflow(page)

    await primaryExecuteButton(page).click()
    const validation = page.locator('.header-validation')
    await expect(validation).toContainText(validationError.message)
    await validation.getByRole('button').click()
    const issue = page.locator('.validation-item').filter({ hasText: validationError.message })
    await expect(issue).toHaveCount(1)
    await issue.click()

    const resourceCard = page.locator('.resource-selection-card')
    await expect(resourceCard).toContainText(RESOURCE_ENGINE.name)
    await expect(resourceCard).toContainText('public.roads')

    const nodePresentation = await page.locator('#workflow-dag-container').evaluate(element => {
      const graph = element.__vueParentComponent?.setupState?.graph
      const node = graph?.findById?.('operator_a_1')
      const summaries = node?.getModel?.()?.parameterSummaries || []
      const visibleSummaryText = (node?.getContainer?.()?.get?.('children') || [])
        .filter(shape => String(shape.get('name') || '').startsWith('workflow-node-summary-'))
        .map(shape => shape.attr('text'))
        .filter(Boolean)
      return { summaries, visibleSummaryText }
    })
    expect(nodePresentation.summaries).toEqual([
      expect.objectContaining({ label: 'source', value: 'manual-source', kind: 'value' }),
      expect.objectContaining({
        label: '数据源',
        engineName: RESOURCE_ENGINE.name,
        resourceName: 'roads',
        path: 'public.roads',
        kind: 'resource'
      })
    ])
    expect(nodePresentation.visibleSummaryText.join(' ')).toContain('manual-source')
    expect(nodePresentation.visibleSummaryText[1]).toBe('public.roads')
    expect(nodePresentation.visibleSummaryText.join(' ')).toContain(RESOURCE_ENGINE.name)
    expect(nodePresentation.visibleSummaryText.join(' ')).toContain('public.roads')
    expect(nodePresentation.visibleSummaryText.join(' ')).not.toContain('addp://')

    const selectResource = page.getByRole('button', { name: '更换资源', exact: true })
    await expect(selectResource).toBeVisible()
    await selectResource.click()
    const resourceDialog = page.getByRole('dialog', { name: '选择数据源', exact: true })
    await expect(resourceDialog.getByText(RESOURCE_ENGINE.name, { exact: true })).toBeVisible()
    await expect(resourceDialog.locator('.el-tag__content')).toContainText(RESOURCE_ENGINE.name)
    await expect(resourceDialog.getByText('public', { exact: true })).toBeVisible()
    await expect(resourceDialog.getByRole('button', { name: '确定', exact: true })).toBeEnabled()
    await expect(page.locator('.el-message')).toHaveCount(0)
    const resourceSurface = visibleDialogSurface(page)
    await expectDialogWithinViewport(page, resourceSurface)
    await expect(resourceSurface.locator('.el-dialog__body')).toHaveCSS('overflow', 'auto')
    await expect(resourceSurface).toHaveScreenshot('workflow-resource-picker-narrow.png', { animations: 'disabled' })
  })
})

test('cancel keeps the current engine and workflow', async ({ page }) => {
  const backend = await installMockBackend(page)
  await openSavedWorkflow(page)

  await requestEngineSwitch(page, ENGINE_B)
  const dialog = engineSwitchDialog(page)
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '保存并清空', exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '清空并切换', exact: true })).toBeVisible()

  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_A.name)
  await expect(primarySaveButton(page)).toBeEnabled()
  expect(backend.updates).toHaveLength(0)
  expect(backend.creates).toHaveLength(0)
})

test('keeps dialog and canvas keyboard focus predictable', async ({ page }) => {
  await installMockBackend(page)
  await openSavedWorkflow(page)

  const executeTrigger = primaryExecuteButton(page)
  await executeTrigger.click()
  const executeDialog = page.getByRole('dialog', { name: '执行工作流', exact: true })
  await expect(executeDialog.getByRole('radio', { name: '任务默认值', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(executeDialog).not.toBeVisible()
  await expect(executeTrigger).toBeFocused()

  const moreTrigger = page.getByRole('button', { name: '更多', exact: true })
  await moreTrigger.click()
  await page.getByRole('menuitem', { name: '查看 JSON', exact: true }).click()
  const jsonDialog = page.getByRole('dialog', { name: '工作流 JSON', exact: true })
  await expect(jsonDialog.getByRole('button', { name: '关闭', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(jsonDialog).not.toBeVisible()
  await expect(moreTrigger).toBeFocused()

  await requestEngineSwitch(page, ENGINE_B)
  const switchDialog = engineSwitchDialog(page)
  await expect(switchDialog.getByRole('button', { name: '取消', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(switchDialog).not.toBeVisible()
  await expect(page.locator('.engine-select input')).toBeFocused()

  const canvas = page.getByRole('region', { name: '工作流 DAG 画布', exact: true })
  await expect(canvas).toHaveAttribute('tabindex', '0')
  await expect(canvas).toHaveAttribute('aria-keyshortcuts', 'ArrowLeft ArrowRight ArrowUp ArrowDown Enter Delete Escape')
  await canvas.focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('status', { name: '工作流画布选择状态' })).toContainText('Operator A')
  await expect(page.getByRole('button', { name: '删除选中项', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: '打开节点参数', exact: true })).toBeVisible()
  await page.keyboard.press('Enter')
  await expect(page.locator('.right-panel')).toBeVisible()
})

test('highlights, selects, and deletes a workflow edge', async ({ page }) => {
  const backend = await installMockBackend(page, {
    includeInputConnections: true,
    includeConnectedEdge: true
  })
  await openSavedWorkflow(page)

  const canvas = page.locator('#workflow-dag-container canvas')
  const beforeHover = await canvas.screenshot()
  const canvasBox = await requiredBoundingBox(canvas)
  const deleteButton = page.getByRole('button', { name: '删除选中项', exact: true })
  const edgePoint = {
    x: canvasBox.x + 400,
    y: canvasBox.y + 195
  }

  await expect(deleteButton).toBeDisabled()
  await page.mouse.move(edgePoint.x, edgePoint.y)
  await expect.poll(async () => !(await canvas.screenshot()).equals(beforeHover)).toBe(true)

  await page.mouse.click(edgePoint.x, edgePoint.y)
  await expect(deleteButton).toBeEnabled()
  await page.keyboard.press('Delete')
  await expect(deleteButton).toBeDisabled()

  await primarySaveButton(page).click()
  await expect.poll(() => backend.updates.length).toBe(1)
  const target = backend.updates[0].payload.content.workflow_definition.tasks
    .find(task => task.id === 'operator_input_1')
  expect(target.params).toEqual({})
  expect(target.depends_on).toEqual([])
})

test('renders a visible gap where workflow edges cross', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('theme-mode', 'dark'))
  await installMockBackend(page, {
    includeInputConnections: true,
    includeCrossingEdges: true
  })
  await openSavedWorkflow(page)

  const edgeRendering = await page.locator('#workflow-dag-container').evaluate(element => {
    const graph = element.__vueParentComponent?.setupState?.graph
    const edges = graph?.getEdges?.() || []
    const beforePaths = edges.map(edge => JSON.stringify(edge.getKeyShape().attr('path')))

    graph.updateItem('operator_a_1', { x: 300, y: 120 })
    graph.refreshPositions()

    return edges.map((edge, index) => {
      const casing = edge.getContainer().find(shape => shape.get('name') === 'workflow-edge-casing')
      const keyPath = JSON.stringify(edge.getKeyShape().attr('path'))
      return {
        casingStyle: casing?.attr?.() || null,
        casingPath: JSON.stringify(casing?.attr?.('path')),
        keyPath,
        pathChanged: keyPath !== beforePaths[index]
      }
    })
  })
  expect(edgeRendering).toHaveLength(2)
  expect(edgeRendering.every(edge => (
    edge.casingStyle?.lineWidth === 6 &&
    edge.casingStyle?.stroke &&
    edge.casingPath === edge.keyPath
  ))).toBe(true)
  expect(edgeRendering.some(edge => edge.pathChanged)).toBe(true)
})

test('edits port bindings from the parameter panel and persists every input reference', async ({ page }) => {
  const backend = await installMockBackend(page, { includeInputConnections: true })
  await openSavedWorkflow(page)

  const canvas = page.locator('#workflow-dag-container canvas')
  const canvasBox = await requiredBoundingBox(canvas)
  await page.mouse.click(canvasBox.x + 520, canvasBox.y + 180)

  const panel = page.locator('.right-panel')
  await expect(panel.getByRole('heading', { name: '输入连接', exact: true })).toBeVisible()
  for (const inputName of ['left_value', 'right_value']) {
    const field = panel.locator('.input-connection-field').filter({ hasText: inputName })
    await expect(field).toHaveCount(1)
    await field.locator('.el-select__wrapper').click()
    const option = page.locator('.el-select-dropdown__item:visible').filter({ hasText: 'Operator A (operator_a_1)' })
    await expect(option).toHaveCount(1)
    await option.click({ force: true })
    await expect(page.locator('.el-select-dropdown:visible')).toHaveCount(0)
  }

  await primarySaveButton(page).click()
  await expect.poll(() => backend.updates.length).toBe(1)
  const target = backend.updates[0].payload.content.workflow_definition.tasks
    .find(task => task.id === 'operator_input_1')
  expect(target.params).toEqual({
    left_value: { $ref: 'operator_a_1' },
    right_value: { $ref: 'operator_a_1' }
  })
  expect(target.depends_on).toEqual(['operator_a_1'])
})

test('clear and switch detaches the saved task without updating it', async ({ page }) => {
  const backend = await installMockBackend(page)
  await openSavedWorkflow(page)

  await requestEngineSwitch(page, ENGINE_B)
  await engineSwitchDialog(page).getByRole('button', { name: '清空并切换', exact: true }).click()

  await expect(engineSwitchDialog(page)).not.toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_B.name)
  await expect(primarySaveButton(page)).toBeDisabled()
  await expect(page).toHaveURL(/\/workflow\?action=create$/)
  expect(backend.updates).toHaveLength(0)
  expect(backend.creates).toHaveLength(0)

  await addOperator(page, 'Category B', 'Operator B')
  await primarySaveButton(page).click()
  await expect(page.getByRole('dialog', { name: '保存工作流', exact: true })).toBeVisible()
  expect(backend.updates).toHaveLength(0)
})

test('save and clear preserves the old task and saves the new engine as a new task', async ({ page }) => {
  const backend = await installMockBackend(page)
  await openSavedWorkflow(page)

  await requestEngineSwitch(page, ENGINE_B)
  await engineSwitchDialog(page).getByRole('button', { name: '保存并清空', exact: true }).click()

  await expect(engineSwitchDialog(page)).not.toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_B.name)
  await expect(page.locator('.el-message').filter({
    hasText: `工作流已保存，并已切换到工作流引擎“${ENGINE_B.name}”`
  })).toBeVisible()
  expect(backend.updates).toHaveLength(1)
  expect(backend.updates[0].id).toBe(SAVED_TASK_ID)
  expect(backend.updates[0].payload.execution_config.engine_id).toBe(ENGINE_A.id)
  expect(backend.updates[0].payload.content.workflow_definition.tasks).toHaveLength(1)

  await addOperator(page, 'Category B', 'Operator B')
  await primarySaveButton(page).click()
  const saveDialog = page.getByRole('dialog', { name: '保存工作流', exact: true })
  await saveDialog.getByPlaceholder('请输入工作流名称', { exact: true }).fill('Engine B workflow')
  await saveDialog.getByRole('button', { name: '保存', exact: true }).click()

  await expect(saveDialog).not.toBeVisible()
  expect(backend.updates).toHaveLength(1)
  expect(backend.creates).toHaveLength(1)
  expect(backend.creates[0].execution_config.engine_id).toBe(ENGINE_B.id)
  expect(backend.creates[0].content.workflow_definition.tasks).toHaveLength(1)
})

test('shows validation details near the header while still allowing draft saves', async ({ page }) => {
  const validationError = {
    code: 'REQUIRED_PARAMETER',
    path: 'tasks[0].params.source',
    message: 'Source is required'
  }
  const backend = await installMockBackend(page, { validationErrors: [validationError] })
  await openSavedWorkflow(page)

  const validation = page.locator('.header-validation')
  await expect(validation).toHaveCount(0)
  expect(backend.validationRequests).toBe(0)
  await expect(primaryExecuteButton(page)).toBeEnabled()

  await addOperator(page, 'Category A', 'Operator A')
  await expect(validation).toHaveCount(0)
  expect(backend.validationRequests).toBe(0)

  const executeButton = primaryExecuteButton(page)
  await executeButton.click()
  await expect.poll(() => backend.validationRequests).toBe(1)
  await expect(validation).toContainText('工作流存在 1 个错误')
  await expect(validation).toContainText(validationError.message)
  await expect(page.locator('.validation-bar')).toHaveCount(0)
  await expect(executeButton).toBeDisabled()
  await expect(primarySaveButton(page)).toBeEnabled()
  await expect(page.getByRole('dialog', { name: '执行工作流', exact: true })).toHaveCount(0)

  await validation.getByRole('button').click()
  const issueGroup = page.locator('.validation-group').filter({ hasText: 'Operator A · operator_a_1' })
  await expect(issueGroup).toHaveCount(1)
  await expect(issueGroup.locator('.validation-item-param')).toHaveText('source')
  const issue = issueGroup.getByRole('button', { name: validationError.message, exact: true })
  await expect(issue).toBeVisible()
  await issue.click()

  const focusedField = page.locator('.param-field[data-param-name="source"]')
  await expect(focusedField).toBeVisible()
  await expect(focusedField).toHaveClass(/is-validation-target/)
  await expect(focusedField.locator('.el-form-item__error')).toHaveText(validationError.message)
  await expect(focusedField.locator('input')).toBeFocused()
  await focusedField.locator('input').fill('draft source')
  await expect(validation).toHaveCount(0)
  await expect(executeButton).toBeEnabled()

  await primarySaveButton(page).click()
  await expect.poll(() => backend.updates.length).toBe(1)
  await expect.poll(() => backend.validationRequests).toBe(2)
  await expect(validation).toContainText(validationError.message)
  await expect(executeButton).toBeDisabled()
  await expect(page.locator('.el-message').filter({
    hasText: '草稿已保存，存在 1 个待处理问题'
  })).toBeVisible()
  await expect(page.locator('.el-message').filter({ hasText: '保存成功' })).toHaveCount(0)
  expect(backend.updates[0].payload.content.workflow_definition.tasks).toHaveLength(2)
})

test('keeps save pending until post-save validation finishes', async ({ page }) => {
  const backend = await installMockBackend(page, { deferValidation: true })
  await openSavedWorkflow(page)

  const saveButton = primarySaveButton(page)
  await saveButton.click()
  await expect.poll(() => backend.updates.length).toBe(1)
  await expect.poll(() => backend.validationRequests).toBe(1)
  await expect(saveButton).toBeDisabled()
  await expect(saveButton).toHaveClass(/is-loading/)
  await expect(page.locator('.editor-content')).toHaveAttribute('inert', '')
  await expect(page.getByRole('status', { name: '工作流状态' })).toHaveText('正在校验工作流')

  backend.releaseValidation()

  await expect(saveButton).toBeEnabled()
  await expect(page.locator('.editor-content')).not.toHaveAttribute('inert', '')
  await expect(page.locator('.el-message').filter({ hasText: '保存成功' })).toBeVisible()
  expect(backend.updates).toHaveLength(1)
})

test('keeps execute pending through validation and automatic save', async ({ page }) => {
  const backend = await installMockBackend(page, {
    deferValidation: true,
    deferUpdate: true
  })
  await openSavedWorkflow(page)
  await addOperator(page, 'Category A', 'Operator A')

  const executeButton = primaryExecuteButton(page)
  await executeButton.click()
  await expect.poll(() => backend.validationRequests).toBe(1)
  await expect(executeButton).toBeDisabled()
  await expect(executeButton).toHaveClass(/is-loading/)
  await expect(page.locator('.editor-content')).toHaveAttribute('inert', '')
  await expect(page.getByRole('status', { name: '工作流状态' })).toHaveText('正在校验工作流')

  backend.releaseValidation()

  await expect.poll(() => backend.updates.length).toBe(1)
  await expect(executeButton).toBeDisabled()
  await expect(executeButton).toHaveClass(/is-loading/)
  await expect(page.locator('.editor-content')).toHaveAttribute('inert', '')
  await expect(page.getByRole('status', { name: '工作流状态' })).toHaveText('正在保存工作流')

  backend.releaseUpdate()

  await expect(page.getByRole('dialog', { name: '执行工作流', exact: true })).toBeVisible()
  await expect(executeButton).not.toHaveClass(/is-loading/)
  await expect(page.locator('.editor-content')).not.toHaveAttribute('inert', '')
  expect(backend.updates).toHaveLength(1)
})

test('locks the execute dialog while submission is pending', async ({ page }) => {
  const backend = await installMockBackend(page, { deferExecution: true })
  await page.addInitScript(() => { window.open = () => null })
  await openSavedWorkflow(page)

  await primaryExecuteButton(page).click()
  const dialog = page.getByRole('dialog', { name: '执行工作流', exact: true })
  await expect(dialog).toBeVisible()
  const input = await configureThresholdOverride(dialog, 10)
  const cancelButton = dialog.getByRole('button', { name: '取消', exact: true })
  const confirmButton = dialog.getByRole('button', { name: '执行', exact: true })

  await confirmButton.click()
  await expect.poll(() => backend.executions.length).toBe(1)
  await expect(input).toBeDisabled()
  await expect(cancelButton).toBeDisabled()
  await expect(confirmButton).toBeDisabled()
  await expect(confirmButton).toHaveClass(/is-loading/)
  await expect(dialog.locator('.el-dialog__headerbtn')).toHaveCount(0)
  await expect(page.getByRole('status', { name: '工作流状态' })).toHaveText('正在提交工作流执行')

  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  expect(backend.executions).toEqual([{ parameters: { threshold: 10 } }])

  backend.releaseExecution()

  await expect(dialog).not.toBeVisible()
  expect(backend.executions).toHaveLength(1)
})

test('keeps execution inputs available after submission fails', async ({ page }) => {
  const backend = await installMockBackend(page, { executionError: 'Runtime unavailable' })
  await openSavedWorkflow(page)

  await primaryExecuteButton(page).click()
  const dialog = page.getByRole('dialog', { name: '执行工作流', exact: true })
  const input = await configureThresholdOverride(dialog, 10)
  await dialog.getByRole('button', { name: '执行', exact: true }).click()

  await expect(page.locator('.el-message').filter({ hasText: 'Runtime unavailable' })).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(input).toBeEnabled()
  await expect(input).toHaveValue('10')
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: '执行', exact: true })).toBeEnabled()
  await expect(dialog.locator('.el-dialog__headerbtn')).toHaveCount(1)
  expect(backend.executions).toEqual([{ parameters: { threshold: 10 } }])
})

test('locks the save dialog while creation is pending', async ({ page }) => {
  const backend = await installMockBackend(page, { deferCreate: true })
  await openNewWorkflow(page)
  await addOperator(page, 'Category A', 'Operator A')

  await primarySaveButton(page).click()
  const dialog = page.getByRole('dialog', { name: '保存工作流', exact: true })
  const nameInput = dialog.getByRole('textbox', { name: /工作流名称$/ })
  await nameInput.fill('Pending workflow')
  const cancelButton = dialog.getByRole('button', { name: '取消', exact: true })
  const saveButton = dialog.getByRole('button', { name: '保存', exact: true })

  await saveButton.click()
  await expect.poll(() => backend.creates.length).toBe(1)
  await expect(nameInput).toBeDisabled()
  await expect(cancelButton).toBeDisabled()
  await expect(saveButton).toBeDisabled()
  await expect(saveButton).toHaveClass(/is-loading/)
  await expect(dialog.locator('.el-dialog__headerbtn')).toHaveCount(0)

  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  expect(backend.creates[0].name).toBe('Pending workflow')

  backend.releaseCreate()

  await expect(dialog).not.toBeVisible()
  expect(backend.creates).toHaveLength(1)
})

test('keeps save inputs available after creation fails', async ({ page }) => {
  const backend = await installMockBackend(page, { createError: 'Name already exists' })
  await openNewWorkflow(page)
  await addOperator(page, 'Category A', 'Operator A')

  await primarySaveButton(page).click()
  const dialog = page.getByRole('dialog', { name: '保存工作流', exact: true })
  const nameInput = dialog.getByRole('textbox', { name: /工作流名称$/ })
  await nameInput.fill('Duplicate workflow')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()

  await expect(page.locator('.el-message').filter({ hasText: 'Name already exists' })).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(nameInput).toBeEnabled()
  await expect(nameInput).toHaveValue('Duplicate workflow')
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await expect(dialog.locator('.el-dialog__headerbtn')).toHaveCount(1)
  expect(backend.creates).toHaveLength(1)
})

test('saves a new workflow before opening and submitting execution', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.addInitScript(() => { window.open = () => null })
  await openNewWorkflow(page)
  await addOperator(page, 'Category A', 'Operator A')

  await primaryExecuteButton(page).click()
  await expect.poll(() => backend.validationRequests).toBe(1)
  const saveDialog = page.getByRole('dialog', { name: '保存工作流', exact: true })
  await expect(saveDialog).toBeVisible()
  await saveDialog.getByRole('textbox', { name: /工作流名称$/ }).fill('Executable workflow')
  await saveDialog.getByRole('button', { name: '保存', exact: true }).click()

  const executeDialog = page.getByRole('dialog', { name: '执行工作流', exact: true })
  await expect(executeDialog).toBeVisible()
  await expect(page).toHaveURL(/\/workflow\?action=edit&id=99$/)
  expect(backend.creates).toHaveLength(1)
  expect(backend.creates[0].name).toBe('Executable workflow')
  await executeDialog.getByRole('button', { name: '执行', exact: true }).click()

  await expect(executeDialog).not.toBeVisible()
  expect(backend.executionTaskIds).toEqual([99])
  expect(backend.executions).toEqual([{ parameters: {} }])
  expect(backend.validationRequests).toBe(1)

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Executable workflow', exact: true })).toBeVisible()
  await expect(primarySaveButton(page)).toBeEnabled()
})

test('locks the editor while AI generation replaces the workflow', async ({ page }) => {
  const backend = await installMockBackend(page, { deferGeneration: true })
  await openSavedWorkflow(page)

  const aiButton = page.getByRole('button', { name: 'AI 工作流助手', exact: true })
  await aiButton.click()
  const aiPanel = page.locator('.ai-inline-panel')
  const prompt = aiPanel.getByPlaceholder(/描述你的 GIS 工作流需求/)
  await prompt.fill('Generate a replacement workflow')
  const generateButton = aiPanel.getByRole('button', { name: '生成工作流', exact: true })
  await generateButton.click()

  await expect.poll(() => backend.generationRequests.length).toBe(1)
  await expect(page.locator('.editor-content')).toHaveAttribute('inert', '')
  await expect(page.locator('.engine-select .el-select__wrapper').first()).toHaveClass(/is-disabled/)
  await expect(prompt).toBeDisabled()
  await expect(generateButton).toBeDisabled()
  await expect(generateButton).toHaveClass(/is-loading/)
  await expect(aiPanel.getByRole('button', { name: '关闭', exact: true })).toBeDisabled()
  await expect(aiButton).toBeDisabled()

  backend.releaseGeneration()

  await expect(aiPanel).not.toBeVisible()
  await expect(page.locator('.editor-content')).not.toHaveAttribute('inert', '')
  await expect(page.locator('.el-message').filter({ hasText: '工作流生成成功，包含 1 个步骤' })).toBeVisible()
  await expect(primarySaveButton(page)).toBeEnabled()
  expect(backend.generationRequests).toHaveLength(1)
})

async function openSavedWorkflow(page) {
  await page.goto(`/workflow?action=edit&id=${SAVED_TASK_ID}`)
  await expect(page.getByRole('heading', { name: 'Saved workflow', exact: true })).toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_A.name)
  await expect(primarySaveButton(page)).toBeEnabled()
}

async function openNewWorkflow(page) {
  await page.goto('/workflow?action=create')
  await expect(page.getByRole('heading', { name: '未命名工作流', exact: true })).toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_A.name)
}

async function requestEngineSwitch(page, engine) {
  const select = page.locator('.engine-select .el-select__wrapper')
  await expect(select).toHaveCount(1)
  await select.click()
  await page.getByRole('option', { name: `${engine.name} ${engine.engine_type} 在线`, exact: true }).click()
  await expect(engineSwitchDialog(page)).toBeVisible()
}

async function addOperator(page, category, operator) {
  const categoryHeader = page.locator('.el-collapse-item__header').filter({ hasText: category })
  await expect(categoryHeader).toHaveCount(1)
  if (await categoryHeader.getAttribute('aria-expanded') !== 'true') await categoryHeader.click()
  const operatorItem = page.locator('.operator-item').filter({ hasText: operator })
  await expect(operatorItem).toHaveCount(1)
  await operatorItem.click()
  await expect(primarySaveButton(page)).toBeEnabled()
}

function selectedEngine(page) {
  return page.locator('.engine-select .el-select__placeholder')
}

function primarySaveButton(page) {
  return page.locator('.primary-actions').getByRole('button', { name: '保存', exact: true })
}

function primaryExecuteButton(page) {
  return page.locator('.primary-actions').getByRole('button', { name: '执行', exact: true })
}

async function configureThresholdOverride(dialog, value) {
  const field = dialog.locator('.parameter-field').filter({ hasText: '阈值' })
  await field.getByText('执行时指定', { exact: true }).click()
  const input = field.getByRole('spinbutton')
  await input.fill(String(value))
  return input
}

function engineSwitchDialog(page) {
  return page.getByRole('dialog', { name: '切换工作流引擎', exact: true })
}

function visibleDialogSurface(page) {
  return page.locator('.el-dialog.addp-dialog:visible')
}

async function installMockBackend(page, {
  validationErrors = [],
  deferValidation = false,
  deferUpdate = false,
  deferExecution = false,
  executionError = '',
  deferCreate = false,
  createError = '',
  deferGeneration = false,
  includeResourcePicker = false,
  includeInputConnections = false,
  includeConnectedEdge = false,
  includeCrossingEdges = false
} = {}) {
  const updates = []
  const creates = []
  const executions = []
  const executionTaskIds = []
  const generationRequests = []
  let validationRequests = 0
  let releaseValidation = () => {}
  const validationGate = deferValidation
    ? new Promise(resolve => { releaseValidation = resolve })
    : null
  let releaseUpdate = () => {}
  const updateGate = deferUpdate
    ? new Promise(resolve => { releaseUpdate = resolve })
    : null
  let releaseExecution = () => {}
  const executionGate = deferExecution
    ? new Promise(resolve => { releaseExecution = resolve })
    : null
  let releaseCreate = () => {}
  const createGate = deferCreate
    ? new Promise(resolve => { releaseCreate = resolve })
    : null
  let releaseGeneration = () => {}
  const generationGate = deferGeneration
    ? new Promise(resolve => { releaseGeneration = resolve })
    : null
  const task = createSavedTask(
    includeResourcePicker,
    includeInputConnections,
    includeConnectedEdge,
    includeCrossingEdges
  )
  const operatorAParameters = [
    { name: 'source', type: 'string', required: false, description: 'Source input' }
  ]
  if (includeResourcePicker) {
    operatorAParameters.push({
      name: '数据源',
      type: 'string',
      required: false,
      description: '选择测试数据源',
      ui_type: 'resource_tree_picker',
      ui_config: {
        api_base_url: '/api/v1/meta',
        engine_families: ['tabular'],
        resource_binding: {
          mode: 'existing',
          locator_param: 'source_locator'
        }
      }
    })
  }
  const operatorsByEngine = {
    [ENGINE_A.id]: [
      createOperator(
        'operator_a',
        'Operator A',
        'Category A',
        ENGINE_A.engine_type,
        operatorAParameters
      ),
      ...(includeInputConnections ? [createOperator(
        'operator_input',
        'Operator Input',
        'Category A',
        ENGINE_A.engine_type,
        [
          { name: 'left_value', type: 'number', param_type: 'input', required: true },
          { name: 'right_value', type: 'number', param_type: 'input', required: true }
        ]
      )] : [])
    ],
    [ENGINE_B.id]: [createOperator('operator_b', 'Operator B', 'Category B', ENGINE_B.engine_type)]
  }

  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'develop-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'develop-e2e' })
    }
    if (path === '/api/v1/develop/workflow-engines') {
      return fulfillJSON(route, [ENGINE_A, ENGINE_B])
    }
    if (path === '/api/v1/meta/engines') {
      return fulfillJSON(route, [RESOURCE_ENGINE])
    }
    if (path === `/api/v1/meta/resource-tree/${RESOURCE_ENGINE.id}`) {
      return fulfillJSON(route, {
        id: `addp://engine/${RESOURCE_ENGINE.id}/path?type=database`,
        locator: `addp://engine/${RESOURCE_ENGINE.id}/path?type=database`,
        label: RESOURCE_ENGINE.name,
        type: 'database',
        children: [{
          id: `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`,
          locator: `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`,
          label: 'public',
          type: 'schema',
          children: [{
            id: RESOURCE_LOCATOR,
            locator: RESOURCE_LOCATOR,
            label: 'roads',
            type: 'table',
            children: []
          }]
        }]
      })
    }
    if (path === `/api/v1/meta/resource-tree/${RESOURCE_ENGINE.id}/node`) {
      const locator = url.searchParams.get('locator')
      if (locator === `addp://engine/${RESOURCE_ENGINE.id}/path?type=database`) {
        return fulfillJSON(route, {
          id: locator,
          locator,
          label: RESOURCE_ENGINE.name,
          type: 'database',
          children: [{
            id: `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`,
            locator: `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`,
            label: 'public',
            type: 'schema',
            hasChildren: true,
            children: []
          }]
        })
      }
      if (locator === `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`) {
        return fulfillJSON(route, {
          id: locator,
          locator,
          label: 'public',
          type: 'schema',
          children: [{
            id: RESOURCE_LOCATOR,
            locator: RESOURCE_LOCATOR,
            label: 'roads',
            type: 'table',
            hasChildren: false,
            children: []
          }]
        })
      }
      return fulfillJSON(route, {})
    }
    if (path === `/api/v1/meta/resource-tree/${RESOURCE_ENGINE.id}/ancestors`) {
      return fulfillJSON(route, {
        engine_id: RESOURCE_ENGINE.id,
        target_locator: RESOURCE_LOCATOR,
        target_kind: 'item',
        ancestors: [
          {
            id: `addp://engine/${RESOURCE_ENGINE.id}/path?type=database`,
            locator: `addp://engine/${RESOURCE_ENGINE.id}/path?type=database`,
            label: RESOURCE_ENGINE.name,
            type: 'database'
          },
          {
            id: `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`,
            locator: `addp://engine/${RESOURCE_ENGINE.id}/path/public?type=schema`,
            label: 'public',
            type: 'schema'
          },
          {
            id: RESOURCE_LOCATOR,
            locator: RESOURCE_LOCATOR,
            label: 'roads',
            type: 'table'
          }
        ]
      })
    }
    const operatorMatch = path.match(/^\/api\/v1\/develop\/workflow-engines\/(\d+)\/operators$/)
    if (operatorMatch) {
      return fulfillJSON(route, { operators: operatorsByEngine[Number(operatorMatch[1])] || [] })
    }
    if (path === '/api/v1/develop/workflow-validations' && method === 'POST') {
      validationRequests += 1
      if (validationGate) await validationGate
      return fulfillJSON(route, {
        valid: validationErrors.length === 0,
        errors: validationErrors,
        warnings: []
      })
    }
    if (path === '/api/v1/copilot/workflow/generate' && method === 'POST') {
      generationRequests.push(request.postDataJSON())
      if (generationGate) await generationGate
      return fulfillJSON(route, {
        status: 'success',
        workflow: {
          tasks: [{
            id: 'generated_operator_1',
            operator: 'operator_a',
            params: {},
            depends_on: []
          }]
        }
      })
    }
    if (path === `/api/v1/develop/task-definitions/${SAVED_TASK_ID}` && method === 'GET') {
      return fulfillJSON(route, task)
    }
    if (path === '/api/v1/develop/task-definitions/99' && method === 'GET') {
      return fulfillJSON(route, {
        ...creates[creates.length - 1],
        id: 99,
        execution_contract: createExecutionContract()
      })
    }
    if (path === `/api/v1/develop/task-definitions/${SAVED_TASK_ID}` && method === 'PUT') {
      const payload = request.postDataJSON()
      updates.push({ id: SAVED_TASK_ID, payload })
      if (updateGate) await updateGate
      return fulfillJSON(route, { ...task, ...payload, id: SAVED_TASK_ID })
    }
    const executionMatch = path.match(/^\/api\/v1\/develop\/task-definitions\/(\d+)\/execute$/)
    if (executionMatch && method === 'POST') {
      executionTaskIds.push(Number(executionMatch[1]))
      executions.push(request.postDataJSON())
      if (executionGate) await executionGate
      if (executionError) {
        return fulfillJSON(route, { error: executionError }, 500)
      }
      return fulfillJSON(route, { execution_id: 777 })
    }
    if (path === '/api/v1/develop/task-definitions' && method === 'POST') {
      const payload = request.postDataJSON()
      creates.push(payload)
      if (createGate) await createGate
      if (createError) {
        return fulfillJSON(route, { error: createError }, 500)
      }
      return fulfillJSON(route, { ...payload, id: 99 })
    }

    return fulfillJSON(route, {})
  })

  return {
    updates,
    creates,
    executions,
    executionTaskIds,
    generationRequests,
    releaseValidation,
    releaseUpdate,
    releaseExecution,
    releaseCreate,
    releaseGeneration,
    get validationRequests() {
      return validationRequests
    }
  }
}

function createSavedTask(
  includeResourcePicker = false,
  includeInputConnections = false,
  includeConnectedEdge = false,
  includeCrossingEdges = false
) {
  const tasks = [{
    id: 'operator_a_1',
    operator: 'operator_a',
    params: includeResourcePicker
      ? { source: 'manual-source', source_locator: RESOURCE_LOCATOR }
      : {},
    depends_on: []
  }]
  if (includeCrossingEdges) {
    tasks.push({
      id: 'operator_a_2',
      operator: 'operator_a',
      params: {},
      depends_on: []
    })
  }
  if (includeInputConnections) {
    tasks.push({
      id: 'operator_input_1',
      operator: 'operator_input',
      params: includeCrossingEdges
        ? {
            left_value: { $ref: 'operator_a_2' },
            right_value: { $ref: 'operator_a_1' }
          }
        : includeConnectedEdge
        ? { left_value: { $ref: 'operator_a_1' } }
        : {},
      depends_on: includeCrossingEdges
        ? ['operator_a_1', 'operator_a_2']
        : includeConnectedEdge ? ['operator_a_1'] : []
    })
  }
  return {
    id: SAVED_TASK_ID,
    name: 'Saved workflow',
    display_name: 'Saved workflow',
    description: '',
    dev_type: 'workflow',
    content: {
      workflow_definition: {
        tasks
      },
      inputs: {}
    },
    execution_config: {
      type: 'workflow',
      engine_id: ENGINE_A.id
    },
    execution_contract: createExecutionContract(),
    editor_layout: {
      nodes: {
        operator_a_1: includeCrossingEdges ? { x: 250, y: 80 } : { x: 260, y: 180 },
        ...(includeCrossingEdges ? { operator_a_2: { x: 330, y: 520 } } : {}),
        ...(includeInputConnections
          ? {
              operator_input_1: includeCrossingEdges
                ? { x: 720, y: 300 }
                : { x: includeConnectedEdge ? 650 : 520, y: 180 }
            }
          : {})
      },
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    }
  }
}

function createExecutionContract() {
  return {
    input_schema: {
      type: 'object',
      title: '执行参数',
      properties: {
        threshold: {
          type: 'integer',
          title: '阈值',
          description: '本次执行使用的阈值'
        }
      },
      additionalProperties: false
    },
    input_defaults: { threshold: 5 },
    input_ui_schema: { threshold: { order: 0 } },
    output_schema: { type: 'object', properties: {}, additionalProperties: false }
  }
}

function createOperator(name, displayName, category, engineType, publicParameters = []) {
  return {
    id: name,
    name,
    display_name: displayName,
    engine_type: engineType,
    category,
    category_path: [category],
    description: `${displayName} description`,
    execution_modes: ['workflow'],
    parameters: [],
    public_parameters: publicParameters,
    output_ports: [{ name: 'default', type: 'number', is_default: true }]
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}

async function requiredBoundingBox(locator) {
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  return box
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
