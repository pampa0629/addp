import { expect, test } from '@playwright/test'

const DOMAINS = [
  { id: 1, name: '客户域', code: 'customer' },
  { id: 2, name: '户外域', code: 'outdoor' }
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
  structural_constraints: { primary_key: [], required_fields: [], foreign_keys: [] },
  materialization: {
    target_parent_locator: 'addp://engine/2/path/public?type=schema&node_id=22',
    target_name: 'dwd_province'
  }
}

const OUTDOOR_CONCEPT_ENTITIES = [
  { id: 70, tenant_id: 1, domain_id: 2, name: '户外参与', code: 'outdoor_participation', status: 'approved', version: 3 },
  { id: 71, tenant_id: 1, domain_id: 2, name: '户外人员', code: 'outdoor_person', status: 'approved', version: 2 }
]

const OUTDOOR_CONCEPT_MAPPINGS = {
  version: 4,
  table_mappings: [{
    id: 101,
    table_id: 2,
    entity_id: 70,
    entity_name: '户外参与',
    entity_code: 'outdoor_participation',
    mapping_role: 'represents',
    entity_version: 3,
    current_entity_version: 3,
    in_sync: true
  }],
  field_mappings: [{
    id: 102,
    table_id: 2,
    field_id: 21,
    field_name: '参与人编号',
    field_column_name: 'person_id',
    entity_id: 70,
    entity_attribute_id: 701,
    entity_name: '户外参与',
    entity_code: 'outdoor_participation',
    entity_attribute_name: '参与人编号',
    attribute_column_name: 'person_id',
    mapping_role: 'direct'
  }],
  relation_mappings: [{
    id: 103,
    table_id: 2,
    table_relation_id: 201,
    entity_relation_id: 301,
    entity_relation_name: '参与人',
    source_entity_name: '户外人员',
    target_entity_name: '户外参与',
    orientation: 'inverse',
    entity_relation_version: 2,
    current_entity_relation_version: 2,
    in_sync: true
  }]
}

const DW_LAYER = {
  id: 1,
  tenant_id: 1,
  layer_code: 'dwd',
  layer_name: '明细层',
  description: '明细数据分层',
  naming_rule: 'dwd_{domain}_{entity}',
  sort_order: 2,
  version: 1
}

const DEFAULT_PERMISSIONS = [
  'model.entity.read',
  'model.logical_model.read'
]

const MERMAID_SNAPSHOT = `# ADDP Entity Relationship Diagram

\`\`\`mermaid
erDiagram
  %% addp:document {"format":"addp.model.er/v2","scope":"domain","domain_code":"outdoor"}
  %% addp:entity {"code":"outdoor","name":"活动","domain_code":"outdoor","description":"户外活动"}
  outdoor {
  }
\`\`\`
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

test('walks the outdoor conceptual model directly into its fact table without a second logical-table set', async ({ page }) => {
  const backend = await installMockBackend(page, {
    conceptMappings: true,
    permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update']
  })

  await page.goto('/logical-tables/2?domain_id=2&tab=concept-mappings')

  await expect(page.getByRole('tab', { name: '概念实现映射', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('维度表、事实表就是实体概念的实现，不需要再建立一套中间逻辑表。', { exact: false })).toBeVisible()
  const tableMappingCard = page.locator('.concept-mapping-editor .el-card').first()
  const fieldMappingCard = page.locator('.concept-mapping-editor .el-card').nth(1)
  const relationMappingCard = page.locator('.concept-mapping-editor .el-card').nth(2)
  await expect(tableMappingCard.getByText('户外参与 (outdoor_participation)', { exact: true })).toBeVisible()
  await expect(fieldMappingCard.getByText('户外参与.参与人编号 (person_id)', { exact: true })).toBeVisible()
  await expect(relationMappingCard.getByText('户外人员 → 户外参与 (参与人)', { exact: true })).toBeVisible()

  await tableMappingCard.locator('.el-select').nth(1).click()
  await page.getByRole('option', { name: '派生自实体', exact: true }).click()
  await page.locator('.concept-mapping-editor .mapping-toolbar').getByRole('button', { name: '保存', exact: true }).click()

  await expect(page.getByRole('alert').filter({ hasText: '保存成功' })).toBeVisible()
  await expect(page.getByText('逻辑表聚合版本：5', { exact: true })).toBeVisible()
  expect(backend.getConceptMappingWrites()).toEqual([{
    version: 4,
    table_mappings: [{ entity_id: 70, mapping_role: 'derives_from' }],
    field_mappings: [{ field_id: 21, entity_attribute_id: 701, mapping_role: 'direct' }],
    relation_mappings: [{ table_relation_id: 201, entity_relation_id: 301, orientation: 'inverse' }]
  }])
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
      'model.entity_relation.read',
      'model.entity_relation.create'
    ]
  })

  await page.goto('/er-diagram')
  await page.getByRole('button', { name: '导入 ER 文档', exact: true }).click()
  const importDialog = page.getByRole('dialog', { name: '导入 ER 文档（Mermaid Markdown）' })
  const editor = importDialog.locator('textarea')
  await editor.fill(MERMAID_SNAPSHOT)
  await importDialog.getByRole('button', { name: '预览导入', exact: true }).click()
  await expect(importDialog).toContainText('预计创建 1 个实体、0 个关系')
  await expect(importDialog).toContainText('户外域（outdoor）')
  await importDialog.getByRole('button', { name: '确认增量导入', exact: true }).click()

  await expect(page.getByRole('alert').filter({
    hasText: '导入失败：资源已被其他用户修改，当前未保存内容已保留。请确认后手动刷新，再重新提交。'
  })).toBeVisible()
  await expect(importDialog).toBeVisible()
  await expect(editor).toHaveValue(MERMAID_SNAPSHOT)
  expect(backend.getMermaidImports()).toEqual([{
    markdown: MERMAID_SNAPSHOT,
    revision: 5
  }])
})

test('reads a selected Mermaid Markdown file before previewing the incremental import', async ({ page }) => {
  await installMockBackend(page, {
    permissions: [
      ...DEFAULT_PERMISSIONS,
      'model.entity.create',
      'model.entity_relation.read',
      'model.entity_relation.create'
    ]
  })

  await page.goto('/er-diagram')
  await page.getByRole('button', { name: '导入 ER 文档', exact: true }).click()
  const importDialog = page.getByRole('dialog', { name: '导入 ER 文档（Mermaid Markdown）' })
  await importDialog.getByRole('tab', { name: '上传文件', exact: true }).click()
  await importDialog.locator('input[type="file"]').setInputFiles({
    name: 'er-diagram-outdoor.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from(MERMAID_SNAPSHOT)
  })

  const editor = importDialog.locator('textarea')
  await expect(editor).toHaveValue(MERMAID_SNAPSHOT)
  await importDialog.getByRole('button', { name: '预览导入', exact: true }).click()
  await expect(importDialog).toContainText('预计创建 1 个实体、0 个关系')
})

test('shows a composite primary key and outgoing references as read-only model constraints', async ({ page }) => {
  await installMockBackend(page)
  const primaryKey = [{ field_id: 21, column_name: 'person_id' }, { field_id: 22, column_name: 'activity_id' }]
  await page.route('**/api/v1/model/logical-tables/2', route => fulfillJSON(route, {
    ...LOGICAL_TABLE,
    structural_constraints: {
      primary_key: primaryKey,
      required_fields: [...primaryKey, { field_id: 23, column_name: 'member_status' }],
      foreign_keys: [{ id: 1, source_field_code: 'person_id', target_table_code: 'dim_outdoor_person', target_field_code: 'person_id' }]
    }
  }))
  await page.goto('/logical-tables/2')
  const constraints = page.locator('.el-card').filter({ has: page.getByText('结构约束', { exact: true }) })
  await expect(constraints.getByText('person_id + activity_id', { exact: true })).toBeVisible()
  await expect(constraints.getByText('person_id, activity_id, member_status', { exact: true })).toBeVisible()
  await expect(constraints.getByText('dim_outdoor_person', { exact: true })).toBeVisible()
  await expect(constraints.getByRole('button')).toHaveCount(0)
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
})

test.describe('DDL preview', () => {
  test.use({ viewport: { width: 620, height: 560 }, colorScheme: 'dark' })

  test('renders generated DDL in a themed dialog that stays inside a narrow viewport', async ({ page }) => {
    const backend = await installMockBackend(page, { theme: 'dark' })

    await page.goto('/logical-tables/2?domain_id=1')
    await page.getByRole('tab', { name: '物理目标', exact: true }).click()
    await expect(page.getByRole('button', { name: '预览建表语句', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '预览建表语句', exact: true }).click()

    const dialog = page.locator('.el-dialog.addp-dialog:visible')
    await expect(dialog).toHaveCount(1)
    await expect(dialog.getByText('CREATE TABLE "public"."dwd_province"', { exact: false })).toBeVisible()
    await expect(dialog.locator('.ddl-wrapper')).toHaveCSS('background-color', 'rgb(20, 20, 20)')
    await expectDialogWithinViewport(page, dialog)
    await expect.poll(() => backend.getDDLRequests()).toEqual([{
      materialization: {
        target_parent_locator: 'addp://engine/2/path/public?type=schema&node_id=22',
        target_name: 'dwd_province'
      }
    }])
  })
})

for (const draftTable of [false, true]) {
  test(`read-only ${draftTable ? 'draft' : 'approved'} detail stays clean through preview and refresh`, async ({ page }) => {
    const backend = await installMockBackend(page, { draftTable })
    await page.goto('/logical-tables/2')
    await expect(page.getByRole('textbox', { name: '逻辑表名', exact: true })).toBeDisabled()
    await page.getByRole('tab', { name: '物理目标', exact: true }).click()
    await expect(page.locator('.resource-tree-picker')).toHaveCount(0)
    await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: '预览建表语句', exact: true }).click()
    await expect(page.locator('.ddl-wrapper')).toBeVisible()
    expect(backend.getDDLRequests()[0].materialization.target_parent_locator).toBe(LOGICAL_TABLE.materialization.target_parent_locator)
    await page.locator('.el-dialog:visible .el-dialog__headerbtn').click()
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByRole('dialog', { name: '存在未保存内容' })).toHaveCount(0)
    await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  })
}

for (const missingTarget of [false, true]) {
  test(`draft target restoration preserves the saved locator when lookup ${missingTarget ? 'is empty' : 'adds a node ID'}`, async ({ page }) => {
    const backend = await installMockBackend(page, { draftTable: true, withoutNodeID: true, missingTarget, permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update'] })
    const restored = page.waitForResponse(response => response.url().includes('/resource-tree/2/ancestors'))
    await page.goto('/logical-tables/2')
    await page.getByRole('tab', { name: '物理目标', exact: true }).click()
    await restored
    await page.getByRole('button', { name: '预览建表语句', exact: true }).click()
    await expect(page.locator('.ddl-wrapper')).toBeVisible()
    expect(backend.getDDLRequests()[0].materialization.target_parent_locator).toBe('addp://engine/2/path/public?type=schema')
    await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  })
}

test('dialog baselines track real edits and preserve the page draft when discarded', async ({ page }) => {
  await installMockBackend(page, { draftTable: true, lifecycle: true, permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update', 'model.logical_model.create'] })
  await page.goto('/logical-tables/2')
  await page.getByRole('tab', { name: '物理目标', exact: true }).click()
  await expect(page.locator('.resource-tree-picker')).toBeVisible()
  await page.getByRole('tab', { name: '模型定义', exact: true }).click()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await page.getByRole('row').filter({ hasText: '编号' }).getByRole('button', { name: '编辑', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '编辑字段' })
  await expect(dialog).toBeVisible()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await dialog.getByRole('textbox', { name: '字段显示名', exact: false }).first().fill('修改编号')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await dialog.getByRole('textbox', { name: '字段显示名', exact: false }).first().fill('编号')
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('textbox', { name: '逻辑表名', exact: true }).fill('本地草稿')
  await page.getByRole('row').filter({ hasText: '编号' }).getByRole('button', { name: '编辑', exact: true }).click()
  await dialog.getByRole('textbox', { name: '字段显示名', exact: false }).first().fill('修改编号')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  const discard = page.getByRole('dialog', { name: '存在未保存内容' })
  await expect(discard).toBeVisible()
  await discard.getByRole('button', { name: '放弃并继续', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('textbox', { name: '逻辑表名', exact: true })).toHaveValue('本地草稿')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await page.getByRole('row').filter({ hasText: '编号' }).getByRole('button', { name: '编辑', exact: true }).click()
  await dialog.getByRole('textbox', { name: '字段显示名', exact: false }).fill('已保存编号')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: '已保存编号' })).toBeVisible()
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await expect(page.getByRole('textbox', { name: '逻辑表名', exact: true })).toHaveValue('本地草稿')

})

test('approval and reopening refresh frozen field revisions without creating a draft', async ({ page }) => {
  await installMockBackend(page, { draftTable: true, lifecycle: true, permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update'] })
  await page.goto('/logical-tables/2')
  await page.getByRole('button', { name: '审批通过', exact: true }).click()
  await expect(page.getByText('冻结修订 #5102', { exact: true })).toBeVisible()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await page.locator('.detail-header').getByRole('button', { name: '退回草稿', exact: true }).click()
  await page.getByRole('dialog', { name: '退回草稿', exact: true }).getByRole('button', { name: '退回草稿', exact: true }).click()
  await expect(page.getByText('冻结修订 #5102', { exact: true })).toHaveCount(0)
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
})

test('ER entry starts scoped and restores domain and related edges from the URL', async ({ page }) => {
  const backend = await installMockBackend(page, { permissions: [...DEFAULT_PERMISSIONS, 'model.entity_relation.read'] })
  await page.goto('/er-diagram')
  await expect(page.getByText('请先选择业务域，或主动选择全部业务域查看总览')).toBeVisible()
  await expect(page.locator('.diagram-container')).toHaveCount(0)
  await page.goto('/er-diagram?domain_id=2&related=1')
  await expect(page.getByRole('checkbox', { name: '展开跨域关联' })).toBeChecked()
  const domainDownload = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出 ER 文档', exact: true }).click()
  expect((await domainDownload).suggestedFilename()).toBe('er-diagram-outdoor.md')
  expect(backend.getMermaidExports()).toEqual([{ domain_id: '2' }])
  await page.reload()
  await expect(page.getByRole('checkbox', { name: '展开跨域关联' })).toBeChecked()
  await page.getByText('展开跨域关联', { exact: true }).click()
  await expect(page).toHaveURL(/er-diagram\?domain_id=2$/)
  await page.goto('/er-diagram?domain_id=all')
  await expect(page.getByRole('checkbox', { name: '展开跨域关联' })).toHaveCount(0)
  await expect(page.locator('.diagram-container')).toBeVisible()
  await expect(page.getByText('导出遵循当前业务域选择；“展开跨域关联”仅影响画布。导入按当前租户精确解析业务域与数据元编码，先预览且只增量创建缺失项，不删除或覆盖现有实体与关系。')).toBeVisible()
})

test('entity list opens the ER diagram in its current business domain', async ({ page }) => {
  await installMockBackend(page, { permissions: [...DEFAULT_PERMISSIONS, 'model.entity_relation.read'] })
  await page.goto('/entities?domain_id=2')
  await page.getByRole('button', { name: '实体关系图（ER图）', exact: true }).click()
  await expect(page).toHaveURL(/er-diagram\?domain_id=2$/)
  await expect(page.locator('.domain-filter')).toContainText('户外域')
})

test('dimensional modeling filters facts by domain and restores selection with cross-domain dimensions', async ({ page }) => {
  const requests = await installDimensionalModelBackend(page)
  await page.goto('/star-schema?domain_id=2&table_id=3')
  await expect(page.locator('.view-title')).toHaveText('维度建模')
  await expect(page.locator('.owner-domain')).toHaveText('户外域')
  await expect(page.locator('.fact-item')).toHaveCount(2)
  await expect(page.locator('.dim-item')).toContainText('省份')
  await expect(page.locator('.mermaid-container svg')).toContainText('省份')
  await expect(page.getByText('模型关系图', { exact: true })).toBeVisible()
  expect(requests.filter(query => query.table_type === 'dimension').every(query => !query.domain_id)).toBe(true)

  await page.reload()
  await expect(page.locator('.fact-item.active')).toContainText('活动参与事实')
  await expect(page.locator('.owner-domain')).toHaveText('户外域')

  await selectModelDomain(page, '客户域')
  await expect(page).toHaveURL(/\/star-schema\?domain_id=1$/)
  await expect(page.locator('.fact-item')).toHaveCount(1)
  await expect(page.locator('.fact-item')).toContainText('订单明细表')
  await expect(page.locator('.fact-detail-card')).toHaveCount(0)
  await page.locator('.fact-item').click()
  await expect(page).toHaveURL(/domain_id=1&table_id=1$/)
  await expect(page.locator('.owner-domain')).toHaveText('客户域')

  await page.goBack()
  await expect(page.locator('.fact-detail-card')).toHaveCount(0)
  await page.goBack()
  await expect(page.locator('.fact-item.active')).toContainText('活动参与事实')
  await expect(page.locator('.dim-item')).toContainText('省份')
  await page.goForward()
  await expect(page.locator('.fact-item')).toHaveCount(1)
  await expect(page.locator('.fact-detail-card')).toHaveCount(0)

  await selectModelDomain(page, '全部业务域')
  await expect(page).toHaveURL(/\/star-schema$/)
  await expect(page.locator('.fact-item')).toHaveCount(4)
  await page.locator('.fact-item').filter({ hasText: '未归属事实' }).click()
  await expect(page.locator('.owner-domain')).toHaveText('未归属业务域')
})

test('dimensional modeling ignores a stale domain response and handles a domain with no facts', async ({ page }) => {
  await installDimensionalModelBackend(page)
  let releaseCustomer
  const customerResponse = new Promise(resolve => { releaseCustomer = resolve })
  let customerRequested = false
  await page.route('**/api/v1/model/logical-tables?**', async route => {
    const url = new URL(route.request().url())
    if (url.searchParams.get('domain_id') !== '1') return route.fallback()
    customerRequested = true
    await customerResponse
    return route.fallback()
  })
  await page.goto('/star-schema?domain_id=2&table_id=3')
  await expect(page.locator('.owner-domain')).toHaveText('户外域')
  await selectModelDomain(page, '客户域')
  await expect.poll(() => customerRequested).toBe(true)
  await selectModelDomain(page, '户外域')
  await expect(page.locator('.fact-item')).toHaveCount(2)
  const lateResponse = page.waitForResponse(response => new URL(response.url()).searchParams.get('domain_id') === '1')
  releaseCustomer()
  await lateResponse
  await expect(page.locator('.fact-item')).toHaveCount(2)
  await expect(page.locator('.fact-item').filter({ hasText: '订单明细表' })).toHaveCount(0)

  await selectModelDomain(page, '空业务域')
  await expect(page).toHaveURL(/domain_id=10$/)
  await expect(page.getByText('暂无事实表', { exact: true })).toBeVisible()
  await expect(page.locator('.fact-detail-card')).toHaveCount(0)
})

test('dimensional modeling uses English labels and fits the domain selector in a narrow viewport', async ({ page }) => {
  await installDimensionalModelBackend(page)
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'en'))
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/star-schema?domain_id=2&table_id=3')
  await expect(page.locator('.view-title')).toHaveText('Dimensional Modeling')
  await expect(page.getByText('Model Relationship Diagram', { exact: true })).toBeVisible()
  const bounds = await page.locator('.domain-filter').boundingBox()
  expect(bounds.x).toBeGreaterThanOrEqual(0)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
})

async function selectModelDomain(page, name) {
  await page.locator('.domain-filter .el-select').click()
  await page.getByRole('option', { name, exact: true }).click()
}

async function installDimensionalModelBackend(page) {
  await installMockBackend(page)
  const facts = [
    { id: 1, name: '订单明细表', code: 'dwd_order_detail', domain_id: 1 },
    { id: 3, name: '活动参与事实', code: 'dwd_outdoor_participation', domain_id: 2 },
    { id: 6, name: '人员指标汇总', code: 'dws_outdoor_person_metric', domain_id: 2 },
    { id: 9, name: '未归属事实', code: 'dwd_unassigned', domain_id: null }
  ].map(table => ({ ...table, table_type: 'fact', layer: 'dwd', status: 'approved', version: 1 }))
  const requests = []
  await page.route('**/api/v1/standard/domains', route => fulfillJSON(route, [...DOMAINS, { id: 10, name: '空业务域' }]))
  await page.route('**/api/v1/model/logical-tables**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/model/logical-tables') {
      const query = Object.fromEntries(url.searchParams)
      requests.push(query)
      const data = query.table_type === 'dimension' ? [LOGICAL_TABLE] :
        facts.filter(table => !query.domain_id || String(table.domain_id) === query.domain_id)
      return fulfillJSON(route, { data, total: data.length })
    }
    if (url.pathname === '/api/v1/model/logical-tables/3/dimension-relations') {
      return fulfillJSON(route, [{ id: 1, source_field: 31, source_field_name: '省份编码', target_table: 2,
        target_table_name: '省份', target_field: 21, target_field_name: '省份编码', relation_type: 'fk' }])
    }
    if (/\/(fields|dimension-relations|metric-implementations)$/.test(url.pathname)) return fulfillJSON(route, [])
    return route.fallback()
  })
  return requests
}

const REVISIONED_ELEMENTS = [{
  id: 51, code: 'person_id', lifecycle_state: 'active',
  current_revision: { id: 5103, name: '当前人员标识', status: 'published', data_type: 'string' },
  draft_revision: { id: 5104, name: '尚未发布的人员标识', status: 'draft', data_type: 'int' }
}, {
  id: 52, code: 'draft_only', lifecycle_state: 'active',
  draft_revision: { id: 5201, name: '仅草稿数据元', status: 'draft', data_type: 'int' }
}]

test('logical field element selection uses the current published revision and clears stale length', async ({ page }) => {
  await installMockBackend(page, {
    draftTable: true, lifecycle: true, elements: REVISIONED_ELEMENTS,
    permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update', 'model.logical_model.create']
  })
  await page.goto('/logical-tables/2')
  await expect(page.getByRole('cell', { name: '当前人员标识', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '添加字段', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '添加字段' })
  await dialog.getByRole('spinbutton').fill('32')
  const elementSelect = dialog.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '关联数据元', exact: true }) })
  await elementSelect.click()
  await expect(page.getByRole('option', { name: '当前人员标识 (person_id)', exact: true })).toBeVisible()
  await expect(page.getByRole('option').filter({ hasText: /undefined|尚未发布|仅草稿/ })).toHaveCount(0)
  await page.getByRole('option', { name: '当前人员标识 (person_id)', exact: true }).click()
  await expect(dialog.getByRole('textbox', { name: '字段显示名', exact: false })).toHaveValue('当前人员标识')
  await expect(dialog.locator('.el-select').filter({ has: page.getByRole('combobox', { name: '数据类型', exact: false }) })).toContainText('string')
  await expect(dialog.getByRole('spinbutton')).toHaveValue('')
})

for (const kind of ['logical-table', 'entity']) {
  test(`${kind} displays the exact frozen element name even after withdrawal and replacement`, async ({ page }) => {
    await installMockBackend(page, {
      lifecycle: true, elements: REVISIONED_ELEMENTS,
      attributes: [{ id: 71, name: '人员编号', column_name: 'person_id', data_type: 'string', element_id: 51, element_revision_id: 5102 }],
      approvedEntity: true
    })
    await page.goto(kind === 'logical-table' ? '/logical-tables/2' : '/entities/7?tab=attributes')
    await expect(page.getByRole('cell').filter({ hasText: '审批时的数据元名称' })).toBeVisible()
    await expect(page.getByRole('cell').filter({ hasText: '当前人员标识' })).toHaveCount(0)
    await expect(page.getByText('冻结修订 #5102', { exact: true })).toBeVisible()
  })
}

for (const kind of ['logical-table', 'entity']) {
  test(`${kind} opens its frozen standard revision through Console`, async ({ page }) => {
    await installMockBackend(page, {
      lifecycle: true, elements: REVISIONED_ELEMENTS, approvedEntity: true,
      attributes: [{ id: 71, name: '人员编号', column_name: 'person_id', data_type: 'string', element_id: 51, element_revision_id: 5102 }],
      permissions: [...DEFAULT_PERMISSIONS, 'standard.element.read']
    })
    await page.route('**/standard/elements/51?revision_id=5102', route => route.fulfill({ contentType: 'text/html', body: '<h1>Frozen revision destination</h1>' }))
    await page.goto(kind === 'logical-table' ? '/logical-tables/2' : '/entities/7?tab=attributes')
    await page.getByRole('button', { name: '冻结修订 #5102', exact: true }).click()
    await expect(page).toHaveURL(/\/standard\/elements\/51\?revision_id=5102$/)
  })
}

test('frozen standard navigation is disabled without element read permission', async ({ page }) => {
  await installMockBackend(page, { lifecycle: true, elements: REVISIONED_ELEMENTS })
  await page.goto('/logical-tables/2')
  await expect(page.getByRole('button', { name: '冻结修订 #5102', exact: true })).toBeDisabled()
})

test('failed frozen element lookup is explicit and never displays the current name', async ({ page }) => {
  await installMockBackend(page, { lifecycle: true, elements: REVISIONED_ELEMENTS, forbidElementRevision: true })
  await page.goto('/logical-tables/2')
  await expect(page.getByRole('cell').filter({ hasText: '数据元名称暂不可用' })).toBeVisible()
  await expect(page.getByRole('cell').filter({ hasText: '当前人员标识' })).toHaveCount(0)
  await expect(page.getByRole('alert').filter({ hasText: '部分标准引用数据暂不可用' })).toBeVisible()
})

test('entity approval and reopening reload attribute revision names immediately', async ({ page }) => {
  await installMockBackend(page, {
    elements: REVISIONED_ELEMENTS, entityElementLifecycle: true,
    permissions: [...DEFAULT_PERMISSIONS, 'model.entity.approve', 'model.entity.update']
  })
  await page.goto('/entities/7?tab=attributes')
  await expect(page.getByRole('cell', { name: '当前人员标识', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '审批通过', exact: true }).click()
  await expect(page.getByRole('cell').filter({ hasText: '审批时的数据元名称' })).toBeVisible()
  await expect(page.getByText('冻结修订 #5102', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '退回草稿', exact: true }).click()
  await page.getByRole('dialog', { name: '退回草稿', exact: true }).getByRole('button', { name: '退回草稿', exact: true }).click()
  await expect(page.getByRole('cell', { name: '当前人员标识', exact: true })).toBeVisible()
  await expect(page.getByText('冻结修订 #5102', { exact: true })).toHaveCount(0)
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
})

async function installMockBackend(target, options = {}) {
  let entityListRequests = 0
  let entity = structuredClone(ENTITIES[0])
  if (options.approvedEntity) entity.status = 'approved'
  let logicalTable = structuredClone(LOGICAL_TABLE)
  let conceptMappings = structuredClone(OUTDOOR_CONCEPT_MAPPINGS)
  let reopenRequests = 0
  let dwLayer = structuredClone(DW_LAYER)
  if (options.concurrentLogicalTable || options.draftTable) logicalTable.status = 'draft'
  if (options.lifecycle) logicalTable.table_type = 'fact'
  if (options.withoutNodeID) logicalTable.materialization.target_parent_locator = 'addp://engine/2/path/public?type=schema'
  let fieldName = "编号"
  const ddlRequests = []
  const updateVersions = []
  const logicalTableUpdateVersions = []
  const dwLayerUpdateVersions = []
  const mermaidImports = []
  const mermaidExports = []
  const conceptMappingWrites = []
  const permissions = options.permissions || DEFAULT_PERMISSIONS

  if (options.conceptMappings) {
    logicalTable = {
      ...logicalTable,
      domain_id: 2,
      name: '户外参与事实',
      code: 'dwd_outdoor_participation',
      table_type: 'fact',
      layer: 'dwd',
      status: 'draft',
      grain_description: '每次人员参与户外活动一行',
      version: conceptMappings.version
    }
  }

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
    const schema = { id: 'schema-22', label: 'public', type: 'schema', locator: 'addp://engine/2/path/public?type=schema&node_id=22', children: [], metadata: { node_id: 22, engine_id: 2 } }
    const root = { id: 'engine-2', label: '业务库', type: 'database', locator: 'addp://engine/2/path/?type=database&node_id=21', children: [schema], metadata: { node_id: 21, engine_id: 2 } }
    if (path === '/api/v1/meta/engines') return fulfillJSON(route, [{
      id: 2, name: '业务库', engine_type: 'postgresql', engine_family: 'tabular',
      catalog_top_term: 'schema', engine_catalog_leaf_term: 'table', status: 'online'
    }])
    if (path === '/api/v1/meta/resource-tree/2') return fulfillJSON(route, root)
    if (path === '/api/v1/meta/resource-tree/2/ancestors') return fulfillJSON(route, { ancestors: options.missingTarget ? [] : [root, schema] })
    if (path === '/api/v1/meta/resource-tree/2/node') return fulfillJSON(route, schema)
    if (path === '/api/v1/standard/metrics') return fulfillJSON(route, { data: [], total: 0 })
    if (path === '/api/v1/model/metric-implementations') return fulfillJSON(route, [])
    if (path === '/api/v1/model/logical-tables/2/approve' || path === '/api/v1/model/logical-tables/2/reopen') {
      if (path.endsWith('/reopen')) reopenRequests += 1
      logicalTable.status = path.endsWith('/approve') ? 'approved' : 'draft'
      logicalTable.version += 1
      return fulfillJSON(route, logicalTable)
    }
    if (path === '/api/v1/model/logical-tables') return fulfillJSON(route, { data: [logicalTable], total: 1 })
    if (path === '/api/v1/standard/domains') return fulfillJSON(route, DOMAINS)
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: options.elements || [], total: options.elements?.length || 0 })
    if (path === '/api/v1/standard/elements/51/revisions/5102') {
      if (options.forbidElementRevision) return fulfillJSON(route, { error: '无权读取修订', error_code: 'permission_denied' }, 403)
      return fulfillJSON(route, { id: 5102, element_id: 51, name: '审批时的数据元名称', data_type: 'string', status: 'withdrawn' })
    }
    if (path === '/api/v1/model/entities/export-mermaid') {
      mermaidExports.push(Object.fromEntries(url.searchParams))
      return fulfillJSON(route, { markdown: MERMAID_SNAPSHOT, scope: url.searchParams.has('domain_id') ? 'domain' : 'all', domain_code: url.searchParams.has('domain_id') ? 'outdoor' : undefined })
    }
    if (path === '/api/v1/model/entities/import-mermaid/preview' && request.method() === 'POST') {
      return fulfillJSON(route, {
        revision: 5,
        scope: 'domain',
        domain_code: 'outdoor',
        resolved_domains: [{ code: 'outdoor', name: '户外域' }],
        created_entities: 1,
        unchanged_entities: 0,
        created_relations: 0,
        unchanged_relations: 0,
        conflicts: []
      })
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
    if (path === '/api/v1/model/entities/70/attributes' && options.conceptMappings) {
      return fulfillJSON(route, [{ id: 701, entity_id: 70, name: '参与人编号', column_name: 'person_id', data_type: 'string' }])
    }
    if (path === '/api/v1/model/entities/7/approve' || path === '/api/v1/model/entities/7/reopen') {
      entity.status = path.endsWith('/approve') ? 'approved' : 'draft'
      entity.version += 1
      return fulfillJSON(route, entity)
    }
    if (path === '/api/v1/model/entities/7/attributes') return fulfillJSON(route, options.entityElementLifecycle ? [{
      id: 71, name: '人员编号', column_name: 'person_id', data_type: 'string', element_id: 51,
      element_revision_id: entity.status === 'approved' ? 5102 : null
    }] : options.attributes || [])
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
    if (path === '/api/v1/model/entity-relations' && options.conceptMappings) {
      return fulfillJSON(route, [{ id: 301, source_entity: 71, target_entity: 70, name: '参与人', relation_type: 'one_to_many', version: 2 }])
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
      const entities = options.conceptMappings ? OUTDOOR_CONCEPT_ENTITIES : options.concurrentEntity ? [entity] : ENTITIES
      const data = domainID ? entities.filter(item => item.domain_id === domainID) : entities
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/model/logical-tables/2/fields/21' && request.method() === 'PUT') {
      fieldName = request.postDataJSON().name
      logicalTable.version += 1
      return fulfillJSON(route, { field: { id: 21, name: fieldName }, version: logicalTable.version })
    }
    if (path === '/api/v1/model/logical-tables/2/fields' && options.conceptMappings) {
      return fulfillJSON(route, [{
        id: 21, table_id: 2, name: '参与人编号', column_name: 'person_id', data_type: 'string', is_pk: false,
        nullable: false, field_role: 'dimension_fk'
      }])
    }
    if (path === '/api/v1/model/logical-tables/2/fields') return fulfillJSON(route, options.lifecycle ? [{
      id: 21, table_id: 2, name: fieldName, column_name: 'code', data_type: 'string', is_pk: true, nullable: false, field_role: 'regular',
      element_id: 51, element_revision_id: logicalTable.status === 'approved' ? 5102 : null
    }] : [])
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
    if (path === '/api/v1/model/logical-tables/2/dimension-relations' && options.conceptMappings) {
      return fulfillJSON(route, [{
        id: 201,
        source_table: 2,
        source_field: 21,
        source_field_name: '参与人编号',
        source_field_code: 'person_id',
        target_table: 3,
        target_table_name: '户外人员维度',
        target_field: 31,
        target_field_name: '人员编号',
        target_field_code: 'person_id',
        relation_type: 'fk'
      }])
    }
    if (path === '/api/v1/model/logical-tables/2/concept-mappings' && request.method() === 'GET' && options.conceptMappings) {
      return fulfillJSON(route, conceptMappings)
    }
    if (path === '/api/v1/model/logical-tables/2/concept-mappings' && request.method() === 'PUT' && options.conceptMappings) {
      const body = request.postDataJSON()
      conceptMappingWrites.push(body)
      conceptMappings = {
        ...conceptMappings,
        version: conceptMappings.version + 1,
        table_mappings: conceptMappings.table_mappings.map(mapping => ({ ...mapping, mapping_role: body.table_mappings[0].mapping_role }))
      }
      logicalTable.version = conceptMappings.version
      return fulfillJSON(route, conceptMappings)
    }

    return fulfillJSON(route, {
      error: `Unexpected E2E request: ${request.method()} ${path}`
    }, 404)
  })

  return {
    getReopenRequests: () => reopenRequests,
    getDDLRequests: () => structuredClone(ddlRequests),
    getEntity: () => structuredClone(entity),
    getEntityListRequests: () => entityListRequests,
    getDWLayerUpdateVersions: () => [...dwLayerUpdateVersions],
    getLogicalTableUpdateVersions: () => [...logicalTableUpdateVersions],
    getMermaidImports: () => structuredClone(mermaidImports),
    getMermaidExports: () => structuredClone(mermaidExports),
    getConceptMappingWrites: () => structuredClone(conceptMappingWrites),
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

test('approved table queues materialization and links only its task executions without becoming dirty', async ({ page }) => {
 await installMockBackend(page,{permissions:[...DEFAULT_PERMISSIONS,'model.materialization.execute']})
 let calls=0
 await page.route('**/api/v1/model/logical-tables/2/materialized-target',async route=>{calls++;expect(route.request().postDataJSON()).toEqual({version:1});await new Promise(r=>setTimeout(r,150));return fulfillJSON(route,{execution_id:'11111111-1111-4111-8111-111111111111',status:'pending'})})
 await page.goto('/logical-tables/2')
 await page.getByRole('tab', { name: '物理目标', exact: true }).click()
 await expect(page.getByText('业务库', { exact: false })).toBeVisible()
 await expect(page.getByText('(postgresql)', { exact: true })).toBeVisible()
 await page.getByRole('button',{name:'创建/校验目标表',exact:true}).click()
 await expect(page.getByRole('alert').filter({hasText:'建表校验任务已提交'})).toBeVisible()
 expect(calls).toBe(1)
 await expect(page.getByText('未保存',{exact:true})).toHaveCount(0)
 await page.context().route('**/monitor/executions?**', route => route.fulfill({ status: 200, contentType: 'text/html', body: '<p>Execution monitor</p>' }))
 const popupPromise = page.waitForEvent('popup')
 await page.getByRole('button', { name: '在统一监控中查看', exact: true }).click()
 const popup = await popupPromise
 await popup.waitForLoadState()
 const monitorURL = new URL(popup.url())
 expect(monitorURL.pathname).toBe('/monitor/executions')
 expect(Object.fromEntries(monitorURL.searchParams)).toEqual({ module: 'model', task_type: 'logical_table_materialization', source_task_id: '2' })
 await popup.close()
})

async function installRelationBackend(page, { draft = false, conflict = false } = {}) {
  await installMockBackend(page, { permissions: [...DEFAULT_PERMISSIONS, 'model.logical_model.update'] })
  const fact = { id: 3, name: '活动参与事实', code: 'dwd_outdoor_participation', table_type: 'fact', domain_id: 2, layer: 'dwd', status: draft ? 'draft' : 'approved', version: 10, materialization: {}, grain_description: '每次参与一行' }
  const dimension = structuredClone(LOGICAL_TABLE)
  const sourceFields = [{ id: 31, table_id: 3, name: '省份编码', column_name: 'province_code', data_type: 'string', field_role: 'dimension_fk' }]
  const targetFields = [
    { id: 21, table_id: 2, name: '省份编码', column_name: 'code', data_type: 'string', is_pk: true, element_id: 51, element_revision_id: 5102 },
    { id: 22, table_id: 2, name: '省份简称', column_name: 'short_code', data_type: 'string', is_pk: false }
  ]
  let relations = [{ id: 7, source_table: 3, source_field: 31, target_table: 2, target_field: 21, relation_type: 'fk' }]
  const writes = []
  const enriched = () => relations.map(relation => ({ ...relation, source_table_name: fact.name, source_table_code: fact.code,
    source_field_name: sourceFields[0].name, source_field_code: sourceFields[0].column_name,
    target_table_name: dimension.name, target_table_code: dimension.code,
    target_field_name: targetFields.find(field => field.id === relation.target_field).name,
    target_field_code: targetFields.find(field => field.id === relation.target_field).column_name }))
  await page.route('**/api/v1/model/logical-tables**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === '/api/v1/model/logical-tables') {
      const data = url.searchParams.get('table_type') === 'dimension' ? [dimension] : [fact]
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/model/logical-tables/3') return fulfillJSON(route, fact)
    if (path === '/api/v1/model/logical-tables/2') return fulfillJSON(route, dimension)
    if (/\/3\/(reopen|approve)$/.test(path)) {
      writes.push({ path, method: request.method(), body: request.postDataJSON() })
      fact.status = path.endsWith('reopen') ? 'draft' : 'approved'
      fact.version += 1
      return fulfillJSON(route, fact)
    }
    if (path.endsWith('/3/fields')) return fulfillJSON(route, sourceFields)
    if (path.endsWith('/2/fields')) return fulfillJSON(route, targetFields)
    if (/\/(metric-implementations|dimension-hierarchies)$/.test(path)) return fulfillJSON(route, [])
    if (/\/dimension-relations(?:\/\d+)?$/.test(path)) {
      if (request.method() === 'GET') return fulfillJSON(route, enriched())
      const body = request.postDataJSON()
      writes.push({ path, method: request.method(), body })
      if (conflict || body.version !== fact.version) return fulfillJSON(route, { error: '资源版本冲突', error_code: 'resource_version_conflict' }, 409)
      fact.version += 1
      if (request.method() === 'DELETE') {
        relations = []
        return fulfillJSON(route, { version: fact.version })
      }
      const relation = { ...body, id: request.method() === 'POST' ? 8 : 7, source_table: fact.id }
      relations = [relation]
      return fulfillJSON(route, { relation, version: fact.version }, request.method() === 'POST' ? 201 : 200)
    }
    return route.fallback()
  })
  return { writes, fact, dimension }
}

test('relation details navigate to the source mapping and incoming references return to the same relation', async ({ page }) => {
  await installRelationBackend(page)
  await page.goto('/star-schema?domain_id=2&table_id=3')
  await page.getByRole('button', { name: '查看关联', exact: true }).click()
  await expect(page).toHaveURL(/\/logical-tables\/3\?domain_id=2&tab=relations&relation_id=7$/)
  const card = page.locator('[data-relation-id="7"]')
  await expect(card).toHaveClass(/selected/)
  await expect(card.locator('code')).toHaveText('dwd_outdoor_participation.province_code = dwd_province.code')
  await expect(page.getByText('修改前请在顶部将该事实表退回草稿', { exact: false })).toBeVisible()
  await expect(card.getByRole('button', { name: '编辑', exact: true })).toHaveCount(0)
  await page.reload()
  await expect(card).toHaveClass(/selected/)
  await card.getByRole('button', { name: '查看维度表', exact: true }).click()
  await expect(page).toHaveURL(/\/logical-tables\/2$/)
  await page.getByRole('tab', { name: '被引用关系', exact: true }).click()
  await expect(page).toHaveURL(/\/logical-tables\/2\?tab=relations$/)
  await expect(card.locator('code')).toHaveText('dwd_outdoor_participation.province_code = dwd_province.code')
  await page.getByRole('button', { name: '查看事实表关联', exact: true }).click()
  await expect(page).toHaveURL(/\/logical-tables\/3\?tab=relations&relation_id=7$/)
  await expect(card).toHaveClass(/selected/)
  await page.goBack()
  await expect(page.getByRole('tab', { name: '被引用关系', exact: true })).toHaveAttribute('aria-selected', 'true')
})

test('draft facts edit, remove and add references to an approved dimension without reopening it', async ({ page }) => {
  const backend = await installRelationBackend(page, { draft: true })
  await page.goto('/logical-tables/3?tab=relations&relation_id=7')
  await page.locator('[data-relation-id="7"]').getByRole('button', { name: '编辑', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '编辑维度关联', exact: true })
  await dialog.locator('label.el-radio').filter({ hasText: 'JOIN（字段等值关联）' }).click()
  await dialog.locator('.el-form-item').filter({ hasText: '目标字段' }).locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: '省份简称（short_code）', exact: true }).click()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.locator('[data-relation-id="7"] code')).toHaveText('dwd_outdoor_participation.province_code = dwd_province.short_code')
  expect(backend.writes[0]).toMatchObject({ method: 'PUT', path: '/api/v1/model/logical-tables/3/dimension-relations/7', body: { version: 10, target_table: 2, target_field: 22, relation_type: 'join' } })
  await page.locator('[data-relation-id="7"]').getByRole('button', { name: '删除', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.locator('[data-relation-id]')).toHaveCount(0)
  await expect(page).toHaveURL(/\/logical-tables\/3\?tab=relations$/)
  await page.getByRole('button', { name: '添加维度关联', exact: true }).click()
  const createDialog = page.getByRole('dialog', { name: '添加维度关联', exact: true })
  await createDialog.locator('.el-form-item').filter({ hasText: '事实表字段' }).locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: '省份编码（province_code）', exact: true }).click()
  await createDialog.locator('.el-form-item').filter({ hasText: '维度表' }).locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: '省份（dwd_province）', exact: true }).click()
  await createDialog.locator('.el-form-item').filter({ hasText: '目标字段' }).locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: '省份编码（code） · PK', exact: true }).click()
  await createDialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(createDialog).toBeHidden()
  await expect(page.locator('[data-relation-id="8"]')).toBeVisible()
  expect(backend.writes.map(write => [write.method, write.body.version])).toEqual([['PUT', 10], ['DELETE', 11], ['POST', 12]])
  expect(backend.dimension.status).toBe('approved')
  expect(backend.dimension.version).toBe(1)
})

test('relation conflicts preserve the edited mapping and unsaved dialog until explicitly discarded', async ({ page }) => {
  const backend = await installRelationBackend(page, { draft: true, conflict: true })
  await page.goto('/logical-tables/3?tab=relations&relation_id=7')
  await page.locator('[data-relation-id="7"]').getByRole('button', { name: '编辑', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '编辑维度关联', exact: true })
  await dialog.locator('label.el-radio').filter({ hasText: 'JOIN（字段等值关联）' }).click()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog.getByRole('alert').filter({ hasText: '资源已被其他用户修改' })).toBeVisible()
  await expect(dialog.getByRole('radio', { name: 'JOIN（字段等值关联）', exact: true })).toBeChecked()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  const confirmation = page.getByRole('dialog', { name: '存在未保存内容', exact: true })
  await confirmation.getByRole('button', { name: '继续编辑', exact: true }).click()
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await confirmation.getByRole('button', { name: '放弃并继续', exact: true }).click()
  await expect(dialog).toBeHidden()
  expect(backend.writes).toHaveLength(1)
})
