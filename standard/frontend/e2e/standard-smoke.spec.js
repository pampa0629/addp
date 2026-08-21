import { expect, test } from '@playwright/test'

const allStandardPermissions = [
  'standard.classification.create',
  'standard.classification.delete',
  'standard.classification.read',
  'standard.classification.update',
  'standard.code_set.create',
  'standard.code_set.delete',
  'standard.code_set.read',
  'standard.code_set.update',
  'standard.dimension_hierarchy.create',
  'standard.dimension_hierarchy.delete',
  'standard.dimension_hierarchy.read',
  'standard.dimension_hierarchy.update',
  'standard.document.create',
  'standard.document.delete',
  'standard.document.read',
  'standard.document.update',
  'standard.domain.create',
  'standard.domain.delete',
  'standard.domain.read',
  'standard.domain.update',
  'standard.element.approve',
  'standard.element.create',
  'standard.element.delete',
  'standard.element.read',
  'standard.element.update',
  'standard.glossary.approve',
  'standard.glossary.create',
  'standard.glossary.delete',
  'standard.glossary.offline',
  'standard.glossary.read',
  'standard.glossary.update',
  'standard.metric.approve',
  'standard.metric.create',
  'standard.metric.delete',
  'standard.metric.offline',
  'standard.metric.read',
  'standard.metric.update',
  'standard.unit.create',
  'standard.unit.delete',
  'standard.unit.read',
  'standard.unit.update'
]

const readOnlyStandardPermissions = allStandardPermissions.filter(permission => permission.endsWith('.read'))

const domains = [
  { id: 2, name: '户外域', code: 'outdoor', description: '户外业务', version: 1, children: [] },
  { id: 1, name: '客户域', code: 'customer', description: '客户业务', version: 1, children: [] }
]

const glossaries = [
  {
    id: 21,
    name: '领队',
    alias: ['leader'],
    domain_id: 2,
    definition: '发起并组织户外活动的人',
    status: 'approved',
    version: 1,
    tags: [],
    created_at: '2026-08-12T08:00:00Z'
  }
]

const listPages = [
  ['/domains', '业务域管理'],
  ['/glossaries', '业务术语词典'],
  ['/elements', '数据元管理'],
  ['/code-sets', '新建码值集'],
  ['/units', '计量单位管理'],
  ['/classifications', '数据分类与分级'],
  ['/dimension-hierarchies', '维度层级'],
  ['/metrics', '指标管理'],
  ['/documents', '全局文档库']
]

const visualPages = [
  ['/domains', '业务域管理', 'domains'],
  ['/glossaries', '业务术语词典', 'glossaries'],
  ['/elements', '数据元管理', 'elements'],
  ['/code-sets', '新建码值集', 'code-sets'],
  ['/units', '计量单位管理', 'units'],
  ['/classifications', '数据分类与分级', 'classifications'],
  ['/dimension-hierarchies', '维度层级', 'dimension-hierarchies'],
  ['/metrics', '指标管理', 'metrics'],
  ['/documents', '全局文档库', 'documents']
]

const narrowVisualPages = visualPages.filter(([, , name]) => (
  ['domains', 'code-sets', 'dimension-hierarchies', 'documents'].includes(name)
))

const themeVisualPages = narrowVisualPages
const themeVisualModes = ['dark', 'blue', 'purple']

test('loads every Standard management page', async ({ page }) => {
  await installMockBackend(page)
  for (const [path, visibleText] of listPages) {
    await page.goto(path)
    await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
  }
})

test.describe('Standard management page visual baselines', () => {
  test.describe('desktop', () => {
    test.use({ viewport: { width: 1280, height: 800 } })

    for (const [path, visibleText, name] of visualPages) {
      test(`${name} desktop`, async ({ page }) => {
        await installMockBackend(page)
        await page.goto(path)
        await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
        await expect(page.locator('.el-loading-mask:visible')).toHaveCount(0)
        await expect(page).toHaveScreenshot(`standard-${name}-desktop.png`, {
          animations: 'disabled',
          caret: 'hide',
          fullPage: true
        })
      })
    }
  })

  test.describe('narrow', () => {
    test.use({ viewport: { width: 720, height: 760 } })

    for (const [path, visibleText, name] of narrowVisualPages) {
      test(`${name} narrow`, async ({ page }) => {
        await installMockBackend(page)
        await page.goto(path)
        await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
        await expect(page.locator('.el-loading-mask:visible')).toHaveCount(0)
        await expect(page).toHaveScreenshot(`standard-${name}-narrow.png`, {
          animations: 'disabled',
          caret: 'hide',
          fullPage: true
        })
      })
    }
  })
})

test.describe('Standard theme visual baselines', () => {
  test.use({ viewport: { width: 1280, height: 800 } })

  for (const theme of themeVisualModes) {
    for (const [path, visibleText, name] of themeVisualPages) {
      test(`${theme} ${name}`, async ({ page }) => {
        await installMockBackend(page, { theme })
        await page.goto(path)
        await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
        await expect.poll(() => page.evaluate(() => document.documentElement.className)).toContain(theme)
        await expect(page.locator('.el-loading-mask:visible')).toHaveCount(0)
        await expect(page).toHaveScreenshot(`standard-${theme}-${name}.png`, {
          animations: 'disabled',
          caret: 'hide',
          fullPage: true
        })
      })
    }
  }
})

test('inherits the selected domain when creating a glossary and preserves filters through detail', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries?domain_id=2&status=approved')
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()

  const actions = page.locator('.table-actions').first()
  await expect(actions).toBeVisible()
  const actionLayout = await actions.evaluate(element => ({
    whiteSpace: getComputedStyle(element).whiteSpace,
    buttonTops: Array.from(element.querySelectorAll('button')).map(button => button.getBoundingClientRect().top)
  }))
  expect(actionLayout.whiteSpace).toBe('nowrap')
  expect(new Set(actionLayout.buttonTops).size).toBe(1)

  await page.getByRole('button', { name: '新建术语' }).click()
  const createDialog = page.getByRole('dialog', { name: '新建业务术语' })
  await expect(createDialog.getByText('户外域', { exact: true })).toBeVisible()
  await createDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('button', { name: '详情' }).click()
  await expect(page).toHaveURL(/\/glossaries\/21\?domain_id=2&status=approved$/)
  await expect(page.getByRole('textbox', { name: '术语名称' })).toHaveValue('领队')
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/glossaries\?domain_id=2&status=approved$/)
})

test('protects unsaved glossary changes when leaving the detail page', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries/21?domain_id=2&status=approved')

  const nameInput = page.getByRole('textbox', { name: '术语名称' })
  await nameInput.fill('尚未保存的领队')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: /返回/ }).click()
  const confirm = page.getByRole('dialog', { name: '未保存的修改' })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '继续编辑' }).click()
  await expect(page).toHaveURL(/\/glossaries\/21\?domain_id=2&status=approved$/)
  await expect(nameInput).toHaveValue('尚未保存的领队')

  await page.getByRole('button', { name: /返回/ }).click()
  await page.getByRole('dialog', { name: '未保存的修改' }).getByRole('button', { name: '离开' }).click()
  await expect(page).toHaveURL(/\/glossaries\?domain_id=2&status=approved$/)
})

test('keeps local glossary edits when a stale version is rejected', async ({ page }) => {
  await installMockBackend(page, { glossaryVersionConflict: true })
  await page.goto('/glossaries/21')

  const nameInput = page.getByRole('textbox', { name: '术语名称' })
  await nameInput.fill('本地尚未保存的领队')
  await page.getByRole('button', { name: '保存' }).click()

  await expect(page.getByText('资源已被其他用户修改，请刷新后重试')).toBeVisible()
  await expect(nameInput).toHaveValue('本地尚未保存的领队')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
})

test('keeps the latest glossary filter result when an older request returns later', async ({ page }) => {
  await installMockBackend(page, { glossaryListRace: true })
  const oldRequest = page.waitForRequest(request => (
    request.url().includes('/api/v1/standard/glossaries?') &&
    new URL(request.url()).searchParams.get('keyword') === 'old'
  ))
  await page.goto('/glossaries?keyword=old')
  await oldRequest

  const keywordInput = page.getByPlaceholder('搜索术语名称或定义')
  await keywordInput.fill('new')
  await keywordInput.press('Tab')

  await expect(page.getByText('新筛选结果', { exact: true })).toBeVisible()
  await expect(page.getByText('旧筛选结果', { exact: true })).toHaveCount(0)
})

test('submits a new glossary only once when the confirm action fires twice', async ({ page }) => {
  const backend = await installMockBackend(page, { delayedGlossaryCreate: true })
  await page.goto('/glossaries')
  await page.getByRole('button', { name: '新建术语' }).click()

  const dialog = page.getByRole('dialog', { name: '新建业务术语' })
  await dialog.getByRole('textbox', { name: '术语名称' }).fill('重复提交测试')
  await dialog.getByRole('textbox', { name: '定义' }).fill('验证写请求只能发送一次')
  const confirmButton = dialog.getByRole('button', { name: '确定' })
  await confirmButton.evaluate(button => {
    button.click()
    button.click()
  })

  await expect(dialog).not.toBeVisible()
  expect(backend.getGlossaryCreateRequests()).toBe(1)
})

test('keeps tree actions on one line and canceling delete sends no request', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/domains')

  const rows = page.locator('.tree-node')
  await expect(rows).toHaveCount(2)
  const layout = await rows.evaluateAll(elements => elements.map(element => {
    const buttons = Array.from(element.querySelectorAll('.node-actions button'))
    return {
      height: element.getBoundingClientRect().height,
      buttonTops: buttons.map(button => Math.round(button.getBoundingClientRect().top)),
      whiteSpace: getComputedStyle(element.querySelector('.node-actions')).whiteSpace
    }
  }))

  for (const row of layout) {
    expect(row.height).toBeLessThanOrEqual(30)
    expect(new Set(row.buttonTops).size).toBe(1)
    expect(row.whiteSpace).toBe('nowrap')
  }

  await rows.filter({ hasText: '户外域' }).getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除业务域「户外域」？')
  await confirm.getByRole('button', { name: '取消' }).click()
  await expect(confirm).not.toBeVisible()
  expect(backend.getDeleteRequests()).toEqual([])
  await expect(page.locator('.el-message--error')).toHaveCount(0)
})

test('keeps local domain edits when a stale version is rejected', async ({ page }) => {
  const backend = await installMockBackend(page, { domainVersionConflict: true })
  await page.goto('/domains')

  await page.locator('.tree-node').filter({ hasText: '户外域' }).getByRole('button', { name: '编辑' }).click()
  const dialog = page.getByRole('dialog', { name: '编辑业务域' })
  const nameInput = dialog.getByRole('textbox', { name: '名称' })
  await nameInput.fill('本地尚未保存的户外域')
  await dialog.getByRole('button', { name: '确定' }).click()

  await expect(page.getByText('资源已被其他用户修改，请刷新后重试')).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(nameInput).toHaveValue('本地尚未保存的户外域')
  expect(backend.getDomainUpdateRequests()).toEqual([{ id: 2, version: 1, name: '本地尚未保存的户外域' }])
})

test('supports keyboard focus and escape dismissal in domain dialogs', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/domains')

  const createButton = page.getByRole('button', { name: '新建业务域' })
  await createButton.click()
  const createDialog = page.getByRole('dialog', { name: '新建业务域' })
  const nameInput = createDialog.getByRole('textbox', { name: '名称' })
  await expect(nameInput).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(createDialog).not.toBeVisible()
  await expect(createButton).toBeFocused()

  const deleteButton = page.locator('.tree-node').filter({ hasText: '户外域' }).getByRole('button', { name: '删除' })
  await deleteButton.click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm.getByRole('button', { name: '确定' })).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(confirm).not.toBeVisible()
  await expect(deleteButton).toBeFocused()
  expect(backend.getDeleteRequests()).toEqual([])
})

test('avoids page overflow and keeps table actions on one line at narrow width', async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 760 })
  await installMockBackend(page)

  for (const path of ['/domains', '/code-sets', '/documents', '/dimension-hierarchies']) {
    await page.goto(path)
    await expect.poll(() => page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }))).toEqual({ clientWidth: 720, scrollWidth: 720 })
  }

  await page.goto('/code-sets')
  const actions = page.locator('.table-actions').first()
  await expect(actions).toBeVisible()
  const actionLayout = await actions.locator('button').evaluateAll(buttons => ({
    tops: buttons.map(button => Math.round(button.getBoundingClientRect().top)),
    whiteSpace: getComputedStyle(buttons[0].parentElement).whiteSpace
  }))
  expect(new Set(actionLayout.tops).size).toBe(1)
  expect(actionLayout.whiteSpace).toBe('nowrap')
})

test('preserves code-set filters through detail and back navigation', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/code-sets?keyword=gender&type=custom')
  await page.getByText('性别', { exact: true }).click()
  await expect(page).toHaveURL(/\/code-sets\/31\?keyword=gender&type=custom$/)
  await expect(page.getByText('性别', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/code-sets\?keyword=gender&type=custom$/)
})

test('updates a data element from draft to approved', async ({ page }) => {
  const backend = await installMockBackend(page, {
    elements: [{
      id: 41,
      name: '活动编号',
      code: 'activity_id',
      data_type: 'string',
      domain_id: 2,
      status: 'draft',
      quality_rules: null
    }]
  })
  await page.goto('/elements?status=draft')

  const row = page.getByRole('row').filter({ hasText: '活动编号' })
  await expect(row.getByText('草稿', { exact: true })).toBeVisible()
  await row.getByRole('button', { name: '审批' }).click()

  await expect(row.getByText('已审批', { exact: true })).toBeVisible()
  await expect(row.getByRole('button', { name: '审批' })).toHaveCount(0)
  expect(backend.getActionRequests()).toContain('/api/v1/standard/elements/41/approve')
})

test('approves a data element only once when the row action fires twice', async ({ page }) => {
  const backend = await installMockBackend(page, {
    delayedElementApprove: true,
    elements: [{
      id: 41,
      name: '审批防重数据元',
      code: 'approve_once',
      data_type: 'string',
      domain_id: 2,
      status: 'draft',
      quality_rules: null
    }]
  })
  await page.goto('/elements?status=draft')

  const row = page.getByRole('row').filter({ hasText: '审批防重数据元' })
  const approveButton = row.getByRole('button', { name: '审批' })
  await approveButton.evaluate(button => {
    button.click()
    button.click()
  })

  await expect(row.getByText('已审批', { exact: true })).toBeVisible()
  expect(backend.getActionRequests().filter(path => path === '/api/v1/standard/elements/41/approve')).toHaveLength(1)
})

test('moves a metric through approve and deprecate states', async ({ page }) => {
  const backend = await installMockBackend(page, {
    elements: [{
      id: 41,
      name: '活动参与人数数据元',
      code: 'participant_count_element',
      data_type: 'int',
      domain_id: 2,
      status: 'approved'
    }],
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'draft',
      tags: [],
      element_ids: [41],
      created_at: '2026-08-12T08:00:00Z'
    }]
  })
  await page.goto('/metrics/51')

  await expect(page.getByText('草稿', { exact: true })).toBeVisible()
  await expect(page.getByText('活动参与人数数据元', { exact: true })).toBeVisible()
  await expect(page.getByText('participant_count_element', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '审批' }).click()
  let confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认审批通过？')
  await confirm.getByRole('button', { name: '确定' }).click()
  await expect(page.getByText('已审批', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '废弃' }).click()
  confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认废弃该指标？')
  await confirm.getByRole('button', { name: '确定' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: '已废弃' })).toBeVisible()
  await expect(page.getByRole('button', { name: '废弃' })).toHaveCount(0)

  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/metrics/51/approve',
    '/api/v1/standard/metrics/51/deprecate'
  ]))
})

test('adds and deletes a dimension hierarchy level', async ({ page }) => {
  const backend = await installMockBackend(page, {
    hierarchy: {
      id: 61,
      name: '时间维度',
      code: 'time',
      description: '时间上下钻',
      version: 1,
      levels: [{ id: 611, level_num: 1, name: '年', description: '年份', sort_order: 0 }]
    }
  })
  await page.goto('/dimension-hierarchies/61')

  await expect(page.getByRole('cell', { name: '年', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '添加层级' }).click()
  const levelDialog = page.getByRole('dialog', { name: '添加层次' })
  await levelDialog.getByRole('textbox', { name: '* 层次名称' }).fill('月')
  await levelDialog.getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('cell', { name: '月', exact: true })).toBeVisible()

  const monthRow = page.getByRole('row').filter({ hasText: '月' })
  await monthRow.getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除层级「月」？')
  await confirm.getByRole('button', { name: '确定' }).click()
  await expect(page.getByRole('cell', { name: '月', exact: true })).toHaveCount(0)

  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/dimension-hierarchies/61/levels',
    '/api/v1/standard/dimension-hierarchies/61/levels/612'
  ]))
})

test('shows the backend domain conflict message after confirmed deletion', async ({ page }) => {
  const backend = await installMockBackend(page, { domainDeleteConflict: true })
  await page.goto('/domains')

  const outdoorRow = page.locator('.tree-node').filter({ hasText: '户外域' })
  await outdoorRow.getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除业务域「户外域」？')
  await confirm.getByRole('button', { name: '确定' }).click()

  await expect(page.locator('.el-message--error')).toContainText(
    '业务域仍被子业务域、业务术语、数据元、指标或维度层级引用，无法删除'
  )
  await expect(outdoorRow).toBeVisible()
  expect(backend.getDeleteRequests()).toEqual(['/api/v1/standard/domains/2'])
  expect(backend.getDomainDeleteRequests()).toEqual([{ id: 2, version: 1 }])
})

test('cancels document deletion without sending a request', async ({ page }) => {
  const backend = await installMockBackend(page, {
    documents: [{
      id: 71,
      name: '户外数据标准',
      doc_type: 'reference',
      source_org: '标准组',
      document_version: 'v1',
      version: 1,
      file_name: 'outdoor-standard.pdf',
      file_size: 2048,
      created_at: '2026-08-12T08:00:00Z'
    }]
  })
  await page.goto('/documents')

  const row = page.getByRole('row').filter({ hasText: '户外数据标准' })
  await row.getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除文档"户外数据标准"？')
  await confirm.getByRole('button', { name: '取消' }).click()
  await expect(confirm).not.toBeVisible()
  await expect(row).toBeVisible()
  expect(backend.getDeleteRequests()).toEqual([])
})

test('restores document detail from its canonical route and closes back to the filtered list', async ({ page }) => {
  await installMockBackend(page, {
    documents: [{
      id: 71,
      name: '户外数据标准',
      doc_type: 'reference',
      source_org: '标准组',
      document_version: 'v1',
      version: 1,
      file_name: 'outdoor-standard.pdf',
      file_size: 2048,
      created_at: '2026-08-12T08:00:00Z'
    }]
  })

  await page.goto('/documents?keyword=户外')
  await page.getByText('户外数据标准', { exact: true }).click()
  await expect(page).toHaveURL(/\/documents\/71\?keyword=/)

  await page.reload()
  const drawer = page.getByRole('dialog', { name: '户外数据标准' })
  await expect(drawer).toBeVisible()
  await expect(drawer).toContainText('outdoor-standard.pdf（2.0 KB）')
  await expect(page).toHaveURL(/\/documents\/71\?keyword=/)

  await drawer.getByRole('button', { name: '关闭此对话框' }).click()
  await expect(drawer).not.toBeVisible()
  await expect(page).toHaveURL(/\/documents\?keyword=/)
})

test('shows the backend upload error when attaching a file to a standard item', async ({ page }) => {
  const backend = await installMockBackend(page, {
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'approved',
      tags: [],
      version: 1,
      created_at: '2026-08-12T08:00:00Z'
    }],
    documents: [],
    uploadError: '文档文件超过 100 MiB 限制'
  })
  await page.goto('/metrics/51')

  await page.getByRole('button', { name: '上传新文档' }).click()
  const uploadDialog = page.getByRole('dialog', { name: '上传并关联文档' })
  await uploadDialog.getByRole('textbox', { name: '文档名称' }).fill('参与人数标准')
  await uploadDialog.locator('input[type="file"]').setInputFiles({
    name: 'participant-standard.pdf',
    mimeType: 'application/pdf',
    buffer: Buffer.from('fixture')
  })
  await uploadDialog.getByRole('button', { name: '上传并关联' }).click()

  await expect(page.locator('.el-message--error')).toContainText('文档文件超过 100 MiB 限制')
  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/metrics/51/documents',
    '/api/v1/standard/documents/72/upload'
  ]))
  await expect(uploadDialog).toBeVisible()
})

test('links an existing document to a metric and refreshes the panel', async ({ page }) => {
  const backend = await installMockBackend(page, {
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'approved',
      tags: [],
      version: 1,
      created_at: '2026-08-12T08:00:00Z'
    }],
    documents: [{
      id: 71,
      name: '户外数据标准',
      doc_type: 'reference',
      source_org: '标准组',
      document_version: 'v1',
      version: 1,
      file_name: 'outdoor-standard.pdf',
      file_size: 2048,
      created_at: '2026-08-12T08:00:00Z'
    }]
  })
  await page.goto('/metrics/51')

  await page.getByRole('button', { name: '关联已有文档' }).click()
  const linkDialog = page.getByRole('dialog', { name: '关联已有文档' })
  await linkDialog.getByRole('combobox').click({ force: true })
  await page.getByRole('option', { name: /户外数据标准/ }).click()
  await page.keyboard.press('Escape')
  const confirmLink = linkDialog.getByRole('button', { name: '确认关联' })
  await expect(confirmLink).toBeEnabled()
  await confirmLink.click()

  await expect.poll(() => backend.getActionRequests()).toContain('/api/v1/standard/metrics/51/documents/link')
  await expect(page.getByRole('cell', { name: '户外数据标准', exact: true })).toBeVisible()
  await expect(linkDialog).not.toBeVisible()
})

test('presents Standard pages as read-only when the role only has read permissions', async ({ page }) => {
  await installMockBackend(page, {
    permissions: readOnlyStandardPermissions,
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'draft',
      tags: []
    }],
    elements: [{
      id: 41,
      name: '活动编号',
      code: 'activity_id',
      data_type: 'string',
      status: 'draft',
      quality_rules: null
    }]
  })

  for (const path of ['/domains', '/glossaries', '/elements', '/code-sets', '/units', '/classifications', '/dimension-hierarchies', '/metrics', '/documents']) {
    await page.goto(path)
    await expect(page.getByRole('button', { name: /新建|新增|添加分类|录入文档/ })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /审批|废弃|删除/ })).toHaveCount(0)
  }

  await page.goto('/glossaries/21')
  await expect(page.getByRole('textbox', { name: '术语名称' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /添加数据元|上传新文档|关联已有文档|解除关联/ })).toHaveCount(0)

  await page.goto('/metrics/51')
  await expect(page.getByRole('textbox', { name: '指标名称' })).toBeDisabled()
  await expect(page.getByRole('button', { name: /保存|审批|上传新文档|关联已有文档/ })).toHaveCount(0)

  await page.goto('/elements/41')
  await expect(page.getByRole('textbox', { name: '名称' }).first()).toBeDisabled()
  await expect(page.getByRole('button', { name: /保存|审批|添加规则|上传新文档|关联已有文档/ })).toHaveCount(0)
})

test('matches document panel controls to the backend permission combinations', async ({ page }) => {
  await installMockBackend(page, {
    permissions: [
      'standard.metric.read',
      'standard.metric.update',
      'standard.document.read',
      'standard.document.create'
    ],
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'approved',
      tags: []
    }]
  })
  await page.goto('/metrics/51')

  await expect(page.getByRole('button', { name: '上传新文档' })).toBeVisible()
  await expect(page.getByRole('button', { name: '关联已有文档' })).toHaveCount(0)
  await page.getByRole('button', { name: '上传新文档' }).click()
  const uploadDialog = page.getByRole('dialog', { name: '上传并关联文档' })
  await expect(uploadDialog.getByRole('button', { name: '选择文件' })).toHaveCount(0)
  await expect(uploadDialog.getByRole('button', { name: '仅录入元数据' })).toBeVisible()
})

test('updates Standard controls immediately when the authorization context changes', async ({ page }) => {
  await installMockBackend(page, {
    permissions: readOnlyStandardPermissions,
    authContextPermissionsByToken: {
      'standard-e2e-token': readOnlyStandardPermissions,
      'standard-e2e-full-token': allStandardPermissions
    }
  })
  await page.goto('/glossaries')
  await expect(page.getByRole('button', { name: '新建术语' })).toHaveCount(0)

  await page.evaluate(() => {
    const channel = new BroadcastChannel('addp-auth-session')
    channel.postMessage({
      type: 'token',
      token: 'standard-e2e-full-token',
      expiresAt: Date.now() + 900_000
    })
    channel.close()
  })
  await expect(page.getByRole('button', { name: '新建术语' })).toBeVisible()

  await page.evaluate(() => {
    const channel = new BroadcastChannel('addp-auth-session')
    channel.postMessage({
      type: 'token',
      token: 'standard-e2e-token',
      expiresAt: Date.now() + 900_000
    })
    channel.close()
  })
  await expect(page.getByRole('button', { name: '新建术语' })).toHaveCount(0)
})

async function installMockBackend(page, options = {}) {
  const deleteRequests = []
  const actionRequests = []
  let metricDocumentListRequests = 0
  let glossaryListRaceRequests = 0
  let glossaryCreateRequests = 0
  const domainUpdateRequests = []
  const domainDeleteRequests = []
  let metricDocumentLinked = false
  const documents = (options.documents || []).map(item => ({ ...item }))
  const elements = (options.elements || []).map(item => ({ ...item }))
  const metrics = (options.metrics || []).map(item => ({ ...item }))
  const hierarchy = options.hierarchy
    ? { ...options.hierarchy, levels: options.hierarchy.levels.map(item => ({ ...item })) }
    : null
  const permissions = options.permissions ?? allStandardPermissions
  const authContextPermissionsByToken = options.authContextPermissionsByToken || {}
  await page.addInitScript(({ theme }) => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', theme || 'light')
  }, { theme: options.theme })
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (request.method() === 'DELETE' && path === '/api/v1/standard/domains/2' && options.domainDeleteConflict) {
      deleteRequests.push(path)
      domainDeleteRequests.push({ id: 2, version: request.postDataJSON().version })
      return fulfillJSON(route, {
        error: '业务域仍被子业务域、业务术语、数据元、指标或维度层级引用，无法删除'
      }, 409)
    }
    if (request.method() === 'PUT' && path === '/api/v1/standard/domains/2') {
      const body = request.postDataJSON()
      domainUpdateRequests.push({ id: 2, version: body.version, name: body.name })
      if (options.domainVersionConflict) {
        return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
      }
      return fulfillJSON(route, { ...domains[0], ...body, version: domains[0].version + 1 })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/glossaries' && options.delayedGlossaryCreate) {
      glossaryCreateRequests += 1
      await new Promise(resolve => setTimeout(resolve, 150))
      return fulfillJSON(route, { id: 24, ...request.postDataJSON(), status: 'draft' }, 201)
    }
    if (request.method() === 'PUT' && path === '/api/v1/standard/glossaries/21' && options.glossaryVersionConflict) {
      return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
    }
    if (request.method() === 'DELETE' && path === '/api/v1/standard/dimension-hierarchies/61/levels/612') {
      actionRequests.push(path)
      const index = hierarchy.levels.findIndex(level => level.id === 612)
      if (index >= 0) hierarchy.levels.splice(index, 1)
      hierarchy.version += 1
      return fulfillJSON(route, { version: hierarchy.version })
    }
    if (request.method() === 'DELETE') {
      deleteRequests.push(path)
      return fulfillJSON(route, {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/elements/41/approve') {
      actionRequests.push(path)
      if (options.delayedElementApprove) await new Promise(resolve => setTimeout(resolve, 150))
      const element = elements.find(item => item.id === 41)
      if (element) element.status = 'approved'
      return fulfillJSON(route, element || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/approve') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.status = 'approved'
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/deprecate') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.status = 'deprecated'
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/dimension-hierarchies/61/levels') {
      actionRequests.push(path)
      const level = { id: 612, ...request.postDataJSON() }
      hierarchy.levels.push(level)
      hierarchy.version += 1
      return fulfillJSON(route, { level, version: hierarchy.version })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/documents') {
      actionRequests.push(path)
      const document = {
        id: 72,
        name: request.postDataJSON().name,
        doc_type: request.postDataJSON().doc_type,
        source_org: request.postDataJSON().source_org,
        document_version: request.postDataJSON().document_version,
        version: 1,
        file_name: null,
        created_at: '2026-08-12T08:00:00Z'
      }
      documents.push(document)
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.version = (metric.version || 1) + 1
      return fulfillJSON(route, { document, version: metric?.version || 2 })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/documents/72/upload' && options.uploadError) {
      actionRequests.push(path)
      return fulfillJSON(route, { error: options.uploadError }, 413)
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/documents/link') {
      actionRequests.push(path)
      documents.forEach(document => { document.linkedToMetric = true })
      metricDocumentLinked = true
      const document = documents[0]
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.version = (metric.version || 1) + 1
      return fulfillJSON(route, { version: metric?.version || 2 })
    }
    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'standard-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'standard-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      const token = request.headers().authorization?.replace(/^Bearer\s+/i, '')
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions: authContextPermissionsByToken[token] ?? permissions }] }
      })
    }
    if (path === '/api/v1/standard/domains') return fulfillJSON(route, domains)
    if (path === '/api/v1/standard/glossaries') {
      if (options.glossaryListRace) {
        glossaryListRaceRequests += 1
        const keyword = url.searchParams.get('keyword') || ''
        if (keyword === 'old') await new Promise(resolve => setTimeout(resolve, 250))
        const data = keyword === 'new'
          ? [{ ...glossaries[0], id: 22, name: '新筛选结果' }]
          : keyword === 'old'
            ? [{ ...glossaries[0], id: 23, name: '旧筛选结果' }]
            : glossaries
        return fulfillJSON(route, { data, total: data.length })
      }
      const domainID = Number(url.searchParams.get('domain_id'))
      const data = domainID ? glossaries.filter(item => item.domain_id === domainID) : glossaries
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/standard/glossaries/21') return fulfillJSON(route, glossaries[0])
    if (path === '/api/v1/standard/glossaries/21/elements') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/glossaries/21/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: elements, total: elements.length })
    if (path === '/api/v1/standard/elements/41') return fulfillJSON(route, elements.find(item => item.id === 41) || {})
    if (path === '/api/v1/standard/code-sets') {
      return fulfillJSON(route, {
        data: [{ id: 31, code: 'gender', name: '性别', type: 'custom', description: '性别码值', version: 1, created_at: '2026-08-12T08:00:00Z' }],
        total: 1
      })
    }
    if (path === '/api/v1/standard/code-sets/31') {
      return fulfillJSON(route, { id: 31, code: 'gender', name: '性别', type: 'custom', description: '性别码值', version: 1 })
    }
    if (path === '/api/v1/standard/code-sets/31/items') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/measurement-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/units') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/classifications') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/grading-levels') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/metric-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/metrics') return fulfillJSON(route, { data: metrics, total: metrics.length })
    if (path === '/api/v1/standard/metrics/51') return fulfillJSON(route, metrics.find(item => item.id === 51) || {})
    if (path === '/api/v1/standard/metrics/51/documents') {
      metricDocumentListRequests += 1
      return fulfillJSON(route, metricDocumentLinked ? documents : [])
    }
    if (path === '/api/v1/standard/documents') return fulfillJSON(route, { data: documents, total: documents.length })
    if (path === '/api/v1/standard/documents/71') return fulfillJSON(route, documents.find(item => item.id === 71) || {})
    if (path === '/api/v1/standard/documents/71/mappings') {
      return fulfillJSON(route, { elements: [], glossaries: [], metrics: [] })
    }
    if (path === '/api/v1/standard/dimension-hierarchies') return fulfillJSON(route, hierarchy ? [hierarchy] : [])
    if (path === '/api/v1/standard/dimension-hierarchies/61') return fulfillJSON(route, hierarchy || {})

    return fulfillJSON(route, {})
  })

  return {
    getActionRequests: () => [...actionRequests],
    getDeleteRequests: () => [...deleteRequests],
    getDomainUpdateRequests: () => [...domainUpdateRequests],
    getDomainDeleteRequests: () => [...domainDeleteRequests],
    getGlossaryCreateRequests: () => glossaryCreateRequests,
    isMetricDocumentLinked: () => metricDocumentLinked
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
