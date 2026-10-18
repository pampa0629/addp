import { expect, test } from '@playwright/test'

const entryID = '00000000-0000-4000-8000-000000000081'
const sharedID = '9007199254740993'
const unavailable = { 'zh-cn': '已保存引用（当前名称不可用）', en: 'Saved reference (current name unavailable)' }

async function openReferenceView(page, view = 'editor') {
  const writes = [], unexpected = [], errors = [], searches = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'catalog-editor-reference-token', expires_in: 900 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: sharedID, username: 'outdoor-develop', display_name: '户外开发者' } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { id: sharedID, type: 'user' }, context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions: ['catalog.entry.read', 'catalog.entry.update', 'catalog.entry.deprecate'] }] }
    } })
    if (request.method() !== 'GET') writes.push(`${request.method()} ${path}`)
    if (request.headers().authorization !== 'Bearer catalog-editor-reference-token') errors.push(`missing user token: ${path}`)
    if (path === `/api/v1/catalog/entries/${entryID}`) return route.fulfill({ json: {
      id: entryID, display_name: '编目引用名称验收', business_name: '户外活动', business_description: '编目引用名称与语言切换验收',
      entry_type: 'data_item', entry_status: 'active', governance_status: view === 'successor' ? 'deprecated' : 'curated', visibility: 'tenant', version: 3,
      ...(view === 'successor' ? { recommended_successor_entry_id: '00000000-0000-4000-8000-000000000082' } : {}),
      semantic_links: [
        { semantic_type: 'domain', semantic_id: sharedID, relation_role: 'primary', observed_snapshot: {} },
        { semantic_type: 'glossary', semantic_id: '9007199254740997', relation_role: 'related', observed_snapshot: {} }
      ],
      responsibilities: [
        { role: 'accountable_department', subject_type: 'department', subject_id: sharedID, status: 'active', observed_snapshot: { name: '户外部', code: 'outdoor' } },
        { role: 'business_owner', subject_type: 'user', subject_id: sharedID, status: 'active', observed_snapshot: { name: '户外开发者', code: 'outdoor-develop' } },
        { role: 'data_steward', subject_type: 'user', subject_id: '9007199254740995', status: 'active', observed_snapshot: {} }
      ],
      components: [{ id: '00000000-0000-4000-8000-000000000083', display_name: 'activity_id', component_status: 'active' }],
      standard_mappings: view === 'mapping' ? [{ id: '00000000-0000-4000-8000-000000000084', version: 1,
        component_id: '00000000-0000-4000-8000-000000000083', element_id: sharedID, review_status: 'proposed', evidence: {} }] : [],
      source: { source_module: 'meta', source_type: 'data_item', source_identity: 'b'.repeat(64), source_status: 'active', observed_snapshot: {} }
    } })
    if (path === `/api/v1/catalog/me/entries/${entryID}/marks`) return route.fulfill({ json: { favorite: false, following: false } })
    if (path === `/api/v1/catalog/entries/${entryID}/data-dictionary`) return route.fulfill({ json: { as_of: '2026-10-18T00:00:00Z', generated_at: '2026-10-18T00:00:00Z', fields: [] } })
    if (path === '/api/v1/catalog/governance/tasks' || path === '/api/v1/catalog/entries') return route.fulfill({ json: { data: [], total: 0, page: 1, page_size: 20, total_pages: 0 } })
    if (path === '/api/v1/catalog/standard-mappings/revision-options') return route.fulfill({ json: [] })
    if (path === '/api/v1/catalog/reference-candidates') {
      searches.push({ type: url.searchParams.get('reference_type'), search: url.searchParams.get('search') || '' })
      return route.fulfill({ json: { data: [], total: 0, page: 1, page_size: 20, total_pages: 0 } })
    }
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_request' } })
  })
  await page.goto(view === 'tasks' ? `/catalog/governance/tasks?entry_id=${entryID}` : `/catalog/entries/${entryID}?tab=curation`)
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  if (view !== 'tasks') await expect(catalog.getByTestId('catalog-entry-detail')).toHaveAttribute('data-load-state', 'loaded')
  if (view === 'editor') await catalog.getByTestId('catalog-curation-action').click()
  if (view === 'mapping') await catalog.locator('.mapping-card').getByRole('button', { name: '编辑候选', exact: true }).click()
  if (view === 'successor') await catalog.getByTestId('catalog-primary-governance-action').click()
  const editor = catalog.locator('.editor-card')
  if (view === 'editor') await expect(editor).toBeVisible()
  return { catalog, editor, searches, verify() {
    expect(writes).toEqual([])
    expect(unexpected).toEqual([])
    expect(errors).toEqual([])
  } }
}

test('Catalog editor preserves readable saved references across searches without confusing equal department and user IDs', async ({ page }) => {
  const fixture = await openReferenceView(page), { editor, catalog } = fixture
  const department = editor.locator('.responsibility-row').nth(0).locator('.el-select').nth(1)
  const owner = editor.locator('.responsibility-row').nth(1).locator('.el-select').nth(1)
  await expect(department.locator('.el-select__selected-item.el-select__placeholder')).toHaveText('户外部 · outdoor')
  await expect(owner.locator('.el-select__selected-item.el-select__placeholder')).toHaveText('户外开发者 · outdoor-develop')
  await owner.getByRole('combobox').fill('没有匹配的账号')
  await expect.poll(() => fixture.searches.some(item => item.type === 'user' && item.search === '没有匹配的账号')).toBe(true)
  await owner.getByRole('combobox').press('Escape')
  await expect(owner.locator('.el-select__selected-item.el-select__placeholder')).toHaveText('户外开发者 · outdoor-develop')
  await expect(department.locator('.el-select__selected-item.el-select__placeholder')).toHaveText('户外部 · outdoor')
  await expect(editor.locator('.id-row .el-select__selected-item.el-select__placeholder')).toHaveText(unavailable['zh-cn'])
  await expect(editor.locator('.responsibility-row').nth(2).locator('.el-select').nth(1).locator('.el-select__selected-item.el-select__placeholder')).toHaveText(unavailable['zh-cn'])
  await expect(editor).not.toContainText(sharedID)
  await expect(catalog.getByTestId('catalog-entry-detail')).toHaveAttribute('data-entry-id', entryID)
  fixture.verify()
})

test('Catalog editor updates unavailable saved-reference labels when Console language changes without losing the draft', async ({ page }) => {
  const fixture = await openReferenceView(page), { editor, catalog } = fixture
  const missingGlossary = editor.locator('.id-row .el-select__selected-item.el-select__placeholder')
  const missingSteward = editor.locator('.responsibility-row').nth(2).locator('.el-select').nth(1).locator('.el-select__selected-item.el-select__placeholder')
  const missingDomain = editor.locator('.domain-row .el-select').nth(0).locator('.el-select__placeholder')
  const name = editor.locator('input[maxlength="200"]')
  await name.fill('未保存的业务名称')
  const origin = await catalog.locator('body').evaluate(() => performance.timeOrigin)
  await expect(missingGlossary).toHaveText(unavailable['zh-cn'])
  await expect(missingSteward).toHaveText(unavailable['zh-cn'])
  await expect(missingDomain).toHaveText(unavailable['zh-cn'])
  await page.getByRole('button', { name: 'CN', exact: true }).click()
  await page.getByRole('menuitem', { name: 'English', exact: true }).click()
  await expect(missingGlossary).toHaveText(unavailable.en)
  await expect(missingSteward).toHaveText(unavailable.en)
  await expect(missingDomain).toHaveText(unavailable.en)
  await expect(name).toHaveValue('未保存的业务名称')
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await page.getByRole('menuitem', { name: '中文', exact: true }).click()
  await expect(missingGlossary).toHaveText(unavailable['zh-cn'])
  await expect(missingSteward).toHaveText(unavailable['zh-cn'])
  await expect(missingDomain).toHaveText(unavailable['zh-cn'])
  await expect(name).toHaveValue('未保存的业务名称')
  expect(await catalog.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  fixture.verify()
})

for (const view of ['mapping', 'successor', 'tasks']) {
  test(`Catalog ${view} updates unresolved selected-reference labels with Console language without exposing IDs`, async ({ page }) => {
    const fixture = await openReferenceView(page, view), { catalog } = fixture
    const selected = view === 'tasks'
      ? catalog.locator('.filter-card .el-select').nth(1).locator('.el-select__placeholder')
      : catalog.getByRole('dialog').locator('.el-select').nth(view === 'mapping' ? 1 : 0).locator('.el-select__placeholder')
    const expected = view === 'mapping'
      ? { 'zh-cn': '数据元信息不可用', en: 'Data element information unavailable' } : unavailable
    await expect(selected).toHaveText(expected['zh-cn'])
    const origin = await catalog.locator('body').evaluate(() => performance.timeOrigin)
    await page.getByRole('button', { name: 'CN', exact: true }).click()
    await page.getByRole('menuitem', { name: 'English', exact: true }).click()
    await expect(selected).toHaveText(expected.en)
    await page.getByRole('button', { name: 'EN', exact: true }).click()
    await page.getByRole('menuitem', { name: '中文', exact: true }).click()
    await expect(selected).toHaveText(expected['zh-cn'])
    expect(await catalog.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
    fixture.verify()
  })
}
