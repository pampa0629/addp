import { expect, test } from '@playwright/test'

const hostURL = '/e2e/fixtures/leave-guard.html?editor=query-service'
const draftSQL = 'SELECT id FROM fixture_rows WHERE category = :category'
const dialog = owner => owner.getByRole('dialog', { name: '有未保存的修改' })

function savedService(id, version = 3) {
  return {
    id, version, service_name: `query_fixture_${id}`, title: `Saved service ${id} v${version}`,
    description: `Description ${id}`, keywords: [`keyword-${id}`], public_access: true,
    max_features: 1000, config_type: 'sql', engine_id: 1, sql_query: draftSQL,
    stable_key: ['id'], protocols: { rest_api: { enabled: true }, ogc_features: { enabled: false } }
  }
}

async function openForm(page, { editId } = {}) {
  const writes = []
  let saveStatus = 200
  const services = new Map([41, 42].map(id => [id, savedService(id)]))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/meta/engines') return route.fulfill({ json: [] })
    if (path === '/api/v1/system/engines') return route.fulfill({ json: [
      { id: 1, name: 'fixture-postgres', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online', capabilities: { compute: { query: { supported: true, languages: ['sql'] } } } }
    ] })
    if (path === '/api/v1/service/query-engines/1/sample-query') return route.fulfill({ json: { language: 'sql', query: draftSQL } })
    if (path === '/api/v1/service/sql/output-contract') return route.fulfill({ json: { table: { fields: [{ name: 'id', type: 'int' }], primary_key: ['id'] } } })
    const serviceId = path.match(/^\/api\/v1\/service\/query\/(41|42)$/)?.[1]
    if (serviceId && route.request().method() === 'GET') return route.fulfill({ json: services.get(Number(serviceId)) })
    if (serviceId && route.request().method() === 'PUT') {
      const update = route.request().postDataJSON()
      writes.push({ id: Number(serviceId), ...update })
      return route.fulfill({ status: saveStatus, json: saveStatus === 200
        ? { ...services.get(Number(serviceId)), ...update, version: update.version + 1 }
        : { error_code: 'resource_version_conflict', error: 'fixture version conflict' } })
    }
    if (path === '/api/v1/service/query' && route.request().method() === 'POST') {
      writes.push(route.request().postDataJSON())
      return route.fulfill({ status: saveStatus, json: saveStatus === 200 ? { id: 41 } : { error: 'fixture save failed' } })
    }
    await route.abort()
    throw new Error(`Unexpected API request: ${route.request().method()} ${path}`)
  })
  await page.goto(`${hostURL}#${editId ? `/service/query-services/${editId}/edit` : '/'}`)
  if (!editId) await page.getByRole('button', { name: 'Open editor', exact: true }).click()
  const editor = page.frameLocator('iframe')
  await expect(editor.getByRole('heading', { name: editId ? '编辑查询服务' : '创建查询服务', exact: true })).toBeVisible()
  if (editId) await expect(editor.getByPlaceholder('例如: 北京POI数据查询', { exact: true })).toHaveValue(services.get(editId).title)
  return { editor, writes, services, conflictSaving: () => { saveStatus = 409 }, failSaving: () => { saveStatus = 500 }, allowSaving: () => { saveStatus = 200 } }
}

async function enterSQLDraft(editor, { selectEngine = false } = {}) {
  await editor.getByRole('heading', { name: 'SQL 配置模式（适合高级用户）', exact: true }).click()
  await editor.getByRole('button', { name: '下一步', exact: true }).click()
  if (selectEngine) {
    await editor.getByText('请选择查询引擎', { exact: true }).click()
    await editor.getByRole('option', { name: /fixture-postgres/ }).click()
    await expect(editor.getByRole('textbox', { name: /SQL 查询语句/ })).toHaveValue(draftSQL)
  } else {
    await editor.getByRole('textbox', { name: /SQL 查询语句/ }).fill(draftSQL)
  }
  await editor.getByRole('button', { name: '添加命名参数', exact: true }).click()
  await editor.getByPlaceholder('参数名', { exact: true }).fill('category')
  await editor.locator('.named-parameter-row').getByRole('textbox').nth(1).fill('demo')
  await editor.getByPlaceholder('参数说明', { exact: true }).fill('Unsaved category description')
}

async function expectDraft(page, editor) {
  await expect(page).toHaveURL(/#\/service\/query-services\/create$/)
  await expect(editor.getByRole('textbox', { name: /SQL 查询语句/ })).toHaveValue(draftSQL)
  await expect(editor.getByPlaceholder('参数名', { exact: true })).toHaveValue('category')
  await expect(editor.locator('.named-parameter-row').getByRole('textbox').nth(1)).toHaveValue('demo')
  await expect(editor.getByPlaceholder('参数说明', { exact: true })).toHaveValue('Unsaved category description')
}

test('real query editor preserves SQL and parameters when internal leave is cancelled; discard synchronizes once', async ({ page }) => {
  const { editor, writes } = await openForm(page)
  await enterSQLDraft(editor)
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog(editor)).toBeVisible()
  await expect(dialog(page)).toHaveCount(0)
  await dialog(editor).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(editor)).toHaveCount(0)
  await expectDraft(page, editor)
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await dialog(editor).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/#\/service\/query-services$/)
  await expect(editor.getByRole('heading', { name: 'Query service list' })).toBeVisible()
  await expect(dialog(page)).toHaveCount(0)
  expect(writes).toEqual([])
})

test('real query editor protects Console menu and back navigation; forward opens a clean draft', async ({ page }) => {
  const { editor } = await openForm(page)
  await enterSQLDraft(editor)
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(dialog(page)).toBeVisible()
  await expect(dialog(editor)).toHaveCount(0)
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expectDraft(page, editor)
  await page.evaluate(() => history.back())
  await expect(dialog(page)).toBeVisible()
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expectDraft(page, editor)
  await page.evaluate(() => history.back())
  await dialog(page).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/#\/$/)
  await page.goForward()
  await expect(page).toHaveURL(/#\/service\/query-services\/create$/)
  await expect(editor.getByRole('radio', { name: /^表配置模式/ })).toBeChecked()
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(page).toHaveURL(/#\/other$/)
  await expect(dialog(page)).toHaveCount(0)
})

test('real query editor cancels browser refresh without losing SQL or named parameters', async ({ page }) => {
  const { editor } = await openForm(page)
  await enterSQLDraft(editor)
  const pendingDialog = page.waitForEvent('dialog')
  await page.evaluate(() => { setTimeout(() => location.reload(), 0) })
  const unload = await pendingDialog
  expect(unload.type()).toBe('beforeunload')
  await unload.dismiss()
  await expectDraft(page, editor)
})

test('query wizard steps alone do not mark an empty form dirty', async ({ page }) => {
  const { editor } = await openForm(page)
  await editor.getByRole('button', { name: '下一步', exact: true }).click()
  await editor.getByRole('button', { name: '上一步', exact: true }).click()
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(page).toHaveURL(/#\/other$/)
  await expect(dialog(page)).toHaveCount(0)
})

test('real query submission preserves input on failure and clears protection only after successful save', async ({ page }) => {
  const h = await openForm(page)
  await enterSQLDraft(h.editor, { selectEngine: true })
  await h.editor.getByText('展开后自动检测并列出当前 SQL 的输出字段', { exact: true }).click()
  await h.editor.getByRole('option', { name: 'id', exact: true }).waitFor()
  // The real output detector selects the primary key returned by the fixture.
  await h.editor.getByRole('combobox', { name: /稳定排序键/ }).press('Escape')
  await h.editor.getByRole('button', { name: '下一步', exact: true }).click()
  await h.editor.getByPlaceholder('例如: beijing_poi', { exact: true }).fill('query_draft_fixture')
  await h.editor.getByPlaceholder('例如: 北京POI数据查询', { exact: true }).fill('Query draft fixture')
  h.failSaving()
  await h.editor.getByRole('button', { name: '创建', exact: true }).click()
  await expect(h.editor.getByText(/fixture save failed/)).toBeVisible()
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(dialog(page)).toBeVisible()
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(h.editor.getByPlaceholder('例如: 北京POI数据查询', { exact: true })).toHaveValue('Query draft fixture')
  h.allowSaving()
  await h.editor.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page).toHaveURL(/#\/service\/query-services\/41$/)
  await expect(h.editor.getByRole('heading', { name: 'Query service detail' })).toBeVisible()
  await expect(dialog(page)).toHaveCount(0)
  await expect(dialog(h.editor)).toHaveCount(0)
  expect(h.writes).toHaveLength(2)
  expect(h.writes[1]).toMatchObject({ sql_query: draftSQL, engine_id: 1, data_config: { stable_key: ['id'] }, named_parameters: [{ name: 'category', type: 'string', required: true, description: 'Unsaved category description' }] })
  page.on('dialog', () => { throw new Error('Saved query editor must not block refresh') })
  await page.reload()
  await expect(page.frameLocator('iframe').getByRole('heading', { name: 'Query service detail' })).toBeVisible()
})

test('existing query conflict retains edits until confirmed reload and saves with the refreshed version', async ({ page }) => {
  const h = await openForm(page, { editId: 41 })
  const title = h.editor.getByPlaceholder('例如: 北京POI数据查询', { exact: true })
  const description = h.editor.getByPlaceholder('服务的简要描述', { exact: true })
  await title.fill('Unsaved conflict title')
  await description.fill('Unsaved conflict description')
  h.conflictSaving()
  await h.editor.getByRole('button', { name: '更新', exact: true }).click()
  await expect(h.editor.locator('.el-alert').filter({ hasText: '服务已被其他用户修改' })).toBeVisible()
  expect(h.writes).toHaveLength(1)
  expect(h.writes[0]).toMatchObject({ id: 41, version: 3, title: 'Unsaved conflict title', description: 'Unsaved conflict description' })

  await page.getByRole('button', { name: 'Other page' }).click()
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  const reloadDialog = h.editor.getByRole('dialog', { name: '重新加载', exact: true })
  await h.editor.getByRole('button', { name: '重新加载', exact: true }).click()
  await reloadDialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(reloadDialog).toHaveCount(0)
  await expect(title).toHaveValue('Unsaved conflict title')
  await expect(description).toHaveValue('Unsaved conflict description')

  // The accepted reload must not trigger a second native/Console leave confirmation.
  page.on('dialog', () => { throw new Error('Confirmed reload must not ask again') })
  h.services.set(41, savedService(41, 4))
  await h.editor.getByRole('button', { name: '重新加载', exact: true }).click()
  await reloadDialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(title).toHaveValue('Saved service 41 v4')
  await expect(description).toHaveValue('Description 41')
  await expect(h.editor.locator('.el-alert').filter({ hasText: '服务已被其他用户修改' })).toHaveCount(0)
  await expect(dialog(page)).toHaveCount(0)
  await expect(dialog(h.editor)).toHaveCount(0)
  expect(h.writes).toHaveLength(1)
  h.allowSaving()
  await title.fill('Updated after reload')
  await h.editor.getByRole('button', { name: '更新', exact: true }).click()
  await expect(page).toHaveURL(/#\/service\/query-services$/)
  await expect(h.editor.getByRole('heading', { name: 'Query service list' })).toBeVisible()
  expect(h.writes).toHaveLength(2)
  expect(h.writes[1]).toMatchObject({ id: 41, version: 4, title: 'Updated after reload', description: 'Description 41' })
  await expect(dialog(page)).toHaveCount(0)
})

test('reused query editor cancels identity changes or discards once and starts a clean target draft', async ({ page }) => {
  const h = await openForm(page, { editId: 41 })
  const title = h.editor.getByPlaceholder('例如: 北京POI数据查询', { exact: true })
  await title.evaluate(element => element.setAttribute('data-reused-editor', 'yes'))
  await title.fill('Unsaved service 41')
  await h.editor.getByRole('button', { name: 'Edit service 42', exact: true }).click()
  await expect(dialog(h.editor)).toBeVisible()
  await expect(dialog(page)).toHaveCount(0)
  await dialog(h.editor).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(h.editor)).toHaveCount(0)
  await expect(page).toHaveURL(/#\/service\/query-services\/41\/edit$/)
  await expect(title).toHaveValue('Unsaved service 41')
  await h.editor.getByRole('button', { name: 'Edit service 42', exact: true }).click()
  await dialog(h.editor).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/#\/service\/query-services\/42\/edit$/)
  await expect(title).toHaveValue('Saved service 42 v3')
  await expect(title).toHaveAttribute('data-reused-editor', 'yes')
  await expect(h.editor.getByPlaceholder('例如: beijing_poi', { exact: true })).toHaveValue('query_fixture_42')
  await expect(h.editor.getByPlaceholder('服务的简要描述', { exact: true })).toHaveValue('Description 42')
  await expect(dialog(page)).toHaveCount(0)
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(page).toHaveURL(/#\/other$/)
  await expect(dialog(page)).toHaveCount(0)
  expect(h.writes).toEqual([])
})

test('late service definition cannot overwrite the current reused query editor or its dirty state', async ({ page }) => {
  const h = await openForm(page, { editId: 41 })
  const title = h.editor.getByPlaceholder('例如: 北京POI数据查询', { exact: true })
  let releaseResponse
  const pendingResponse = new Promise(resolve => { releaseResponse = resolve })
  await page.route('**/api/v1/service/query/42', async route => {
    await pendingResponse
    await route.fulfill({ json: savedService(42) })
  })
  try {
    const requested = page.waitForRequest('**/api/v1/service/query/42')
    await h.editor.getByRole('button', { name: 'Edit service 42', exact: true }).click()
    await requested
    await expect(page).toHaveURL(/#\/service\/query-services\/42\/edit$/)
    await h.editor.getByRole('button', { name: 'Edit service 41', exact: true }).click()
    await expect(page).toHaveURL(/#\/service\/query-services\/41\/edit$/)
    await expect(title).toHaveValue('Saved service 41 v3')
    await title.fill('New draft after switching back')
    const received = page.waitForResponse('**/api/v1/service/query/42')
    releaseResponse()
    await (await received).finished()
    // Flush the browser's response callback and Vue rendering before asserting isolation.
    await title.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await expect(title).toHaveValue('New draft after switching back')
    await expect(h.editor.getByPlaceholder('例如: beijing_poi', { exact: true })).toHaveValue('query_fixture_41')
    await page.getByRole('button', { name: 'Other page' }).click()
    await dialog(page).getByRole('button', { name: '继续编辑' }).click()
    await expect(dialog(page)).toHaveCount(0)
    await expect(title).toHaveValue('New draft after switching back')
    expect(h.writes).toEqual([])
  } finally {
    releaseResponse()
    await page.unrouteAll({ behavior: 'wait' })
  }
})
