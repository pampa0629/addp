import { expect, test } from '@playwright/test'

const SAVED_TASK_ID = 42
const ENGINE_A = { id: 1, name: 'Engine A', engine_type: 'engine_a' }
const ENGINE_B = { id: 2, name: 'Engine B', engine_type: 'engine_b' }

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

test('clear and switch detaches the saved task without updating it', async ({ page }) => {
  const backend = await installMockBackend(page)
  await openSavedWorkflow(page)

  await requestEngineSwitch(page, ENGINE_B)
  await engineSwitchDialog(page).getByRole('button', { name: '清空并切换', exact: true }).click()

  await expect(engineSwitchDialog(page)).not.toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_B.name)
  await expect(primarySaveButton(page)).toBeDisabled()
  await expect(page).toHaveURL(/\/workflow$/)
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

  backend.releaseValidation()

  await expect.poll(() => backend.updates.length).toBe(1)
  await expect(executeButton).toBeDisabled()
  await expect(executeButton).toHaveClass(/is-loading/)
  await expect(page.locator('.editor-content')).toHaveAttribute('inert', '')

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
  const inputs = dialog.getByRole('textbox', { name: '执行参数', exact: true })
  await inputs.fill('{"threshold": 10}')
  const cancelButton = dialog.getByRole('button', { name: '取消', exact: true })
  const confirmButton = dialog.getByRole('button', { name: '执行', exact: true })

  await confirmButton.click()
  await expect.poll(() => backend.executions.length).toBe(1)
  await expect(inputs).toBeDisabled()
  await expect(cancelButton).toBeDisabled()
  await expect(confirmButton).toBeDisabled()
  await expect(confirmButton).toHaveClass(/is-loading/)
  await expect(dialog.locator('.el-dialog__headerbtn')).toHaveCount(0)

  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  expect(backend.executions).toEqual([{ threshold: 10 }])

  backend.releaseExecution()

  await expect(dialog).not.toBeVisible()
  expect(backend.executions).toHaveLength(1)
})

test('keeps execution inputs available after submission fails', async ({ page }) => {
  const backend = await installMockBackend(page, { executionError: 'Runtime unavailable' })
  await openSavedWorkflow(page)

  await primaryExecuteButton(page).click()
  const dialog = page.getByRole('dialog', { name: '执行工作流', exact: true })
  const inputs = dialog.getByRole('textbox', { name: '执行参数', exact: true })
  await inputs.fill('{"threshold": 10}')
  await dialog.getByRole('button', { name: '执行', exact: true }).click()

  await expect(page.locator('.el-message').filter({ hasText: 'Runtime unavailable' })).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(inputs).toBeEnabled()
  await expect(inputs).toHaveValue('{"threshold": 10}')
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: '执行', exact: true })).toBeEnabled()
  await expect(dialog.locator('.el-dialog__headerbtn')).toHaveCount(1)
  expect(backend.executions).toEqual([{ threshold: 10 }])
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
  await expect(page).toHaveURL(/\/workflow\?id=99$/)
  expect(backend.creates).toHaveLength(1)
  expect(backend.creates[0].name).toBe('Executable workflow')
  await executeDialog.getByRole('button', { name: '执行', exact: true }).click()

  await expect(executeDialog).not.toBeVisible()
  expect(backend.executionTaskIds).toEqual([99])
  expect(backend.executions).toEqual([{}])
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
  await page.goto(`/workflow?id=${SAVED_TASK_ID}`)
  await expect(page.getByRole('heading', { name: 'Saved workflow', exact: true })).toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_A.name)
  await expect(primarySaveButton(page)).toBeEnabled()
}

async function openNewWorkflow(page) {
  await page.goto('/workflow')
  await expect(page.getByRole('heading', { name: '未命名工作流', exact: true })).toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_A.name)
}

async function requestEngineSwitch(page, engine) {
  const select = page.locator('.engine-select .el-select__wrapper')
  await expect(select).toHaveCount(1)
  await select.click()
  await page.getByRole('option', { name: `${engine.name} ${engine.engine_type}`, exact: true }).click()
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

function engineSwitchDialog(page) {
  return page.getByRole('dialog', { name: '切换工作流引擎', exact: true })
}

async function installMockBackend(page, {
  validationErrors = [],
  deferValidation = false,
  deferUpdate = false,
  deferExecution = false,
  executionError = '',
  deferCreate = false,
  createError = '',
  deferGeneration = false
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
  const task = createSavedTask()
  const operatorsByEngine = {
    [ENGINE_A.id]: [createOperator(
      'operator_a',
      'Operator A',
      'Category A',
      ENGINE_A.engine_type,
      [{ name: 'source', type: 'string', required: false, description: 'Source input' }]
    )],
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
      return fulfillJSON(route, { ...creates[creates.length - 1], id: 99 })
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

function createSavedTask() {
  return {
    id: SAVED_TASK_ID,
    name: 'Saved workflow',
    display_name: 'Saved workflow',
    description: '',
    dev_type: 'workflow',
    content: {
      workflow_definition: {
        tasks: [{
          id: 'operator_a_1',
          operator: 'operator_a',
          params: {},
          depends_on: []
        }]
      },
      inputs: {}
    },
    execution_config: {
      type: 'workflow',
      engine_id: ENGINE_A.id
    },
    editor_layout: {
      nodes: { operator_a_1: { x: 260, y: 180 } },
      viewport: { zoom: 1, translate_x: 0, translate_y: 0 }
    }
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
