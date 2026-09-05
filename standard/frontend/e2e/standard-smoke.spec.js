import { expect, test } from '@playwright/test'

const allStandardPermissions = [
  'standard.code_set.create',
  'standard.code_set.delete',
  'standard.code_set.publish',
  'standard.code_set.read',
  'standard.code_set.update',
  'standard.document.create',
  'standard.document.delete',
  'standard.document.publish',
  'standard.document.read',
  'standard.document.update',
  'standard.document_extraction.create',
  'standard.domain.create',
  'standard.domain.delete',
  'standard.domain.read',
  'standard.domain.update',
  'standard.element.create',
  'standard.element.delete',
  'standard.element.publish',
  'standard.element.read',
  'standard.element.update',
  'standard.glossary.create',
  'standard.glossary.delete',
  'standard.glossary.publish',
  'standard.glossary.read',
  'standard.glossary.update',
  'standard.metric.create',
  'standard.metric.delete',
  'standard.metric.publish',
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
    code: 'leader',
    scope_type: 'domain',
    owner_domain_id: 2,
    lifecycle_state: 'active',
    version: 1,
    has_publication_history: false,
    tags: [],
    draft_revision_id: 211,
    current_revision: null,
    draft_revision: {
      id: 211,
      glossary_id: 21,
      revision_no: 1,
      name: '领队',
      alias: ['leader'],
      definition: '发起并组织户外活动的人',
      example: '',
      note: '',
      related_ids: [],
      change_summary: '初始修订',
      effective_from: '2026-08-12T08:00:00Z',
      status: 'draft'
    },
    created_at: '2026-08-12T08:00:00Z'
  }
]

const createDocumentFixture = (overrides = {}) => {
  const { revision: revisionOverrides = {}, ...identityOverrides } = overrides
  const id = identityOverrides.id || 71
  const revision = {
    id: id * 10 + 1,
    document_id: id,
    revision_no: 1,
    status: 'draft',
    name: '户外数据标准',
    version_label: 'v1',
    description: '户外业务数据标准',
    file_name: 'outdoor-standard.md',
    file_size: 2048,
    media_type: 'text/markdown',
    content_sha256: 'f'.repeat(64),
    change_summary: '初始修订',
    created_at: '2026-08-12T08:00:00Z',
    ...revisionOverrides
  }
  return {
    id,
    code: 'outdoor_data_standard',
    scope_type: 'domain',
    owner_domain_id: 2,
    doc_type: 'reference',
    source_org: '标准组',
    tags: [],
    lifecycle_state: 'active',
    version: 1,
    draft_revision_id: revision.id,
    draft_revision: revision,
    current_revision: null,
    has_publication_history: false,
    created_at: '2026-08-12T08:00:00Z',
    updated_at: '2026-08-12T08:00:00Z',
    ...identityOverrides
  }
}

const listPages = [
  ['/domains', '业务域管理'],
  ['/glossaries', '业务术语词典'],
  ['/elements', '数据元管理'],
  ['/code-sets', '新建码值集'],
  ['/units', '计量单位管理'],
  ['/metrics', '指标管理'],
  ['/documents', '全局文档库']
]

const visualPages = [
  ['/domains', '业务域管理', 'domains'],
  ['/glossaries', '业务术语词典', 'glossaries'],
  ['/elements', '数据元管理', 'elements'],
  ['/code-sets', '新建码值集', 'code-sets'],
  ['/units', '计量单位管理', 'units'],
  ['/metrics', '指标管理', 'metrics'],
  ['/documents', '全局文档库', 'documents']
]

const narrowVisualPages = visualPages.filter(([, , name]) => (
  ['domains', 'code-sets', 'documents'].includes(name)
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
  await page.goto('/glossaries?owner_domain_id=2&status=draft')
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
  await expect(page).toHaveURL(/\/glossaries\/21\?owner_domain_id=2&status=draft$/)
  await expect(page.getByRole('textbox', { name: '术语名称' })).toHaveValue('领队')
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/glossaries\?owner_domain_id=2&status=draft$/)
})

test('protects unsaved glossary changes when leaving the detail page', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries/21?owner_domain_id=2&status=draft')

  const nameInput = page.getByRole('textbox', { name: '术语名称' })
  await nameInput.fill('尚未保存的领队')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: /返回/ }).click()
  const confirm = page.getByRole('dialog', { name: '未保存的修改' })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '继续编辑' }).click()
  await expect(page).toHaveURL(/\/glossaries\/21\?owner_domain_id=2&status=draft$/)
  await expect(nameInput).toHaveValue('尚未保存的领队')

  await page.getByRole('button', { name: /返回/ }).click()
  await page.getByRole('dialog', { name: '未保存的修改' }).getByRole('button', { name: '离开' }).click()
  await expect(page).toHaveURL(/\/glossaries\?owner_domain_id=2&status=draft$/)
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
  await dialog.getByRole('textbox', { name: '编码' }).fill('duplicate_submit')
  await dialog.getByRole('textbox', { name: '术语名称' }).fill('重复提交测试')
  await dialog.getByRole('textbox', { name: '定义' }).fill('验证写请求只能发送一次')
  await dialog.getByRole('textbox', { name: '变更说明' }).fill('初始修订')
  const confirmButton = dialog.getByRole('button', { name: '确定' })
  await confirmButton.evaluate(button => {
    button.click()
    button.click()
  })

  await expect(dialog).not.toBeVisible()
  expect(backend.getGlossaryCreateRequests()).toBe(1)
})

test('hides glossary delete after any publication history exists', async ({ page }) => {
  const backend = await installMockBackend(page, { glossaryPublicationHistory: true })
  await page.goto('/glossaries')

  const row = page.getByRole('row').filter({ hasText: 'leader' })
  await expect(row.getByRole('button', { name: '详情' })).toBeVisible()
  await expect(row.getByRole('button', { name: '删除' })).toHaveCount(0)
  expect(backend.getDeleteRequests()).toEqual([])
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

  for (const path of ['/domains', '/code-sets', '/documents']) {
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
  await page.goto('/code-sets?keyword=gender&scope_type=domain')
  await page.getByText('性别', { exact: true }).click()
  await expect(page).toHaveURL(/\/code-sets\/31\?keyword=gender&scope_type=domain$/)
  await expect(page.getByText('性别', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/code-sets\?keyword=gender&scope_type=domain$/)
})

test('submits and publishes a draft data element revision', async ({ page }) => {
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
  await page.goto('/elements/41')

  await expect(page.getByText('R1 · 草稿', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '提交审核' }).click()
  await page.getByRole('dialog', { name: '提示' }).getByRole('button', { name: '确定' }).click()
  await expect(page.getByText('R1 · 审核中', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '发布' }).click()
  await page.getByRole('dialog', { name: '提示' }).getByRole('button', { name: '确定' }).click()
  await expect(page.getByText('R1 · 已发布', { exact: true })).toBeVisible()
  expect(backend.getActionRequests()).toEqual([
    '/api/v1/standard/elements/41/revisions/411/submit',
    '/api/v1/standard/elements/41/revisions/411/publish'
  ])
})

test('submits a data element only once when confirmation fires twice', async ({ page }) => {
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
  await page.goto('/elements/41')

  await page.getByRole('button', { name: '提交审核' }).click()
  const confirmButton = page.getByRole('dialog', { name: '提示' }).getByRole('button', { name: '确定' })
  await confirmButton.evaluate(button => {
    button.click()
    button.click()
  })

  await expect(page.getByText('R1 · 审核中', { exact: true })).toBeVisible()
  expect(backend.getActionRequests().filter(path => path === '/api/v1/standard/elements/41/revisions/411/submit')).toHaveLength(1)
})

test('moves a metric revision through submit, publish and withdraw states', async ({ page }) => {
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

  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 草稿' })).toBeVisible()
  await page.getByRole('button', { name: '提交审核' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 审核中' })).toBeVisible()

  await page.getByRole('button', { name: '发布' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 已发布' })).toBeVisible()

  await page.getByRole('button', { name: '撤回' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 已撤回' })).toBeVisible()

  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/metrics/51/revisions/511/submit',
    '/api/v1/standard/metrics/51/revisions/511/publish',
    '/api/v1/standard/metrics/51/revisions/511/withdraw'
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
    '业务域仍被子业务域、业务术语、数据元或指标引用，无法删除'
  )
  await expect(outdoorRow).toBeVisible()
  expect(backend.getDeleteRequests()).toEqual(['/api/v1/standard/domains/2'])
  expect(backend.getDomainDeleteRequests()).toEqual([{ id: 2, version: 1 }])
})

test('cancels document deletion without sending a request', async ({ page }) => {
  const backend = await installMockBackend(page, {
    documents: [createDocumentFixture()]
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

test('restores document detail from its canonical route and returns to the filtered list', async ({ page }) => {
  await installMockBackend(page, {
    documents: [createDocumentFixture()]
  })

  await page.goto('/documents?keyword=户外')
  await page.getByRole('row').filter({ hasText: '户外数据标准' }).getByRole('button', { name: '详情' }).click()
  await expect(page).toHaveURL(/\/documents\/71\?keyword=/)

  await page.reload()
  await expect(page.getByRole('heading', { name: '户外数据标准' })).toBeVisible()
  await expect(page.getByText(/outdoor-standard\.md · 2\.0 KB · SHA256/)).toBeVisible()
  await expect(page).toHaveURL(/\/documents\/71\?keyword=/)

  await page.getByRole('button', { name: /返回/ }).click()
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
  await uploadDialog.getByRole('textbox', { name: '编码' }).fill('participant_standard')
  await uploadDialog.getByRole('textbox', { name: '文档名称' }).fill('参与人数标准')
  await uploadDialog.getByRole('textbox', { name: '变更说明' }).fill('初始修订')
  await uploadDialog.locator('input[type="file"]').setInputFiles({
    name: 'participant-standard.pdf',
    mimeType: 'application/pdf',
    buffer: Buffer.from('fixture')
  })
  await uploadDialog.getByRole('button', { name: '上传并关联' }).click()

  await expect(page.locator('.el-message--error')).toContainText('文档文件超过 100 MiB 限制')
  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/metrics/51/documents',
    '/api/v1/standard/documents/72/revisions/721/file'
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
    documents: [createDocumentFixture()]
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

  for (const path of ['/domains', '/glossaries', '/elements', '/code-sets', '/units', '/metrics', '/documents']) {
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
  const glossaryFixtures = structuredClone(glossaries)
  if (options.glossaryPublicationHistory) {
    const published = { ...glossaryFixtures[0].draft_revision, status: 'published' }
    Object.assign(glossaryFixtures[0], {
      draft_revision_id: null,
      draft_revision: null,
      current_revision: published,
      has_publication_history: true
    })
  }
  const metrics = (options.metrics || []).map(item => {
    if (item.current_revision || item.draft_revision) return structuredClone(item)
    const revision = {
      id: item.id * 10 + 1,
      metric_definition_id: item.id,
      revision_no: 1,
      name: item.name,
      definition: item.definition || `${item.name}定义`,
      statistical_caliber: item.statistical_caliber || item.definition || `${item.name}口径`,
      semantic_formula: item.formula || '',
      metric_type: item.type || 'atomic',
      unit_id: null,
      dependencies: [],
      change_summary: '初始修订',
      effective_from: '2026-08-12T08:00:00Z',
      status: item.status === 'approved' ? 'published' : item.status === 'deprecated' ? 'withdrawn' : item.status,
      created_at: item.created_at || '2026-08-12T08:00:00Z'
    }
    const hasDraft = revision.status === 'draft' || revision.status === 'in_review'
    return {
      id: item.id,
      code: item.code,
      scope_type: item.domain_id ? 'domain' : 'tenant_common',
      owner_domain_id: item.domain_id || null,
      category_id: item.category_id || null,
      steward_id: null,
      tags: item.tags || [],
      lifecycle_state: 'active',
      version: item.version || 1,
      draft_revision_id: hasDraft ? revision.id : null,
      draft_revision: hasDraft ? revision : null,
      current_revision: hasDraft ? null : revision
    }
  })
  const elementAggregates = elements.map(item => {
    const revision = {
      id: item.id * 10 + 1,
      element_id: item.id,
      revision_no: 1,
      name: item.name,
      definition: item.definition || `${item.name}定义`,
      data_type: item.data_type,
      nullable: true,
      value_domain_kind: 'unrestricted',
      example_values: [],
      change_summary: '初始修订',
      status: item.status === 'approved' ? 'published' : item.status,
      created_at: '2026-08-12T08:00:00Z'
    }
    const hasDraft = revision.status === 'draft' || revision.status === 'in_review'
    return {
      id: item.id,
      code: item.code,
      scope_type: item.domain_id ? 'domain' : 'tenant_common',
      owner_domain_id: item.domain_id || null,
      tags: [],
      lifecycle_state: 'active',
      version: 1,
      draft_revision_id: hasDraft ? revision.id : null,
      draft_revision: hasDraft ? revision : null,
      current_revision: hasDraft ? null : revision
    }
  })
  const codeSetRevision = {
    id: 311,
    code_set_id: 31,
    revision_no: 1,
    name: '性别',
    description: '性别码值',
    value_type: 'string',
    change_summary: '初始修订',
    status: 'published',
    items: [],
    created_at: '2026-08-12T08:00:00Z'
  }
  const codeSetAggregate = {
    id: 31,
    code: 'gender',
    scope_type: 'domain',
    owner_domain_id: 2,
    origin: 'tenant',
    tags: [],
    lifecycle_state: 'active',
    version: 1,
    current_revision: codeSetRevision,
    draft_revision: null
  }
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
        error: '业务域仍被子业务域、业务术语、数据元或指标引用，无法删除'
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
      return fulfillJSON(route, { id: 24, code: request.postDataJSON().code, version: 1, draft_revision: { ...request.postDataJSON(), id: 241, status: 'draft', revision_no: 1 } }, 201)
    }
    if (request.method() === 'PUT' && path === '/api/v1/standard/glossaries/21' && options.glossaryVersionConflict) {
      return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
    }
    if (request.method() === 'DELETE') {
      deleteRequests.push(path)
      return fulfillJSON(route, {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/elements/41/revisions/411/submit') {
      actionRequests.push(path)
      if (options.delayedElementApprove) await new Promise(resolve => setTimeout(resolve, 150))
      const element = elementAggregates.find(item => item.id === 41)
      if (element?.draft_revision) {
        element.draft_revision.status = 'in_review'
        element.version += 1
      }
      return fulfillJSON(route, element || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/elements/41/revisions/411/publish') {
      actionRequests.push(path)
      const element = elementAggregates.find(item => item.id === 41)
      if (element?.draft_revision) {
        element.draft_revision.status = 'published'
        element.current_revision = element.draft_revision
        element.draft_revision = null
        element.draft_revision_id = null
        element.version += 1
      }
      return fulfillJSON(route, element || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/revisions/511/submit') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric?.draft_revision) {
        metric.draft_revision.status = 'in_review'
        metric.version += 1
      }
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/revisions/511/publish') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric?.draft_revision) {
        metric.draft_revision.status = 'published'
        metric.current_revision = metric.draft_revision
        metric.draft_revision = null
        metric.draft_revision_id = null
        metric.version += 1
      }
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/revisions/511/withdraw') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric?.current_revision) {
        metric.current_revision.status = 'withdrawn'
        metric.version += 1
      }
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/documents') {
      actionRequests.push(path)
      const body = request.postDataJSON()
      const document = createDocumentFixture({
        id: 72,
        code: body.code,
        scope_type: body.scope_type,
        owner_domain_id: body.owner_domain_id,
        doc_type: body.doc_type,
        source_org: body.source_org,
        revision: {
          name: body.name,
          version_label: body.version_label,
          description: body.description,
          file_name: null,
          file_size: 0,
          media_type: '',
          content_sha256: '',
          change_summary: body.change_summary
        }
      })
      documents.push(document)
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.version = (metric.version || 1) + 1
      return fulfillJSON(route, { document, version: metric?.version || 2 })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/documents/72/revisions/721/file' && options.uploadError) {
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
          ? [{ ...glossaryFixtures[0], id: 22, draft_revision: { ...glossaryFixtures[0].draft_revision, name: '新筛选结果' } }]
          : keyword === 'old'
            ? [{ ...glossaryFixtures[0], id: 23, draft_revision: { ...glossaryFixtures[0].draft_revision, name: '旧筛选结果' } }]
            : glossaryFixtures
        return fulfillJSON(route, { data, total: data.length })
      }
      const domainID = Number(url.searchParams.get('owner_domain_id'))
      const data = domainID ? glossaryFixtures.filter(item => item.owner_domain_id === domainID) : glossaryFixtures
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/standard/glossaries/21') return fulfillJSON(route, glossaryFixtures[0])
    if (path === '/api/v1/standard/glossaries/21/revisions') return fulfillJSON(route, [glossaryFixtures[0].draft_revision || glossaryFixtures[0].current_revision])
    if (path === '/api/v1/standard/glossaries/21/elements') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/glossaries/21/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: elementAggregates, total: elementAggregates.length })
    if (path === '/api/v1/standard/elements/41') return fulfillJSON(route, elementAggregates.find(item => item.id === 41) || {})
    if (path === '/api/v1/standard/elements/41/revisions') {
      const element = elementAggregates.find(item => item.id === 41)
      const revision = element?.draft_revision || element?.current_revision
      return fulfillJSON(route, revision ? [revision] : [])
    }
    if (path === '/api/v1/standard/elements/41/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/code-sets') {
      return fulfillJSON(route, { data: [codeSetAggregate], total: 1 })
    }
    if (path === '/api/v1/standard/code-sets/31') return fulfillJSON(route, codeSetAggregate)
    if (path === '/api/v1/standard/code-sets/31/revisions') return fulfillJSON(route, [codeSetRevision])
    if (path === '/api/v1/standard/code-sets/31/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/measurement-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/units') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/metric-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/metrics') return fulfillJSON(route, { data: metrics, total: metrics.length })
    if (path === '/api/v1/standard/metrics/51') return fulfillJSON(route, metrics.find(item => item.id === 51) || {})
    if (path === '/api/v1/standard/metrics/51/revisions') {
      const metric = metrics.find(item => item.id === 51)
      const revision = metric?.draft_revision || metric?.current_revision
      return fulfillJSON(route, revision ? [revision] : [])
    }
    if (path === '/api/v1/standard/metrics/51/documents') {
      metricDocumentListRequests += 1
      return fulfillJSON(route, metricDocumentLinked ? documents : [])
    }
    if (path === '/api/v1/standard/documents') return fulfillJSON(route, { data: documents, total: documents.length })
    if (path === '/api/v1/standard/documents/71') return fulfillJSON(route, documents.find(item => item.id === 71) || {})
    if (path === '/api/v1/standard/documents/71/revisions') {
      const document = documents.find(item => item.id === 71)
      const revision = document?.draft_revision || document?.current_revision
      return fulfillJSON(route, revision ? [revision] : [])
    }
    if (path === '/api/v1/standard/documents/71/extractions') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/documents/71/mappings') {
      return fulfillJSON(route, { elements: [], glossaries: [], metrics: [] })
    }
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
