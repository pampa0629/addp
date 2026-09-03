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

test('selects a spatial table through the shared picker and applies capability facts', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/spatial-quick-view/vector-tile-cache?create=1')

  const dialog = page.getByRole('dialog', { name: '新建瓦片缓存任务' })
  await expect(dialog).toBeVisible()
  await chooseEngine(page, dialog, POSTGRES_ENGINE.name)
  await expandTreeNode(dialog, 'public')
  await treeNodeContent(dialog, 'farmland').click()

  await expect(dialog.locator('.selected-resource')).toContainText('public.farmland')
  await expect(dialog.locator('.selected-resource')).toContainText('Business PostgreSQL')
  await expect(dialog.getByText('geometry', { exact: true }).first()).toBeVisible()
  await expect.poll(() => backend.capabilityLocators).toEqual([FARMLAND_LOCATOR])
})

test('restores and locks the source table while editing an existing task', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/spatial-quick-view/vector-tile-cache?task_id=41')

  const dialog = page.getByRole('dialog', { name: '编辑瓦片缓存任务' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('源数据已锁定', { exact: true })).toBeVisible()
  await expect(dialog.locator('.selected-resource')).toContainText('public.farmland')
  expect(await pickerNodeClasses(dialog, 'farmland')).not.toContain('is-disabled')
  expect(await pickerNodeClasses(dialog, 'rivers')).toContain('is-disabled')

  await treeNodeContent(dialog, 'rivers').click()
  await expect(dialog.locator('.selected-resource')).toContainText('public.farmland')
  expect(backend.capabilityLocators).not.toContain(RIVERS_LOCATOR)
})

test('keeps directory and file vectorization semantics in the shared picker', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/vectorization-tasks?create=1')

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

async function installMockBackend(page) {
  const state = { capabilityLocators: [] }

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
      return fulfillJSON(route, nfsTree())
    }
    if (path === `/api/v1/meta/resource-tree/${NFS_ENGINE.id}/node`) {
      const locator = url.searchParams.get('locator') || ''
      return fulfillJSON(route, {
        children: locator === DOC_LOCATOR ? nfsTree().children[0].children : nfsTree().children
      })
    }
    if (path === '/api/v1/manager/quick-view/capability') {
      const locator = url.searchParams.get('locator') || ''
      state.capabilityLocators.push(locator)
      return fulfillJSON(route, quickViewCapability(locator))
    }
    if (path === '/api/v1/manager/engines') {
      return fulfillJSON(route, { data: [POSTGRES_ENGINE, NFS_ENGINE] })
    }
    if (path === '/api/v1/manager/vector_tile_cache_tasks/41') {
      return fulfillJSON(route, tileCacheTask())
    }
    if (path === '/api/v1/manager/vector_tile_cache_tasks') {
      return fulfillJSON(route, { data: [tileCacheTask()], total: 1 })
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
    children: [{
      id: DOC_LOCATOR,
      locator: DOC_LOCATOR,
      label: 'doc',
      type: 'directory',
      children: [{
        id: README_LOCATOR,
        locator: README_LOCATOR,
        label: 'README.md',
        type: 'file',
        path: 'doc/README.md',
        children: [],
        metadata: { item_id: 1201, data_type: 'document', format: 'markdown' }
      }]
    }]
  }
}

function quickViewCapability(locator) {
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
    realtime_tile: { performance_mode: 'native_mvt' }
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

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
