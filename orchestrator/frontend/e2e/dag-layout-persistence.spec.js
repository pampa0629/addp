import { expect, test } from '@playwright/test'

const ORCHESTRATION_ID = 'dag-layout-e2e'
const NODE_ID = 'fixture-step'
const INTERACTION_ORCHESTRATION_ID = 'dag-interactions-e2e'
const SOURCE_NODE_ID = 'source-step'
const TARGET_NODE_ID = 'target-step'
const TASK_LIBRARY_ORCHESTRATION_ID = 'task-library-e2e'

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

async function installMockBackend(page, initialOrchestration, taskLibrary = {}) {
  let persistedPayload = null
  let orchestration = initialOrchestration
  const taskProviders = taskLibrary.taskProviders || []
  const tasksByType = taskLibrary.tasksByType || {}
  const pendingTaskRequests = []

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
      orchestration = { ...orchestration, ...persistedPayload }
      return fulfillJSON(route, orchestration)
    }
    if (path === '/api/v1/orchestrator/orchestrations' && request.method() === 'GET') {
      return fulfillJSON(route, [orchestration])
    }

    return fulfillJSON(route, {})
  })

  return {
    getPersistedPayload: () => persistedPayload,
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
