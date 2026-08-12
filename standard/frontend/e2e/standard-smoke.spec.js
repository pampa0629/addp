import { expect, test } from '@playwright/test'

const domains = [
  { id: 2, name: '户外域', code: 'outdoor', description: '户外业务', children: [] },
  { id: 1, name: '客户域', code: 'customer', description: '客户业务', children: [] }
]

const glossaries = [
  {
    id: 21,
    name: '领队',
    alias: ['leader'],
    domain_id: 2,
    definition: '发起并组织户外活动的人',
    status: 'approved',
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

test('loads every Standard management page', async ({ page }) => {
  await installMockBackend(page)
  for (const [path, visibleText] of listPages) {
    await page.goto(path)
    await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
  }
})

test('inherits the selected domain when creating a glossary and preserves filters through detail', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries?domain_id=2&status=approved')
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()

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

test('moves a metric through approve and deprecate states', async ({ page }) => {
  const backend = await installMockBackend(page, {
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'draft',
      tags: [],
      created_at: '2026-08-12T08:00:00Z'
    }]
  })
  await page.goto('/metrics/51')

  await expect(page.getByText('草稿', { exact: true })).toBeVisible()
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
})

test('cancels document deletion without sending a request', async ({ page }) => {
  const backend = await installMockBackend(page, {
    documents: [{
      id: 71,
      name: '户外数据标准',
      doc_type: 'reference',
      source_org: '标准组',
      version: 'v1',
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
      created_at: '2026-08-12T08:00:00Z'
    }],
    documents: [{
      id: 71,
      name: '户外数据标准',
      doc_type: 'reference',
      source_org: '标准组',
      version: 'v1',
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

async function installMockBackend(page, options = {}) {
  const deleteRequests = []
  const actionRequests = []
  let metricDocumentListRequests = 0
  let metricDocumentLinked = false
  const documents = (options.documents || []).map(item => ({ ...item }))
  const elements = (options.elements || []).map(item => ({ ...item }))
  const metrics = (options.metrics || []).map(item => ({ ...item }))
  const hierarchy = options.hierarchy
    ? { ...options.hierarchy, levels: options.hierarchy.levels.map(item => ({ ...item })) }
    : null
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (request.method() === 'DELETE' && path === '/api/v1/standard/domains/2' && options.domainDeleteConflict) {
      deleteRequests.push(path)
      return fulfillJSON(route, {
        error: '业务域仍被子业务域、业务术语、数据元、指标或维度层级引用，无法删除'
      }, 409)
    }
    if (request.method() === 'DELETE' && path === '/api/v1/standard/dimension-hierarchies/61/levels/612') {
      actionRequests.push(path)
      const index = hierarchy.levels.findIndex(level => level.id === 612)
      if (index >= 0) hierarchy.levels.splice(index, 1)
      return fulfillJSON(route, {})
    }
    if (request.method() === 'DELETE') {
      deleteRequests.push(path)
      return fulfillJSON(route, {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/elements/41/approve') {
      actionRequests.push(path)
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
      return fulfillJSON(route, level)
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/documents') {
      actionRequests.push(path)
      const document = {
        id: 72,
        name: request.postDataJSON().name,
        doc_type: request.postDataJSON().doc_type,
        source_org: request.postDataJSON().source_org,
        version: request.postDataJSON().version,
        file_name: null,
        created_at: '2026-08-12T08:00:00Z'
      }
      documents.push(document)
      return fulfillJSON(route, document)
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
      return fulfillJSON(route, document || {})
    }
    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'standard-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'standard-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, { context: { type: 'tenant' }, authorization: { role_assignments: [] } })
    }
    if (path === '/api/v1/standard/domains') return fulfillJSON(route, domains)
    if (path === '/api/v1/standard/glossaries') {
      const domainID = Number(url.searchParams.get('domain_id'))
      const data = domainID ? glossaries.filter(item => item.domain_id === domainID) : glossaries
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/standard/glossaries/21') return fulfillJSON(route, glossaries[0])
    if (path === '/api/v1/standard/glossaries/21/elements') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/glossaries/21/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: elements, total: elements.length })
    if (path === '/api/v1/standard/code-sets') {
      return fulfillJSON(route, {
        data: [{ id: 31, code: 'gender', name: '性别', type: 'custom', description: '性别码值', created_at: '2026-08-12T08:00:00Z' }],
        total: 1
      })
    }
    if (path === '/api/v1/standard/code-sets/31') {
      return fulfillJSON(route, { id: 31, code: 'gender', name: '性别', type: 'custom', description: '性别码值' })
    }
    if (path === '/api/v1/standard/code-sets/31/items') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/measurement-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/units') return fulfillJSON(route, { data: [], total: 0 })
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
    if (path === '/api/v1/standard/dimension-hierarchies') return fulfillJSON(route, hierarchy ? [hierarchy] : [])
    if (path === '/api/v1/standard/dimension-hierarchies/61') return fulfillJSON(route, hierarchy || {})

    return fulfillJSON(route, {})
  })

  return {
    getActionRequests: () => [...actionRequests],
    getDeleteRequests: () => [...deleteRequests],
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
