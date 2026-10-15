import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const TIDB_ENGINE = {
  id: 25,
  name: 'Business TiDB',
  engine_type: 'tidb',
  lifecycle_state: 'active',
  connection_status: 'online'
}

const ROOT_LOCATOR = 'addp://engine/25/path/?type=server&node_id=377'
const BUSINESS_LOCATOR = 'addp://engine/25/path/business?type=database&node_id=378'

for (const [locale, label, completedDepth] of [['zh-cn', '深度扫描', '深度'], ['en', 'Deep Scan', 'Deep']]) {
  test(`deep scans only the selected node and updates its range facts (${locale})`, async ({ page }) => {
    const backend = await installMockBackend(page, { locale, executionStatus: 'running' })
    await page.goto('/data-explorer')
    await treeNodeContent(page, TIDB_ENGINE.name).click()
    await treeNodeContent(page, 'business').click()
    const button = page.getByRole('button', { name: label, exact: true })
    await button.click()
    await expect(button).toBeDisabled()
    await expect(page.locator('.scan-status')).toHaveCount(1)
    await expect(page.locator('.scan-status .el-progress')).toBeVisible()
    await expect(page.locator('.scan-status__percent')).toHaveText('41%')
    backend.progress = 72
    await expect(page.locator('.scan-status__percent')).toHaveText('72%')
    await expect.poll(() => backend.deepScanRequests).toEqual([{
      engine_id: 25, targets: [BUSINESS_LOCATOR], scan_depth: 'deep', force: true, source: 'manager'
    }])
    expect(backend.refreshRequests).toEqual([])
    backend.executionStatus = 'success'
    await expect(button).toBeEnabled()
    await expect(page.locator('.scan-status__percent')).toHaveText('100%')
    await expect(page.locator('.scan-status .el-progress')).toHaveClass(/is-success/)
    await expect(page.locator('.node-panel .el-descriptions')).toContainText(completedDepth)
    await expect(page.locator('.node-panel .el-descriptions')).toContainText('2026-10-14T01:02:03Z')
    await expect(page.locator('.node-panel .header-title')).toHaveText('business')
    await expect(treeNodeContent(page, 'customers')).toBeVisible()
  })
}

test('keeps the new selection when a node deep scan finishes', async ({ page }) => {
  const backend = await installMockBackend(page, { executionStatus: 'running' })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await treeNodeContent(page, 'business').click()
  await page.getByRole('button', { name: '深度扫描', exact: true }).click()
  await expect.poll(() => backend.deepScanRequests.length).toBe(1)
  await treeNodeContent(page, 'customers').click()
  await expect(page.locator('.node-panel')).toHaveCount(0)
  await expect(page.locator('.scan-status .el-progress')).toBeVisible()
  await expect(page.locator('.scan-status__percent')).toHaveText('41%')
  backend.holdNodeReload = true
  backend.executionStatus = 'success'
  await expect(page.locator('.scan-status__title')).toHaveText('扫描已完成，正在刷新资源树')
  await expect.poll(() => typeof backend.releaseNodeReload).toBe('function')
  backend.releaseNodeReload()
  await expect(page.locator('.el-message--success')).toBeVisible()
  await expect(page.locator('.scan-status__title')).toHaveText('扫描已完成，资源树已刷新')
  await expect(page.locator('.node-panel')).toHaveCount(0)
  await expect(treeNodeContent(page, 'customers').locator('..')).toHaveClass(/is-current/)
})

test('shows deep scan conflicts and failures without reporting success', async ({ page }) => {
  const backend = await installMockBackend(page, { deepScanConflict: true })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await treeNodeContent(page, 'business').click()
  const button = page.getByRole('button', { name: '深度扫描', exact: true })
  await button.click()
  await expect(page.locator('.el-message--warning')).toContainText('该扫描范围正在执行中')
  await expect(page.locator('.scan-status')).toHaveCount(0)
  await expect(button).toBeEnabled()
  await expect(page.locator('.el-message--error')).toHaveCount(0)
  backend.deepScanConflict = false
  backend.executionStatus = 'failed'
  await button.click()
  await expect(page.locator('.el-message--error')).toContainText('深度扫描失败：输入或语法错误')
  await expect(page.locator('.scan-status__title')).toHaveText('后台扫描失败')
  await expect(page.locator('.scan-status__detail')).toHaveText('输入或语法错误')
  const failure = page.locator('.node-panel .execution-failure')
  await expect(failure).toContainText('scan_target_invalid')
  await expect(failure).toContainText('node-deep-scan-1')
  const monitorLink = failure.getByRole('link', { name: '查看监控错误详情' })
  await expect(monitorLink).toBeVisible()
  await expect(monitorLink).toHaveAttribute('href', /execution_id=node-deep-scan-1/)
  await expect(page.locator('.node-panel .el-descriptions')).toContainText('失败')
  await expect(page.locator('.node-panel .el-descriptions')).toContainText('2026-10-13T01:02:03Z')
  await expect(page.getByText('PRIVATE_SCAN_RUNTIME_DETAIL', { exact: false })).toHaveCount(0)
  await expect(button).toBeEnabled()
  await expect(page.locator('.el-message--success')).toHaveCount(0)
})

test('keeps long resource names and refresh actions inside a scrolling tree panel', async ({ page }) => {
  const labels = Array.from({ length: 60 }, (_, index) => `resource_${index}_with_a_long_name_that_must_not_push_the_refresh_action_outside_the_panel`)
  const backend = await installMockBackend(page, { labels })
  await page.setViewportSize({ width: 1280, height: 560 })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  const branch = treeNodeContent(page, 'business')
  await branch.locator(':scope > .el-tree-node__expand-icon').click()
  await expect(branch.locator('..')).toHaveAttribute('aria-expanded', 'true')

  const action = treeNodeContent(page, labels[45]).getByTitle('深度刷新：重建当前数据项的完整元数据')
  await expect(action).toBeVisible()
  await expect.poll(async () => {
    await action.scrollIntoViewIfNeeded()
    return action.evaluate(element => {
      const tree = element.closest('.el-scrollbar__wrap')
      const panel = element.closest('.split-container').querySelector(':scope > .tree-container')
      const rect = element.getBoundingClientRect()
      const treeRect = tree.getBoundingClientRect()
      const panelRect = panel.getBoundingClientRect()
      const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)
      return {
        treeFitsPanel: treeRect.bottom <= panelRect.bottom && treeRect.right <= panelRect.right,
        panelFitsViewport: panelRect.bottom <= innerHeight,
        actionFitsPanel: rect.right <= panelRect.right && rect.bottom <= panelRect.bottom,
        actionReceivesClick: element.contains(hit),
        treeScrolls: tree.scrollHeight > tree.clientHeight
      }
    })
  }).toEqual({
    treeFitsPanel: true,
    panelFitsViewport: true,
    actionFitsPanel: true,
    actionReceivesClick: true,
    treeScrolls: true
  })
  await action.click()
  await expect.poll(() => backend.itemRefreshRequests).toEqual([
    'addp://engine/25/path/business/' + labels[45] + '?type=table&item_id=946'
  ])
})

test('loads and expands an engine catalog root with the first arrow click', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/data-explorer')

  const rootContent = treeNodeContent(page, TIDB_ENGINE.name)
  const rootNode = rootContent.locator('..')
  const expandIcon = rootContent.locator(':scope > .el-tree-node__expand-icon')

  await expect(expandIcon).toHaveClass(/is-leaf/)
  await expandIcon.click()

  await expect(rootNode).toHaveAttribute('aria-expanded', 'true')
  await expect(treeNodeContent(page, 'business')).toBeVisible()
})

test('expands an unloaded TiDB database after refreshing the catalog root', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/data-explorer')

  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await expect(treeNodeContent(page, 'business')).toBeVisible()

  const rootContent = treeNodeContent(page, TIDB_ENGINE.name)
  await rootContent.getByTitle('基础刷新：重新发现此节点下的新资源').click()

  await expect.poll(() => backend.refreshRequests).toEqual([ROOT_LOCATOR])
  const businessContent = treeNodeContent(page, 'business')
  const expandIcon = businessContent.locator(':scope > .el-tree-node__expand-icon')
  await expect(expandIcon).toHaveClass(/is-leaf/)
  await expect(treeNodeContent(page, 'customers')).toHaveCount(0)

  await expandIcon.click()

  await expect.poll(() => backend.childRequests).toContain(BUSINESS_LOCATOR)
  await expect(treeNodeContent(page, 'addp_engine_probe')).toBeVisible()
  await expect(treeNodeContent(page, 'customers')).toBeVisible()
  await expect(treeNodeContent(page, 'orders')).toBeVisible()
})

function treeNodeContent(scope, label) {
  return scope.locator('.el-tree-node__content').filter({
    hasText: new RegExp(`^\\s*${escapeRegExp(label)}\\s*$`)
  }).first()
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

test('shows a scan submission conflict without marking a scan as failed', async ({ page }) => {
  const message = '该扫描范围正在执行中，请等待当前扫描完成'
  const backend = await installMockBackend(page, { conflictLocator: ROOT_LOCATOR })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await expect(treeNodeContent(page, 'business')).toBeVisible()
  await treeNodeContent(page, TIDB_ENGINE.name).getByTitle('基础刷新：重新发现此节点下的新资源').click()
  await expect(page.locator('.el-message--warning')).toHaveText(message)
  await expect(page.locator('.scan-status')).toHaveCount(0)
  await expect(page.getByText('后台扫描失败', { exact: true })).toHaveCount(0)
  await expect.poll(() => backend.refreshRequests).toEqual([ROOT_LOCATOR])
})

test('preserves an active scan progress when another submission conflicts', async ({ page }) => {
  const backend = await installMockBackend(page, { conflictLocator: BUSINESS_LOCATOR, executionStatus: 'running' })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await expect(treeNodeContent(page, 'business')).toBeVisible()
  await treeNodeContent(page, TIDB_ENGINE.name).getByTitle('基础刷新：重新发现此节点下的新资源').click()
  await expect(page.locator('.scan-status__percent')).toHaveText('41%')
  await treeNodeContent(page, 'business').getByTitle('基础刷新：重新发现此节点下的新资源').click()
  await expect(page.locator('.el-message--warning')).toBeVisible()
  await expect(page.locator('.scan-status__percent')).toHaveText('41%')
  await expect(page.locator('.scan-status__title')).toHaveText('后台扫描进行中')
  await expect(page.locator('.el-progress.is-exception')).toHaveCount(0)
  backend.executionStatus = 'success'
  await expect(page.locator('.scan-status__title')).toHaveText('扫描已完成，资源树已刷新')
  await expect.poll(() => backend.refreshRequests).toEqual([ROOT_LOCATOR, BUSINESS_LOCATOR])
})

test('keeps real server errors visible with their returned explanation', async ({ page }) => {
  await installMockBackend(page, { refreshError: { error: '扫描服务暂时不可用' } })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await expect(treeNodeContent(page, 'business')).toBeVisible()
  await treeNodeContent(page, TIDB_ENGINE.name).getByTitle('基础刷新：重新发现此节点下的新资源').click()
  await expect(page.locator('.scan-status__title')).toHaveText('后台扫描失败')
  await expect(page.locator('.scan-status__detail')).toHaveText('扫描服务暂时不可用')
  await expect(page.locator('.el-message--error')).toContainText('扫描服务暂时不可用')
})

async function installMockBackend(page, options = {}) {
  const state = {
    refreshRequests: [],
    deepScanRequests: [],
    deepScanConflict: options.deepScanConflict || false,
    itemRefreshRequests: [],
    childRequests: [],
    executionStatus: options.executionStatus || 'success',
    progress: 41,
    scanProgress: options.scanProgress,
    holdNodeReload: false,
    releaseNodeReload: null
  }

  await page.addInitScript(locale => {
    localStorage.setItem('addp-lang', locale)
    localStorage.setItem('theme-mode', 'light')
  }, options.locale || 'zh-cn')

  await page.route('**/plugins/manifest.json', route => fulfillJSON(route, { scripts: [] }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'manager-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: '1', display_name: 'manager-e2e', local_account: { username: 'manager-e2e' } })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, managerAuthContext)
    }
    if (path === '/api/v1/manager/engines') {
      return fulfillJSON(route, { data: [TIDB_ENGINE] })
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}`) {
      return fulfillJSON(route, shallowTree())
    }
    if (path === `/api/v1/manager/engines/${TIDB_ENGINE.id}/items/refresh` && request.method() === 'POST') {
      state.itemRefreshRequests.push(url.searchParams.get('locator') || '')
      return fulfillJSON(route, {})
    }
    if (path === '/api/v1/meta/scan/run/manual' && request.method() === 'POST') {
      state.deepScanRequests.push(request.postDataJSON())
      if (state.deepScanConflict) {
        return fulfillJSON(route, { error_code: 'scan_scope_active', error: '该扫描范围正在执行中，请等待当前扫描完成' }, 409)
      }
      return fulfillJSON(route, { execution_id: 'node-deep-scan-1', status: 'pending' }, 201)
    }
    if (path === '/api/v1/meta/executions/node-deep-scan-1') {
      return fulfillJSON(route, {
        execution_id: 'node-deep-scan-1', status: state.executionStatus,
        started_at: new Date(Date.now() - 120000).toISOString(),
        scan_progress: state.scanProgress,
        progress: state.executionStatus === 'success' ? 100 : state.progress,
        error_details: state.executionStatus === 'failed' ? {
          category: 'invalid_input', code: 'scan_target_invalid', failed_targets_count: 1,
          message: 'PRIVATE_SCAN_RUNTIME_DETAIL'
        } : undefined
      })
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}/refresh` && request.method() === 'POST') {
      state.refreshRequests.push(url.searchParams.get('locator') || '')
      if (options.refreshError) return fulfillJSON(route, options.refreshError, 500)
      if (url.searchParams.get('locator') === options.conflictLocator) {
        return fulfillJSON(route, {
          error: '该扫描范围正在执行中，请等待当前扫描完成',
          error_code: 'scan_scope_active'
        }, 409)
      }
      return fulfillJSON(route, {
        run: {
          execution_id: 'tidb-root-refresh-execution-1',
          status: 'pending',
          progress: 10,
          current_step: 'queued'
        }
      }, 202)
    }
    if (path === '/api/v1/meta/executions/tidb-root-refresh-execution-1') {
      return fulfillJSON(route, {
        execution_id: 'tidb-root-refresh-execution-1',
        status: state.executionStatus,
        progress: state.executionStatus === 'success' ? 100 : 41,
        current_step: state.executionStatus === 'success' ? 'completed' : 'scanning'
      })
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}/node`) {
      const locator = url.searchParams.get('locator') || ''
      state.childRequests.push(locator)
      if (locator === BUSINESS_LOCATOR && state.holdNodeReload && state.deepScanRequests.length && state.executionStatus === 'success') {
        await new Promise(resolve => { state.releaseNodeReload = resolve })
      }
      const node = locator === BUSINESS_LOCATOR ? businessNode(true, options.labels) : shallowTree()
      if (locator === BUSINESS_LOCATOR) node.metadata = { ...node.metadata, ...options.nodeMetadata }
      if (locator === BUSINESS_LOCATOR && state.deepScanRequests.length && state.executionStatus === 'success') {
        node.metadata = { ...node.metadata, scan_status: 'completed', scanned_depth: 'deep', scanned_at: '2026-10-14T01:02:03Z' }
      }
      if (locator === BUSINESS_LOCATOR && state.deepScanRequests.length && state.executionStatus === 'failed') {
        node.metadata = { ...node.metadata, scan_status: 'failed', scanned_depth: 'basic', scanned_at: '2026-10-13T01:02:03Z' }
      }
      return fulfillJSON(route, node)
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}/ancestors`) {
      return fulfillJSON(route, { ancestors: [{ ...shallowTree(), children: [] }], target_locator: ROOT_LOCATOR })
    }

    return fulfillJSON(route, {})
  })

  return state
}

function shallowTree() {
  return {
    id: ROOT_LOCATOR,
    locator: ROOT_LOCATOR,
    label: TIDB_ENGINE.name,
    type: 'server',
    hasChildren: true,
    children: [businessNode(false)]
  }
}

function businessNode(withChildren, labels = ['addp_engine_probe', 'customers', 'orders']) {
  return {
    id: BUSINESS_LOCATOR,
    locator: BUSINESS_LOCATOR,
    label: 'business',
    type: 'database',
    hasChildren: true,
    metadata: { item_count: 3 },
    children: withChildren
      ? labels.map((label, index) => ({
          id: `addp://engine/25/path/business/${label}?type=table&item_id=${901 + index}`,
          locator: `addp://engine/25/path/business/${label}?type=table&item_id=${901 + index}`,
          label,
          type: 'table',
          hasChildren: false,
          children: []
        }))
      : []
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}


test('reports discovery, recursive item totals and large-file reads separately from history', async ({ page }) => {
  const snapshot = { phase: 'discovering', operation: 'listing', current_path: '3d',
    files_discovered: 30, directories_discovered: 4, files_total_known: false,
    items_planned: 0, items_total_known: false, items_completed: 0, items_succeeded: 0, items_failed: 0,
    current_bytes_read: 0, current_bytes_total: 0, last_progress_at: new Date(Date.now() - 40000).toISOString() }
  const backend = await installMockBackend(page, { executionStatus: 'running', scanProgress: snapshot,
    nodeMetadata: { scan_status: 'failed', scanned_depth: 'deep', scanned_at: '2026-10-13T01:02:03Z' } })
  await page.goto('/data-explorer')
  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await treeNodeContent(page, 'business').click()
  await page.getByRole('button', { name: '深度扫描', exact: true }).click()
  const progress = page.locator('.scan-status')
  await expect(progress.locator('.scan-status__percent')).toHaveText('总量待确定')
  await expect(progress.locator('.scan-status__indeterminate')).toBeVisible()
  await expect(progress).toContainText('已发现 30 个文件、4 个目录')
  await expect(progress.locator('.scan-status__timing')).toContainText('耗时')
  await expect(progress.locator('.scan-status__stale')).toBeVisible()
  const description = page.locator('.node-panel .el-descriptions')
  await expect(description).toContainText('本次执行状态运行中')
  await expect(description).toContainText('最近范围扫描状态失败')
  await expect(description).toContainText('2026-10-13T01:02:03Z')
  backend.scanProgress = { ...snapshot, phase: 'processing', operation: 'hashing', current_path: '3d/model/large.obj',
    files_discovered: 19238, directories_discovered: 172, files_total_known: true,
    items_planned: 100, items_total_known: true, items_completed: 20, items_succeeded: 19, items_failed: 1,
    current_bytes_read: 1048576, current_bytes_total: 1073741824, last_progress_at: new Date().toISOString() }
  await expect(progress.locator('.scan-status__percent')).toHaveText('20%')
  await expect(progress).toContainText('文件总数 19238，目录 172')
  await expect(progress).toContainText('已处理 20/100 个数据项')
  await expect(progress).toContainText('成功 19，失败 1')
  await expect(progress).toContainText('3d/model/large.obj')
  await expect(progress).toContainText('内容指纹读取 1.0 MiB / 1.0 GiB')
  await expect(progress.locator('.scan-status__stale')).toHaveCount(0)
  backend.scanProgress.current_bytes_read = 104857600
  await expect(progress).toContainText('内容指纹读取 100.0 MiB / 1.0 GiB')
  backend.scanProgress = { ...backend.scanProgress, operation: 'enriching', current_bytes_read: 0, current_bytes_total: 0,
    current_range_bytes_read: 229376, current_ranges_read: 4096 }
  await expect(progress).toContainText('属性范围读取 224.0 KiB，已读取 4096 个范围')
  await expect(progress).not.toContainText('内容指纹读取')
  await expect(progress.locator('.scan-status__percent')).toHaveText('20%')
  backend.scanProgress.current_range_bytes_read = 458752
  backend.scanProgress.current_ranges_read = 8192
  await expect(progress).toContainText('属性范围读取 448.0 KiB，已读取 8192 个范围')
  backend.scanProgress = { ...backend.scanProgress, operation: 'cleaning_index', current_bytes_read: 0, current_bytes_total: 0,
    index_cleanup_total: 205, index_cleanup_completed: 100, index_cleanup_failed: 0 }
  await expect(progress).toContainText('清理旧组件索引：成功 100/205，未确认 0')
  backend.scanProgress.index_cleanup_completed = 105
  backend.scanProgress.index_cleanup_failed = 100
  await expect(progress).toContainText('清理旧组件索引：成功 105/205，未确认 100')
  backend.executionStatus = 'failed'
  await expect(progress.locator('.scan-status__title')).toHaveText('后台扫描失败')
  await expect(progress.locator('.scan-status__percent')).toHaveText('20%')
  await expect(progress).toContainText('成功 19，失败 1')
})
