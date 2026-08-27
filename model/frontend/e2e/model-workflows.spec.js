import { expect, test } from '@playwright/test'

const DOMAINS = [
  { id: 1, name: '客户域' },
  { id: 2, name: '户外域' }
]

const ENTITIES = [
  {
    id: 7,
    tenant_id: 1,
    domain_id: 2,
    name: '活动',
    code: 'outdoor',
    description: '',
    status: 'draft',
    version: 1,
    created_at: '2026-08-11T10:50:11+08:00'
  },
  {
    id: 8,
    tenant_id: 1,
    domain_id: 1,
    name: '客户',
    code: 'customer',
    description: '',
    status: 'draft',
    version: 1,
    created_at: '2026-08-11T11:00:00+08:00'
  }
]

const LOGICAL_TABLE = {
  id: 2,
  tenant_id: 1,
  domain_id: 1,
  name: '省份',
  code: 'dwd_province',
  table_type: 'dimension',
  layer: 'dwd',
  status: 'approved',
  scd_type: 0,
  description: '',
  version: 1,
  materialization: {
    target_parent_locator: 'addp://engine/2/path/public?type=schema',
    target_name: 'dwd_province',
    partition_by: '',
    partition_type: 'range'
  }
}

const DW_LAYER = {
  id: 1,
  tenant_id: 1,
  layer_code: 'dwd',
  layer_name: '明细层',
  description: '明细数据分层',
  naming_rule: 'dwd_{domain}_{entity}',
  quality_sla: {},
  sort_order: 2,
  version: 1
}

const DEFAULT_PERMISSIONS = [
  'model.entity.read',
  'model.logical_model.read'
]

const MERMAID_SNAPSHOT = `erDiagram
  %% addp:entity {"code":"outdoor","name":"活动","domain_id":2,"description":"户外活动"}
  outdoor {
  }
`

test('shows an explicit permission error instead of an empty entity page after a 403', async ({ page }) => {
  const backend = await installMockBackend(page, { forbidEntityList: true })

  await page.goto('/entities')

  const permissionAlert = page.getByRole('alert').filter({
    hasText: '当前账号没有访问权限，请联系租户管理员分配对应角色或权限后重试。'
  })
  await expect(permissionAlert).toBeVisible()
  await expect(permissionAlert.getByRole('button', { name: '重试', exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toHaveCount(0)
  await expect.poll(() => backend.getEntityListRequests()).toBe(1)
})

test('preserves the business-domain URL state across entity detail navigation', async ({ page }) => {
  await installMockBackend(page)

  await page.goto('/entities?domain_id=2')
  await expect(page).toHaveURL(/\/entities\?domain_id=2$/)
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('cell', { name: '活动', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '客户', exact: true })).toHaveCount(0)
  await expect(page.locator('.el-pagination').getByText('20条/页', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '设计', exact: true }).click()
  await expect(page).toHaveURL(/\/entities\/7\?domain_id=2$/)
  await expect(page.getByRole('textbox', { name: '实体名称', exact: true })).toHaveValue('活动')

  await page.getByRole('button', { name: '返回', exact: true }).click()
  await expect(page).toHaveURL(/\/entities\?domain_id=2$/)
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('cell', { name: '活动', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '客户', exact: true })).toHaveCount(0)
})

test('preserves an unsaved entity draft when another page advances the resource version', async ({ browser }) => {
  const context = await browser.newContext({ baseURL: 'http://127.0.0.1:4182' })
  const backend = await installMockBackend(context, {
    concurrentEntity: true,
    permissions: [...DEFAULT_PERMISSIONS, 'model.entity.update']
  })
  const pageA = await context.newPage()
  const pageB = await context.newPage()

  try {
    await Promise.all([pageA.goto('/entities/7'), pageB.goto('/entities/7')])
    await expect(pageA.getByRole('textbox', { name: '实体名称', exact: true })).toHaveValue('活动')
    await expect(pageB.getByRole('textbox', { name: '实体名称', exact: true })).toHaveValue('活动')

    await pageA.getByRole('textbox', { name: '实体名称', exact: true }).fill('活动并发临时')
    await pageA.getByRole('button', { name: '保存', exact: true }).click()
    await expect(pageA.getByRole('alert').filter({ hasText: '保存成功' })).toBeVisible()

    await pageB.getByRole('textbox', { name: '描述', exact: true }).fill('并发冲突草稿')
    await pageB.getByRole('button', { name: '保存', exact: true }).click()
    await expect(pageB.getByRole('alert').filter({
      hasText: '资源已被其他用户修改，当前未保存内容已保留。请确认后手动刷新，再重新提交。'
    })).toBeVisible()
    await expect(pageB.getByRole('textbox', { name: '实体名称', exact: true })).toHaveValue('活动')
    await expect(pageB.getByRole('textbox', { name: '描述', exact: true })).toHaveValue('并发冲突草稿')
    await expect(pageB.getByText('未保存', { exact: true })).toBeVisible()
    expect(backend.getUpdateVersions()).toEqual([1, 1])

    await pageB.getByRole('button', { name: '刷新', exact: true }).click()
    const discardDialog = pageB.getByRole('dialog', { name: '存在未保存内容' })
    await expect(discardDialog).toBeVisible()
    await expect(discardDialog).toContainText('离开或刷新将丢失当前未保存内容，是否继续？')
    await discardDialog.getByRole('button', { name: '放弃并继续', exact: true }).click()

    await expect(pageB.getByRole('textbox', { name: '实体名称', exact: true })).toHaveValue('活动并发临时')
    await expect(pageB.getByRole('textbox', { name: '描述', exact: true })).toHaveValue('')
    await expect(pageB.getByText('未保存', { exact: true })).toHaveCount(0)
    expect(backend.getEntity().version).toBe(2)
  } finally {
    await context.close()
  }
})

test('preserves an unsaved logical-table draft when another page advances the aggregate version', async ({ browser }) => {
  const context = await browser.newContext({ baseURL: 'http://127.0.0.1:4182' })
  const backend = await installMockBackend(context, {
    concurrentLogicalTable: true,
    permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update']
  })
  const pageA = await context.newPage()
  const pageB = await context.newPage()

  try {
    await Promise.all([pageA.goto('/logical-tables/2'), pageB.goto('/logical-tables/2')])
    await expect(pageA.getByRole('textbox', { name: '逻辑表名', exact: true })).toHaveValue('省份')
    await expect(pageB.getByRole('textbox', { name: '逻辑表名', exact: true })).toHaveValue('省份')

    await pageA.getByRole('textbox', { name: '逻辑表名', exact: true }).fill('省份并发更新')
    await pageA.getByRole('button', { name: '保存', exact: true }).click()
    await expect(pageA.getByRole('alert').filter({ hasText: '保存成功' })).toBeVisible()

    await pageB.getByRole('textbox', { name: '描述', exact: true }).fill('逻辑表并发冲突草稿')
    await pageB.getByRole('button', { name: '保存', exact: true }).click()
    await expect(pageB.getByRole('alert').filter({
      hasText: '资源已被其他用户修改，当前未保存内容已保留。请确认后手动刷新，再重新提交。'
    })).toBeVisible()
    await expect(pageB.getByRole('textbox', { name: '逻辑表名', exact: true })).toHaveValue('省份')
    await expect(pageB.getByRole('textbox', { name: '描述', exact: true })).toHaveValue('逻辑表并发冲突草稿')
    await expect(pageB.getByText('未保存', { exact: true })).toBeVisible()
    expect(backend.getLogicalTableUpdateVersions()).toEqual([1, 1])

    await pageB.getByRole('button', { name: '刷新', exact: true }).click()
    const discardDialog = pageB.getByRole('dialog', { name: '存在未保存内容' })
    await expect(discardDialog).toBeVisible()
    await discardDialog.getByRole('button', { name: '放弃并继续', exact: true }).click()

    await expect(pageB.getByRole('textbox', { name: '逻辑表名', exact: true })).toHaveValue('省份并发更新')
    await expect(pageB.getByRole('textbox', { name: '描述', exact: true })).toHaveValue('')
    await expect(pageB.getByText('未保存', { exact: true })).toHaveCount(0)
  } finally {
    await context.close()
  }
})

test('keeps the DW-layer edit dialog and draft open after a version conflict', async ({ page }) => {
  const backend = await installMockBackend(page, {
    dwLayerConflict: true,
    permissions: ['model.dw_layer.read', 'model.dw_layer.update']
  })

  await page.goto('/dw-layers')
  const layerRow = page.getByRole('row').filter({ hasText: 'DWD' })
  await layerRow.getByRole('button', { name: '编辑', exact: true }).click()
  const editDialog = page.getByRole('dialog', { name: '编辑数仓分层' })
  const layerName = editDialog.getByRole('textbox', { name: '* 层级名称', exact: true })
  await layerName.fill('本地未保存分层名称')
  await editDialog.getByRole('button', { name: '保存', exact: true }).click()

  await expect(page.getByRole('alert').filter({
    hasText: '资源已被其他用户修改，当前未保存内容已保留。请确认后手动刷新，再重新提交。'
  })).toBeVisible()
  await expect(editDialog).toBeVisible()
  await expect(layerName).toHaveValue('本地未保存分层名称')
  expect(backend.getDWLayerUpdateVersions()).toEqual([1])
})

test('preserves Mermaid import text when the entity-model revision becomes stale', async ({ page }) => {
  const backend = await installMockBackend(page, {
    mermaidConflict: true,
    permissions: [
      ...DEFAULT_PERMISSIONS,
      'model.entity.create',
      'model.entity.delete',
      'model.entity_relation.read',
      'model.entity_relation.create',
      'model.entity_relation.delete'
    ]
  })

  await page.goto('/er-diagram')
  await page.getByRole('button', { name: '导入Mermaid', exact: true }).click()
  const importDialog = page.getByRole('dialog', { name: '导入Mermaid ER图' })
  const editor = importDialog.locator('textarea')
  await editor.fill(MERMAID_SNAPSHOT)
  await importDialog.getByRole('button', { name: '导入并全量替换', exact: true }).click()

  const confirmDialog = page.getByRole('dialog', { name: '确认导入' })
  await expect(confirmDialog).toContainText('导入将全量替换当前租户的草稿实体、属性和关系')
  await confirmDialog.getByRole('button', { name: '确定', exact: true }).click()

  await expect(page.getByRole('alert').filter({
    hasText: '导入失败：资源已被其他用户修改，当前未保存内容已保留。请确认后手动刷新，再重新提交。'
  })).toBeVisible()
  await expect(importDialog).toBeVisible()
  await expect(editor).toHaveValue(MERMAID_SNAPSHOT)
  expect(backend.getMermaidImports()).toEqual([{
    mermaid_code: MERMAID_SNAPSHOT,
    revision: 5
  }])
})

test.describe('DDL preview', () => {
  test.use({ viewport: { width: 620, height: 560 }, colorScheme: 'dark' })

  test('renders generated DDL in a themed dialog that stays inside a narrow viewport', async ({ page }) => {
    const backend = await installMockBackend(page, { theme: 'dark' })

    await page.goto('/logical-tables/2?domain_id=1')
    await expect(page.getByRole('button', { name: '预览 DDL', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '预览 DDL', exact: true }).click()

    const dialog = page.locator('.el-dialog.addp-dialog:visible')
    await expect(dialog).toHaveCount(1)
    await expect(dialog.getByText('CREATE TABLE "public"."dwd_province"', { exact: false })).toBeVisible()
    await expect(dialog.locator('.ddl-wrapper')).toHaveCSS('background-color', 'rgb(20, 20, 20)')
    await expectDialogWithinViewport(page, dialog)
    await expect.poll(() => backend.getDDLRequests()).toEqual([{
      materialization: {
        target_parent_locator: 'addp://engine/2/path/public?type=schema',
        target_name: 'dwd_province'
      }
    }])
  })
})

async function installMockBackend(target, options = {}) {
  let entityListRequests = 0
  let entity = structuredClone(ENTITIES[0])
  let logicalTable = structuredClone(LOGICAL_TABLE)
  let dwLayer = structuredClone(DW_LAYER)
  if (options.concurrentLogicalTable) logicalTable.status = 'draft'
  const ddlRequests = []
  const updateVersions = []
  const logicalTableUpdateVersions = []
  const dwLayerUpdateVersions = []
  const mermaidImports = []
  const permissions = options.permissions || DEFAULT_PERMISSIONS

  await target.addInitScript(({ theme }) => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', theme || 'light')
  }, { theme: options.theme })

  await target.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'model-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'model-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions }] }
      })
    }
    if (path === '/api/v1/standard/domains') return fulfillJSON(route, DOMAINS)
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: [], total: 0 })
    if (path === '/api/v1/model/entities/export-mermaid') {
      return fulfillJSON(route, { mermaid_code: MERMAID_SNAPSHOT, revision: 5 })
    }
    if (path === '/api/v1/model/entities/import-mermaid' && request.method() === 'POST') {
      mermaidImports.push(request.postDataJSON())
      if (options.mermaidConflict) {
        return fulfillJSON(route, {
          error: '资源版本冲突',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      return fulfillJSON(route, { created_entities: 1, created_relations: 0, revision: 6 })
    }
    if (path === '/api/v1/model/dw-layers' && request.method() === 'GET') {
      return fulfillJSON(route, [dwLayer])
    }
    if (path === '/api/v1/model/dw-layers/1' && request.method() === 'PUT') {
      const body = request.postDataJSON()
      dwLayerUpdateVersions.push(body.version)
      if (options.dwLayerConflict) {
        dwLayer = { ...dwLayer, layer_name: '明细层（他人已更新）', version: dwLayer.version + 1 }
        return fulfillJSON(route, {
          error: '资源版本冲突',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      dwLayer = { ...dwLayer, ...body, version: dwLayer.version + 1 }
      return fulfillJSON(route, dwLayer)
    }
    if (path === '/api/v1/model/entities/7/attributes') return fulfillJSON(route, [])
    if (path === '/api/v1/model/entities/7' && request.method() === 'GET') {
      return fulfillJSON(route, entity)
    }
    if (path === '/api/v1/model/entities/7' && request.method() === 'PUT' && options.concurrentEntity) {
      const body = request.postDataJSON()
      updateVersions.push(body.version)
      if (body.version !== entity.version) {
        return fulfillJSON(route, {
          error: '资源版本冲突',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      entity = { ...entity, ...body, version: entity.version + 1 }
      return fulfillJSON(route, entity)
    }
    if (path === '/api/v1/model/entity-relations') return fulfillJSON(route, [])
    if (path === '/api/v1/model/entities' && request.method() === 'GET') {
      entityListRequests += 1
      if (options.forbidEntityList) {
        return fulfillJSON(route, {
          error: '当前账号没有访问权限',
          error_code: 'permission_denied'
        }, 403)
      }
      const domainID = Number(url.searchParams.get('domain_id'))
      const entities = options.concurrentEntity ? [entity] : ENTITIES
      const data = domainID ? entities.filter(item => item.domain_id === domainID) : entities
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/model/logical-tables/2/fields') return fulfillJSON(route, [])
    if (path === '/api/v1/model/logical-tables/2/preview-ddl' && request.method() === 'POST') {
      ddlRequests.push(request.postDataJSON())
      return fulfillJSON(route, {
        ddl: 'CREATE TABLE "public"."dwd_province" (\n  "province" TEXT,\n  "code" TEXT,\n  PRIMARY KEY ("code")\n);'
      })
    }
    if (path === '/api/v1/model/logical-tables/2' && request.method() === 'PUT' && options.concurrentLogicalTable) {
      const body = request.postDataJSON()
      logicalTableUpdateVersions.push(body.version)
      if (body.version !== logicalTable.version) {
        return fulfillJSON(route, {
          error: '资源版本冲突',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      logicalTable = { ...logicalTable, ...body, version: logicalTable.version + 1 }
      return fulfillJSON(route, logicalTable)
    }
    if (path === '/api/v1/model/logical-tables/2' && request.method() === 'GET') {
      return fulfillJSON(route, logicalTable)
    }

    return fulfillJSON(route, {
      error: `Unexpected E2E request: ${request.method()} ${path}`
    }, 404)
  })

  return {
    getDDLRequests: () => structuredClone(ddlRequests),
    getEntity: () => structuredClone(entity),
    getEntityListRequests: () => entityListRequests,
    getDWLayerUpdateVersions: () => [...dwLayerUpdateVersions],
    getLogicalTableUpdateVersions: () => [...logicalTableUpdateVersions],
    getMermaidImports: () => structuredClone(mermaidImports),
    getUpdateVersions: () => [...updateVersions]
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}

async function expectDialogWithinViewport(page, dialog) {
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

  const box = await dialog.boundingBox()
  const viewport = page.viewportSize()
  expect(box).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(box.x).toBeGreaterThanOrEqual(12)
  expect(box.y).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width - 12)
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false)
}
