import { expect, test } from '@playwright/test'

const POSTGRES_ENGINE = {
  id: 11,
  name: 'Business PostgreSQL',
  engine_type: 'postgresql',
  lifecycle_state: 'active',
  connection_status: 'online'
}

const NFS_ENGINE = {
  id: 12,
  name: 'Business NFS',
  engine_type: 'nfs',
  lifecycle_state: 'active',
  connection_status: 'online'
}

const FARMLAND_LOCATOR = 'addp://engine/11/path/public/farmland?type=table&item_id=1101'
const RIVERS_LOCATOR = 'addp://engine/11/path/public/rivers?type=table&item_id=1102'
const DOC_LOCATOR = 'addp://engine/12/path/doc?type=directory&node_id=220'
const README_LOCATOR = 'addp://engine/12/path/doc/README.md?type=file&item_id=1201'
const SLIDES_LOCATOR = 'addp://engine/12/path/doc/slides.pptx?type=file&item_id=1202'
const TILE_SET_LOCATOR = 'addp://engine/12/path/tiles/farmland.pmtiles?type=file&item_id=1301'

test('selects a spatial table through the shared picker and applies capability facts', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/derived-tasks?category=spatial_business&task_type=vector_tile_set_generation&create=1')

  const dialog = page.getByRole('dialog', { name: '新建任务' })
  await expect(dialog).toBeVisible()
  await chooseEngine(page, dialog, POSTGRES_ENGINE.name)
  await expandTreeNode(dialog, 'public')
  await treeNodeContent(dialog, 'farmland').click()

  await expect(dialog.locator('.selection-summary')).toContainText('public.farmland')
  await expect(dialog.locator('.selection-summary')).toContainText('farmland')
  await expect(dialog.getByText('geometry', { exact: true }).first()).toBeVisible()
  await expect.poll(() => backend.capabilityLocators).toEqual([FARMLAND_LOCATOR])
})

test('restores the source table from the unified spatial task create URL', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto(`/derived-tasks?category=spatial_business&task_type=vector_tile_set_generation&create=1&locator=${encodeURIComponent(FARMLAND_LOCATOR)}`)

  const dialog = page.getByRole('dialog', { name: '新建任务' })
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('.selection-summary')).toContainText('public.farmland')
  await expect.poll(() => backend.capabilityLocators).toContain(FARMLAND_LOCATOR)
})

test('creates a managed quick-view task from one selected source without a target', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/derived-tasks?category=managed_quick_view&task_type=vector_tile_cache_generation&create=1')

  const dialog = page.getByRole('dialog', { name: '新建快显任务' })
  await expect(dialog).toBeVisible()
  await chooseEngine(page, dialog, POSTGRES_ENGINE.name)
  await expandTreeNode(dialog, 'public')
  await treeNodeContent(dialog, 'farmland').click()

  await expect(dialog.getByText('矢量瓦片缓存', { exact: true })).toBeVisible()
  await expect(dialog.getByText('目标', { exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: '生成并执行' }).click()

  await expect.poll(() => backend.quickViewActions).toEqual([{
    locator: FARMLAND_LOCATOR,
    action: 'generate_vector_tile_cache'
  }])
})

test('opens the source preview when the selected source already has a current quick-view result', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto(`/derived-tasks?category=managed_quick_view&task_type=vector_tile_cache_generation&create=1&locator=${encodeURIComponent(RIVERS_LOCATOR)}`)

  const dialog = page.getByRole('dialog', { name: '新建快显任务' })
  await expect(dialog.getByText('当前源数据已存在可直接使用的快显结果，无需重复生成。')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '生成并执行' })).toHaveCount(0)
  await dialog.getByRole('button', { name: '查看已有结果' }).click()

  await expect.poll(() => backend.preferredModeRequests).toEqual([{
    locator: RIVERS_LOCATOR,
    preferred_mode: 'map_quick_view'
  }])

  await expect.poll(() => {
    const url = new URL(page.url())
    return { pathname: url.pathname, locator: url.searchParams.get('locator') }
  }).toEqual({ pathname: '/data-explorer', locator: RIVERS_LOCATOR })
})

test('opens a managed quick-view task source from the task list without exposing infra result navigation', async ({ page }) => {
  const backend = await installMockBackend(page, { includeResultTask: true })
  await page.goto('/derived-tasks?category=managed_quick_view')

  const row = page.getByRole('row', { name: /public\.rivers 瓦片缓存/ })
  await expect(row.getByRole('button', { name: '结果', exact: true })).toHaveCount(0)
  await expect(row.getByText('可用', { exact: true })).toBeVisible()
  await row.getByRole('button', { name: '源数据', exact: true }).click()

  await expect.poll(() => {
    const url = new URL(page.url())
    return { pathname: url.pathname, locator: url.searchParams.get('locator') }
  }).toEqual({ pathname: '/data-explorer', locator: RIVERS_LOCATOR })
  expect(backend.taskDetailRequests).toEqual([])
  expect(backend.preferredModeRequests).toEqual([])
})

test('opens a spatial task target only after resolving the Meta data item', async ({ page }) => {
  const backend = await installMockBackend(page, { includeSpatialTask: true })
  await page.goto('/derived-tasks?category=spatial_business')

  const row = page.getByRole('row', { name: /耕地矢量瓦片集/ })
  await row.getByRole('button', { name: '目标数据', exact: true }).click()

  await expect.poll(() => backend.targetItemLookups).toEqual([{
    engineID: '12',
    catalogPath: 'tiles/farmland.pmtiles'
  }])
  await expect.poll(() => {
    const url = new URL(page.url())
    return { pathname: url.pathname, locator: url.searchParams.get('locator') }
  }).toEqual({ pathname: '/data-explorer', locator: TILE_SET_LOCATOR })
})

test('filters failed generation tasks and deletes only the selected tasks', async ({ page }) => {
  const backend = await installMockBackend(page, { includeFailedTasks: true })
  await page.goto('/derived-tasks?category=managed_quick_view')

  await page.locator('.toolbar .el-select').nth(1).click()
  await page.getByRole('option', { name: '失败', exact: true }).click()

  await expect.poll(() => backend.taskListQueries.some(query => (
    query.category === 'managed_quick_view' && query.executionStatus === 'failed'
  ))).toBe(true)
  await expect.poll(() => new URL(page.url()).searchParams.get('execution_status')).toBe('failed')

  const farmlandRow = page.getByRole('row', { name: /失败任务 - farmland/ })
  const buildingRow = page.getByRole('row', { name: /失败任务 - building/ })
  await farmlandRow.locator('.el-checkbox').click()
  await buildingRow.locator('.el-checkbox').click()
  await page.getByRole('button', { name: '批量删除（2）', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: '批量删除任务' })
  await expect(dialog).toContainText('确认删除已选择的 2 个任务定义？')
  await dialog.getByRole('button', { name: '确定', exact: true }).click()

  await expect.poll(() => [...backend.deletedTasks].sort()).toEqual([
    'model_3d_glb_generation/72',
    'vector_tile_cache_generation/71'
  ])
  await expect(page.getByText('已删除 2 个任务')).toBeVisible()
  await expect(page.getByText('暂无数据任务')).toBeVisible()
  expect(backend.tasks.map(task => task.id)).toEqual([73])
})

test('filters generation tasks with missing resource bindings and explains the issue', async ({ page }) => {
  const backend = await installMockBackend(page, { includeFailedTasks: true })
  await page.goto('/derived-tasks?category=managed_quick_view&binding_status=missing')

  await expect.poll(() => backend.taskListQueries.some(query => (
    query.category === 'managed_quick_view' && query.bindingStatus === 'missing'
  ))).toBe(true)
  await expect(page.getByText('失败任务 - farmland')).toBeVisible()
  await expect(page.getByText('失败任务 - building')).toBeVisible()
  await expect(page.getByText('成功任务 - rivers')).toHaveCount(0)
  await expect(page.getByText('源数据缺失', { exact: true })).toBeVisible()
  await expect(page.getByText('引擎已删除', { exact: true })).toBeVisible()
})

test('rebinds a missing managed quick-view task without starting an execution', async ({ page }) => {
  const backend = await installMockBackend(page, { includeFailedTasks: true })
  await page.goto('/derived-tasks?category=managed_quick_view&binding_status=missing')

  const row = page.getByRole('row', { name: /失败任务 - farmland/ })
  await row.getByRole('button', { name: '重新绑定', exact: true }).click()

  await expect.poll(() => {
    const url = new URL(page.url())
    return {
      taskType: url.searchParams.get('task_type'),
      rebindTaskID: url.searchParams.get('rebind_task_id')
    }
  }).toEqual({ taskType: 'vector_tile_cache_generation', rebindTaskID: '71' })
  await page.reload()

  const dialog = page.getByRole('dialog', { name: '重新绑定源资源' })
  await expect(dialog).toContainText('重新绑定不会自动执行任务')
  await chooseEngine(page, dialog, POSTGRES_ENGINE.name)
  await expandTreeNode(dialog, 'public')
  await treeNodeContent(dialog, 'rivers').click()
  await dialog.getByRole('button', { name: '确认重新绑定', exact: true }).click()

  await expect.poll(() => backend.rebindRequests).toEqual([{
    taskType: 'vector_tile_cache_generation',
    taskID: 71,
    body: { version: 1, locator: RIVERS_LOCATOR }
  }])
  expect(backend.quickViewActions).toEqual([])
  await expect(page.getByText('源资源已重新绑定，可按需执行任务')).toBeVisible()
  await expect(page.getByText('失败任务 - farmland')).toHaveCount(0)
})

test('creates a PPTX PDF quick-view task through its owner action', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/derived-tasks?category=managed_quick_view&task_type=pptx_pdf_generation&create=1')

  const dialog = page.getByRole('dialog', { name: '新建快显任务' })
  await chooseEngine(page, dialog, NFS_ENGINE.name)
  await expandTreeNode(dialog, 'doc')
  await treeNodeContent(dialog, 'slides.pptx').click()

  await expect(dialog.getByText('演示文稿 PDF', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '生成并执行' }).click()
  await expect.poll(() => backend.quickViewActions).toContainEqual({
    locator: SLIDES_LOCATOR,
    action: 'generate_pptx_pdf'
  })
})

test('keeps directory and file vectorization semantics in the shared picker', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/derived-tasks?category=embedding&create=1')

  const dialog = page.getByRole('dialog', { name: '新建向量化任务' })
  await expect(dialog).toBeVisible()
  await chooseEngine(page, dialog, NFS_ENGINE.name)
  await treeNodeContent(dialog, 'doc').click()

  await expect(dialog.locator('.selected-resource')).toContainText('节点')
  await expect(dialog.locator('.selected-resource')).toContainText('Business NFS / doc')
  await expect(dialog.getByText('递归', { exact: true })).toBeVisible()

  await expandTreeNode(dialog, 'doc')
  await treeNodeContent(dialog, 'README.md').click()

  await expect(dialog.locator('.selected-resource')).toContainText('数据项')
  await expect(dialog.locator('.selected-resource')).toContainText('Business NFS / doc/README.md')
  await expect(dialog.getByText('递归', { exact: true })).toHaveCount(0)
})

test('keeps a deep resource-tree node selected and populated after a basic refresh', async ({ page }) => {
  const browserErrors = []
  page.on('console', message => {
    if (message.type() === 'error') browserErrors.push(message.text())
  })
  page.on('pageerror', error => browserErrors.push(error.message))

  const backend = await installMockBackend(page, { refreshRegression: true })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(DOC_LOCATOR)}`)

  const docContent = treeNodeContent(page, 'doc')
  const nodeRefresh = docContent.getByTitle('基础刷新：重新发现此节点下的新资源')
  await expect(docContent).toBeVisible()
  await expect(docContent.locator('..')).toHaveClass(/is-current/)
  await expect(page.getByRole('button', { name: 'README.md', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'slides.pptx', exact: true })).toBeVisible()
  await expect(nodeRefresh).toBeVisible()

  await nodeRefresh.click()

  await expect.poll(() => backend.nodeRefreshRequests).toEqual([DOC_LOCATOR])
  await expect.poll(() => backend.scanExecutionRequests).toBe(1)
  await expect.poll(() => backend.treeRequests).toBe(1)
  await expect(docContent.locator('..')).toHaveClass(/is-current/)
  await expect(page.getByRole('button', { name: 'README.md', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'slides.pptx', exact: true })).toBeVisible()

  await expandTreeNode(page, 'doc')
  await expect(page.getByTitle('深度刷新：重建当前数据项的完整元数据')).toHaveCount(2)
  expect(browserErrors).toEqual([])
})

async function chooseEngine(page, dialog, engineName) {
  await dialog.locator('.resource-tree-picker .el-select').first().click()
  await page.getByRole('option', { name: new RegExp(engineName) }).click()
}

function treeNodeContent(scope, label) {
  return scope.locator('.el-tree-node__content').filter({
    hasText: new RegExp(`^\\s*${escapeRegExp(label)}\\s*$`)
  }).first()
}

async function pickerNodeClasses(scope, label) {
  return treeNodeContent(scope, label).locator('.picker-node').evaluate(element => element.className)
}

async function expandTreeNode(scope, label) {
  const content = treeNodeContent(scope, label)
  const expandIcon = content.locator(':scope > .el-tree-node__expand-icon')
  await expect(expandIcon).toBeVisible()
  if (!(await expandIcon.getAttribute('class'))?.includes('expanded')) {
    await expandIcon.click()
  }
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

async function installMockBackend(page, options = {}) {
  const tasks = options.includeFailedTasks
    ? failedTaskFixtures()
    : (options.includeResultTask ? [resultTask()] : (options.includeSpatialTask ? [spatialTask()] : []))
  const state = {
    capabilityLocators: [],
    quickViewActions: [],
    preferredModeRequests: [],
    taskDetailRequests: [],
    taskListQueries: [],
    deletedTasks: [],
    rebindRequests: [],
    nodeRefreshRequests: [],
    scanExecutionRequests: 0,
    treeRequests: 0,
    targetItemLookups: [],
    tasks
  }

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', 'light')
  })

  await page.route('**/plugins/manifest.json', route => fulfillJSON(route, { scripts: [] }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'manager-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'manager-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions: [] }] }
      })
    }
    if (path === '/api/v1/meta/engines') {
      return fulfillJSON(route, [POSTGRES_ENGINE, NFS_ENGINE])
    }
    if (path === '/api/v1/meta/items/by-catalog-path') {
      state.targetItemLookups.push({
        engineID: url.searchParams.get('engine_id') || '',
        catalogPath: url.searchParams.get('catalog_path') || ''
      })
      return fulfillJSON(route, {
        id: 1301,
        engine_id: NFS_ENGINE.id,
        item_type: 'file',
        full_name: 'tiles/farmland.pmtiles'
      })
    }
    if (path === `/api/v1/meta/resource-tree/${POSTGRES_ENGINE.id}`) {
      return fulfillJSON(route, postgresTree())
    }
    if (path === `/api/v1/meta/resource-tree/${POSTGRES_ENGINE.id}/node`) {
      const locator = url.searchParams.get('locator') || ''
      return fulfillJSON(route, {
        children: locator.includes('/path/public?')
          ? postgresTree().children[0].children
          : postgresTree().children
      })
    }
    if (path === `/api/v1/meta/resource-tree/${POSTGRES_ENGINE.id}/ancestors`) {
      return fulfillJSON(route, { ancestors: postgresAncestors(url.searchParams.get('locator')) })
    }
    if (path === `/api/v1/meta/resource-tree/${NFS_ENGINE.id}`) {
      state.treeRequests += 1
      return fulfillJSON(route, options.refreshRegression ? nfsShallowTree() : nfsTree())
    }
    if (path === `/api/v1/meta/resource-tree/${NFS_ENGINE.id}/ancestors`) {
      return fulfillJSON(route, {
        target_locator: DOC_LOCATOR,
        ancestors: [nfsShallowTree(), nfsDocNode()]
      })
    }
    if (path === `/api/v1/meta/resource-tree/${NFS_ENGINE.id}/refresh` && request.method() === 'POST') {
      state.nodeRefreshRequests.push(url.searchParams.get('locator') || '')
      return fulfillJSON(route, {
        run: {
          execution_id: 'node-refresh-execution-1',
          status: 'pending',
          progress: 10,
          current_step: 'queued'
        }
      }, 202)
    }
    if (path === '/api/v1/meta/executions/node-refresh-execution-1') {
      state.scanExecutionRequests += 1
      return fulfillJSON(route, {
        execution_id: 'node-refresh-execution-1',
        status: 'success',
        progress: 100,
        current_step: 'completed'
      })
    }
    if (path === `/api/v1/meta/resource-tree/${NFS_ENGINE.id}/node`) {
      const locator = url.searchParams.get('locator') || ''
      return fulfillJSON(route, locator === DOC_LOCATOR ? nfsDocNode() : nfsTree())
    }
    if (path === '/api/v1/manager/quick-view/actions' && request.method() === 'POST') {
      const payload = request.postDataJSON()
      state.quickViewActions.push(payload)
      const isPPTX = payload.action === 'generate_pptx_pdf'
      return fulfillJSON(route, {
        task_type: isPPTX ? 'pptx_pdf_generation' : 'vector_tile_cache_generation',
        task_id: isPPTX ? 52 : 51,
        execution_id: isPPTX ? 'pptx-execution-1' : 'quick-view-execution-1',
        status: 'pending'
      }, 202)
    }
    if (path === '/api/v1/manager/quick-view/capability') {
      const locator = url.searchParams.get('locator') || ''
      state.capabilityLocators.push(locator)
      return fulfillJSON(route, quickViewCapability(locator))
    }
    if (path === '/api/v1/manager/preview-state/preferred-mode' && request.method() === 'PATCH') {
      state.preferredModeRequests.push(request.postDataJSON())
      return fulfillJSON(route, {})
    }
    if (path === '/api/v1/manager/engines') {
      return fulfillJSON(route, { data: [POSTGRES_ENGINE, NFS_ENGINE] })
    }
    if (path === '/api/v1/manager/tasks/vector_tile_set_generation/41') {
      return fulfillJSON(route, tileCacheTask())
    }
    const taskRebindMatch = path.match(/^\/api\/v1\/manager\/tasks\/([^/]+)\/(\d+)\/rebind$/)
    if (taskRebindMatch && request.method() === 'POST') {
      const taskType = decodeURIComponent(taskRebindMatch[1])
      const taskID = Number(taskRebindMatch[2])
      state.rebindRequests.push({ taskType, taskID, body: request.postDataJSON() })
      state.tasks = state.tasks.filter(task => task.id !== taskID)
      return fulfillJSON(route, { task_type: taskType, task_id: 81, replaced_task_id: taskID })
    }
    const taskMemberMatch = path.match(/^\/api\/v1\/manager\/tasks\/([^/]+)\/(\d+)$/)
    if (taskMemberMatch && request.method() === 'GET') {
      const taskType = decodeURIComponent(taskMemberMatch[1])
      const taskID = Number(taskMemberMatch[2])
      const task = state.tasks.find(task => task.task_type === taskType && task.id === taskID) || {}
      if (taskType === 'vector_tile_cache_generation' && taskID === 61) {
        state.taskDetailRequests.push('vector_tile_cache_generation/61')
        return fulfillJSON(route, { ...task, has_current_result: options.resultExists !== false })
      }
      return fulfillJSON(route, task)
    }
    if (taskMemberMatch && request.method() === 'DELETE') {
      const taskKey = `${decodeURIComponent(taskMemberMatch[1])}/${taskMemberMatch[2]}`
      state.deletedTasks.push(taskKey)
      state.tasks = state.tasks.filter(task => `${task.task_type}/${task.id}` !== taskKey)
      return fulfillJSON(route, {})
    }
    if (path === '/api/v1/manager/tasks') {
      const executionStatus = url.searchParams.get('execution_status') || ''
      const bindingStatus = url.searchParams.get('binding_status') || ''
      state.taskListQueries.push({
        category: url.searchParams.get('category') || '',
        executionStatus,
        bindingStatus
      })
      let items = executionStatus
        ? state.tasks.filter(task => task.last_execution_status === executionStatus)
        : state.tasks
      if (bindingStatus) items = items.filter(task => task.binding_status === bindingStatus)
      return fulfillJSON(route, { items, total: items.length, page: 1, page_size: 20 })
    }
    if (path === '/api/v1/manager/vector_tile_cache') {
      return fulfillJSON(route, { data: [], total: 0 })
    }
    if (path === '/api/v1/manager/embedding_tasks' || path === '/api/v1/manager/embeddings') {
      return fulfillJSON(route, { data: [], total: 0 })
    }

    return fulfillJSON(route, {})
  })

  return state
}

function postgresTree() {
  return {
    id: 'addp://engine/11/path/?type=database&node_id=100',
    locator: 'addp://engine/11/path/?type=database&node_id=100',
    label: POSTGRES_ENGINE.name,
    type: 'database',
    children: [{
      id: 'addp://engine/11/path/public?type=schema&node_id=101',
      locator: 'addp://engine/11/path/public?type=schema&node_id=101',
      label: 'public',
      type: 'schema',
      children: [spatialTable('farmland', FARMLAND_LOCATOR, 1101), spatialTable('rivers', RIVERS_LOCATOR, 1102)]
    }]
  }
}

function spatialTable(label, locator, itemID) {
  return {
    id: locator,
    locator,
    label,
    type: 'table',
    children: [],
    metadata: {
      item_id: itemID,
      data_type: 'table',
      item_fingerprint: `fingerprint-${label}`,
      spatial: {
        geometry_columns: ['geometry'],
        primary_geometry_column: 'geometry'
      }
    }
  }
}

function postgresAncestors(locator) {
  const tree = postgresTree()
  const target = tree.children[0].children.find(node => node.locator === locator)
  return target ? [tree, tree.children[0], target] : []
}

function nfsTree() {
  return {
    id: 'addp://engine/12/path/?type=root&node_id=200',
    locator: 'addp://engine/12/path/?type=root&node_id=200',
    label: NFS_ENGINE.name,
    type: 'root',
    children: [nfsDocNode()]
  }
}

function nfsShallowTree() {
  return {
    ...nfsTree(),
    children: []
  }
}

function nfsDocNode() {
  return {
    id: DOC_LOCATOR,
    locator: DOC_LOCATOR,
    label: 'doc',
    type: 'directory',
    hasChildren: true,
    loaded: true,
    metadata: { item_count: 2, scanned_at: '2026-09-11T04:18:56Z' },
    children: [{
      id: README_LOCATOR,
      locator: README_LOCATOR,
      label: 'README.md',
      type: 'file',
      path: 'doc/README.md',
      children: [],
      metadata: { item_id: 1201, data_type: 'document', format: 'markdown' }
    }, {
      id: SLIDES_LOCATOR,
      locator: SLIDES_LOCATOR,
      label: 'slides.pptx',
      type: 'file',
      path: 'doc/slides.pptx',
      children: [],
      metadata: { item_id: 1202, data_type: 'document', format: 'pptx' }
    }]
  }
}

function quickViewCapability(locator) {
  if (locator === SLIDES_LOCATOR) {
    return {
      locator,
      source_kind: 'document',
      source_engine_id: NFS_ENGINE.id,
      item_fingerprint: 'fingerprint-slides',
      available_actions: ['generate_pptx_pdf'],
      pptx_pdf: { format: 'pptx', status: 'missing' }
    }
  }
  const parsedTable = locator === RIVERS_LOCATOR ? 'rivers' : 'farmland'
  return {
    locator,
    source_engine_id: POSTGRES_ENGINE.id,
    source_schema: 'public',
    source_table: parsedTable,
    item_fingerprint: `fingerprint-${parsedTable}`,
    quick_view: {
      geometry_column: 'geometry',
      geometry_columns: ['geometry'],
      min_zoom: 4,
      max_zoom: 12,
      target_srid: 3857
    },
    render_facts: {
      source_srid: 4326,
      render_extent: [108.5, 24.5, 114.3, 30.2],
      render_extent_srid: 4326,
      zoom_recommendation: { min_zoom: 4, max_zoom: 12, tile_budget: 10000 }
    },
    optimization: { available: true, status: 'ready' },
    realtime_tile: { performance_mode: 'native_mvt' },
    default_vector_tile_cache_id: locator === RIVERS_LOCATOR ? 61 : 0,
    available_actions: locator === RIVERS_LOCATOR ? [] : ['generate_vector_tile_cache']
  }
}

function tileCacheTask() {
  return {
    id: 41,
    name: 'public.farmland 瓦片缓存',
    description: '',
    enabled: true,
    config: {
      target: {
        source_engine_id: POSTGRES_ENGINE.id,
        source_kind: 'table',
        full_name: 'public/farmland',
        schema: 'public',
        table: 'farmland',
        item_id: 1101,
        item_fingerprint: 'fingerprint-farmland',
        locator: FARMLAND_LOCATOR
      },
      tile: {
        archive_format: 'pmtiles',
        tile_type: 'mvt',
        tile_matrix_set: 'WebMercatorQuad',
        min_zoom: 4,
        max_zoom: 12,
        source_srid: 4326,
        target_srid: 3857,
        extent_srid: 4326,
        extent: [108.5, 24.5, 114.3, 30.2]
      },
      storage: {},
      options: { geometry_column: 'geometry' }
    }
  }
}

function resultTask() {
  return {
    id: 61,
    task_type: 'vector_tile_cache_generation',
    category: 'managed_quick_view',
    name: 'public.rivers 瓦片缓存',
    enabled: true,
    has_current_result: true,
    current_result_status: 'ready',
    last_execution_status: 'success',
    last_execution_id: 'result-execution-61',
    updated_at: '2026-09-08T12:00:00Z',
    config: {
      target: {
        source_engine_id: POSTGRES_ENGINE.id,
        item_locator: RIVERS_LOCATOR
      }
    }
  }
}

function spatialTask() {
  return {
    id: 62,
    task_type: 'vector_tile_set_generation',
    category: 'spatial_business',
    name: '耕地矢量瓦片集',
    enabled: true,
    last_execution_status: 'success',
    last_execution_id: 'spatial-execution-62',
    updated_at: '2026-09-08T13:00:00Z',
    config: {
      source: {
        source_engine_id: POSTGRES_ENGINE.id,
        locator: FARMLAND_LOCATOR,
        item_id: 1101
      },
      target: {
        engine_id: NFS_ENGINE.id,
        storage_locator: 'addp://engine/12/path/tiles?type=directory&node_id=230',
        name: 'farmland.pmtiles'
      }
    }
  }
}

function failedTaskFixtures() {
  return [{
    id: 71,
    version: 1,
    task_type: 'vector_tile_cache_generation',
    category: 'managed_quick_view',
    name: '失败任务 - farmland',
    enabled: true,
    last_execution_status: 'failed',
    binding_status: 'missing',
    binding_issue: 'missing_source',
    updated_at: '2026-09-09T12:00:00Z',
    config: { target: { source_engine_id: POSTGRES_ENGINE.id, item_locator: FARMLAND_LOCATOR } }
  }, {
    id: 72,
    version: 1,
    task_type: 'model_3d_glb_generation',
    category: 'managed_quick_view',
    name: '失败任务 - building',
    enabled: true,
    last_execution_status: 'failed',
    binding_status: 'missing',
    binding_issue: 'missing_engine',
    updated_at: '2026-09-09T12:01:00Z',
    config: { source: { source_engine_id: NFS_ENGINE.id, item_locator: README_LOCATOR } }
  }, {
    id: 73,
    version: 1,
    task_type: 'vector_tile_cache_generation',
    category: 'managed_quick_view',
    name: '成功任务 - rivers',
    enabled: true,
    last_execution_status: 'success',
    binding_status: 'active',
    binding_issue: '',
    updated_at: '2026-09-09T12:02:00Z',
    config: { target: { source_engine_id: POSTGRES_ENGINE.id, item_locator: RIVERS_LOCATOR } }
  }]
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
