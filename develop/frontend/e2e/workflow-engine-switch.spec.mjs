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
  await expect(validation).toContainText('工作流存在 1 个错误')
  await expect(validation).toContainText(validationError.message)
  await expect(page.locator('.validation-bar')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '执行', exact: true })).toBeDisabled()
  await expect(primarySaveButton(page)).toBeEnabled()

  await validation.getByRole('button').click()
  const issue = page.locator('.validation-popover').getByRole('button', { name: validationError.message, exact: true })
  await expect(issue).toBeVisible()

  await primarySaveButton(page).click()
  await expect.poll(() => backend.updates.length).toBe(1)
  expect(backend.updates[0].payload.content.workflow_definition.tasks).toHaveLength(1)
})

async function openSavedWorkflow(page) {
  await page.goto(`/workflow?id=${SAVED_TASK_ID}`)
  await expect(page.getByRole('heading', { name: 'Saved workflow', exact: true })).toBeVisible()
  await expect(selectedEngine(page)).toContainText(ENGINE_A.name)
  await expect(primarySaveButton(page)).toBeEnabled()
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

function engineSwitchDialog(page) {
  return page.getByRole('dialog', { name: '切换工作流引擎', exact: true })
}

async function installMockBackend(page, { validationErrors = [] } = {}) {
  const updates = []
  const creates = []
  let validationRequests = 0
  const task = createSavedTask()
  const operatorsByEngine = {
    [ENGINE_A.id]: [createOperator('operator_a', 'Operator A', 'Category A', ENGINE_A.engine_type)],
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
      return fulfillJSON(route, {
        valid: validationErrors.length === 0,
        errors: validationErrors,
        warnings: []
      })
    }
    if (path === `/api/v1/develop/task-definitions/${SAVED_TASK_ID}` && method === 'GET') {
      return fulfillJSON(route, task)
    }
    if (path === `/api/v1/develop/task-definitions/${SAVED_TASK_ID}` && method === 'PUT') {
      const payload = request.postDataJSON()
      updates.push({ id: SAVED_TASK_ID, payload })
      return fulfillJSON(route, { ...task, ...payload, id: SAVED_TASK_ID })
    }
    if (path === '/api/v1/develop/task-definitions' && method === 'POST') {
      const payload = request.postDataJSON()
      creates.push(payload)
      return fulfillJSON(route, { ...payload, id: 99 })
    }

    return fulfillJSON(route, {})
  })

  return {
    updates,
    creates,
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

function createOperator(name, displayName, category, engineType) {
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
    public_parameters: [],
    output_ports: [{ name: 'default', type: 'number', is_default: true }]
  }
}

async function fulfillJSON(route, body) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
