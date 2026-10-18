import { expect, test } from '@playwright/test'

const entryID = '00000000-0000-4000-8000-000000000051'
const currentID = '9007199254740993'
const adjacentID = '9007199254740992'
const entryURL = `/catalog/entries/${entryID}?tab=sharing`
const cases = [
  { type: 'user', id: currentID, name: '目录业务负责人', code: 'catalog-owner', status: 'resolved',
    label: '账号 · 目录业务负责人 · catalog-owner (当前账号)' },
  { type: 'user', id: adjacentID, name: '目录读取人员', code: 'catalog-reader', status: 'resolved',
    label: '账号 · 目录读取人员 · catalog-reader' },
  { type: 'project_group', id: currentID, name: '目录协作组', code: 'catalog-team', status: 'resolved',
    label: '项目组 · 目录协作组 · catalog-team' },
  { type: 'user', id: '73', name: '已删除的旧名称', code: 'stale-deleted', status: 'not_found',
    label: '账号 · 接收方已不存在' },
  { type: 'project_group', id: '74', name: '无法解析的旧名称', code: 'stale-unavailable', status: 'unavailable',
    label: '项目组 · 接收方名称暂时无法解析' }
]
const decisions = cases.map((item, index) => ({
  id: `00000000-0000-4000-8000-00000000006${index}`,
  recipient_type: item.type, recipient_id: item.id, recipient_name: item.name,
  recipient_code: item.code, recipient_label_status: item.status,
  created_at: '2026-10-17T02:26:58Z', expiry_mode: 'until_revoked', expires_at: null,
  reason: '接收方名称展示验收', target: { engine_id: '2', version: 'v1', segments: [{ name: 'outdoor' }, { name: 'activities' }] }
}))

async function openSharing(page, { withoutHistoryCodes = false } = {}) {
  const unexpected = [], writes = [], errors = [], reads = []
  const history = withoutHistoryCodes ? decisions.map(({ recipient_code, ...row }) => row) : decisions
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'catalog-sharing-owner-token', expires_in: 900 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: {
      id: currentID, username: 'catalog-owner', display_name: '目录业务负责人'
    } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { id: currentID, type: 'user' },
      context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' },
        permissions: ['catalog.entry.read', 'catalog.sharing_decision.create'] }] }
    } })
    // Inspecting recipients and existing approvals must not submit a business command.
    if (request.method() !== 'GET') writes.push(`${request.method()} ${path}`)
    const suffix = path.startsWith(`/api/v1/catalog/entries/${entryID}`)
      ? path.slice(`/api/v1/catalog/entries/${entryID}`.length) : null
    if (suffix !== null) {
      reads.push({ suffix, params: Object.fromEntries(url.searchParams), authorization: request.headers().authorization })
      if (suffix === '') return route.fulfill({ json: {
        id: entryID, display_name: '共享接收方展示验收', entry_type: 'data_item',
        entry_status: 'active', governance_status: 'curated', visibility: 'tenant', version: 3,
        semantic_links: [], components: [], responsibilities: [
          { role: 'business_owner', subject_type: 'user', subject_id: currentID, status: 'active' }
        ], source: { source_module: 'meta', source_type: 'table', source_identity: 'a'.repeat(64), source_status: 'active' }
      } })
      if (suffix === '/data-dictionary') return route.fulfill({ json: {
        as_of: '2026-10-17T02:26:58Z', generated_at: '2026-10-17T02:26:58Z', fields: []
      } })
      if (suffix === '/sharing_decisions') return route.fulfill({ json: { data: history, total: history.length } })
      if (suffix === '/sharing_recipient_candidates') {
        const type = url.searchParams.get('recipient_type'), search = url.searchParams.get('search') || ''
        const options = cases.filter(item => item.status === 'resolved' && item.type === type && `${item.name} ${item.code}`.includes(search))
          .map(item => ({ id: item.id, recipient_type: item.type, name: item.name, code: item.code, status: 'active' }))
        return route.fulfill({ json: { data: options, total: options.length } })
      }
      const decision = history.find(item => suffix === `/sharing_decisions/${item.id}`)
      if (decision) return route.fulfill({ json: decision })
      if (decisions.some(item => suffix === `/sharing_decisions/${item.id}/fulfillments`)) {
        return route.fulfill({ json: { data: [], total: 0 } })
      }
    }
    if (path === `/api/v1/catalog/me/entries/${entryID}/marks`) return route.fulfill({ json: { favorite: false, following: false } })
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_request' } })
  })
  await page.goto(entryURL)
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(catalog.getByTestId('catalog-entry-detail')).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('sharing-confirmations').locator('.el-table__body tbody tr')).toHaveCount(cases.length)
  return { catalog, reads, verifyNoCommands() {
    expect(writes).toEqual([])
    expect(unexpected).toEqual([])
    expect(errors).toEqual([])
    expect(reads.length).toBeGreaterThan(0)
    expect(reads.every(read => read.authorization === 'Bearer catalog-sharing-owner-token')).toBe(true)
  } }
}

test('Catalog current account remains readable when historical recipient codes are absent', async ({ page }) => {
  const fixture = await openSharing(page, { withoutHistoryCodes: true }), { catalog } = fixture
  const nameOnly = '账号 · 目录业务负责人 (当前账号)'
  const row = catalog.getByTestId('sharing-confirmations').locator('.el-table__body tbody tr').first()
  await expect(row.locator('td').first()).toHaveText(nameOnly)
  await row.getByTestId('sharing-open-confirmation').click()
  const recipient = catalog.getByTestId('sharing-confirmation-detail').locator('td.el-descriptions__label')
    .filter({ hasText: /^接收方$/ }).locator('xpath=following-sibling::td[1]')
  await expect(recipient).toHaveText(nameOnly)
  await catalog.getByTestId('sharing-workspace').getByText('批准数据使用', { exact: true }).click()
  await catalog.getByTestId('sharing-create-panel').locator('.el-select').nth(1).click()
  await expect(catalog.getByRole('option', { name: cases[0].label, exact: true })).toBeVisible()
  fixture.verifyNoCommands()
})

test('Catalog recipient options show names and mark only the exact current user across recipient type changes', async ({ page }) => {
  const fixture = await openSharing(page), { catalog } = fixture
  await catalog.getByTestId('sharing-workspace').getByText('批准数据使用', { exact: true }).click()
  const form = catalog.getByTestId('sharing-create-panel'), selects = form.locator('.el-select')
  await selects.nth(1).click()
  const current = catalog.getByRole('option', { name: cases[0].label, exact: true })
  await expect(current).toBeVisible()
  await expect(catalog.getByRole('option', { name: cases[1].label, exact: true })).toBeVisible()
  await expect(catalog.getByRole('option').filter({ hasText: '当前账号' })).toHaveCount(1)
  await expect(catalog.getByRole('listbox')).not.toContainText(currentID)
  await expect(catalog.getByRole('listbox')).not.toContainText(adjacentID)
  await current.click()
  await expect(selects.nth(1)).toContainText(cases[0].label)
  await selects.nth(0).click()
  await catalog.getByRole('option', { name: '项目组', exact: true }).click()
  await expect(selects.nth(1)).toContainText('按名称或编码查找并选择接收方')
  await selects.nth(1).click()
  const group = catalog.getByRole('option', { name: cases[2].label, exact: true })
  await expect(group).toBeVisible()
  await expect(catalog.getByRole('option').filter({ hasText: '当前账号' })).toHaveCount(0)
  await group.click()
  await expect(selects.nth(1)).toContainText(cases[2].label)
  await selects.nth(0).click()
  await catalog.getByRole('option', { name: '账号', exact: true }).click()
  await expect(selects.nth(1)).toContainText('按名称或编码查找并选择接收方')
  await selects.nth(1).click()
  await selects.nth(1).getByRole('combobox').fill('catalog-reader')
  await expect(catalog.getByRole('option', { name: cases[1].label, exact: true })).toBeVisible()
  await expect(catalog.getByRole('option', { name: cases[0].label, exact: true })).toHaveCount(0)
  expect(fixture.reads.some(read => read.suffix === '/sharing_recipient_candidates' &&
    read.params.recipient_type === 'user' && read.params.search === 'catalog-reader' && read.params.page === '1' && read.params.page_size === '50')).toBe(true)
  fixture.verifyNoCommands()
})

for (const [index, item] of cases.entries()) {
  test(`Catalog approval ${item.type}/${item.status}/${index} retains its recipient label in records, detail and URL reload`, async ({ page }) => {
    const fixture = await openSharing(page), { catalog } = fixture
    const records = catalog.getByTestId('sharing-confirmations')
    const row = records.locator('.el-table__body tbody tr').nth(index)
    const recipient = row.locator('td').first()
    await expect(recipient).toHaveText(item.label)
    await expect(recipient).not.toContainText(item.id)
    if (item.status !== 'resolved') await expect(recipient).not.toContainText(item.name)
    await row.getByTestId('sharing-open-confirmation').click()
    const detail = catalog.getByTestId('sharing-confirmation-detail')
    const detailRecipient = detail.locator('td.el-descriptions__label').filter({ hasText: /^接收方$/ })
      .locator('xpath=following-sibling::td[1]')
    for (let attempt = 0; attempt < 3; attempt++) {
      await expect(detailRecipient).toHaveText(item.label)
      await expect(detailRecipient).not.toContainText(item.id)
      if (item.status !== 'resolved') await expect(detailRecipient).not.toContainText(item.name)
      await expect(page).toHaveURL(new RegExp(`sharing_decision_id=${decisions[index].id}`))
      if (attempt === 0) {
        const before = fixture.reads.length
        await detail.getByTestId('sharing-query-confirmation').click()
        await expect.poll(() => fixture.reads.slice(before).some(read => read.suffix === `/sharing_decisions/${decisions[index].id}`)).toBe(true)
      }
      if (attempt === 1) await page.reload()
    }
    await catalog.getByTestId('sharing-back-records').click()
    await expect(page).toHaveURL(entryURL)
    await catalog.getByTestId('sharing-refresh-history').click()
    await expect(recipient).toHaveText(item.label)
    fixture.verifyNoCommands()
  })
}
