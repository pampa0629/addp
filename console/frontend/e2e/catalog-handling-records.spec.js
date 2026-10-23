import { expect, test } from '@playwright/test'

const entryID = '00000000-0000-4000-8000-000000000091'
const decisionID = '00000000-0000-4000-8000-000000000092'
const id = index => `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
const rows = [
  { state: 'accepted', granted_at: '2026-10-21T11:45:00Z' },
  { state: 'accepted' }, { state: 'pending' },
  { state: 'accepted', unissued_reason: 'relation_exists' },
  { state: 'accepted', unissued_reason: 'window_expired' }, { state: 'closed' }
].map((outcome, index) => ({
  request_id: id(index + 100), decision_id: decisionID, requirement_version: '9007199254740993',
  recipient_type: index === 3 ? 'project_group' : 'user', recipient_id: '9007199254740993',
  recipient_name: index === 3 ? '户外分析项目组' : '户外开发者', recipient_code: index === 3 ? 'outdoor-analysis' : 'outdoor-develop',
  recipient_label_status: 'resolved', action: 'read', expiry_mode: 'until_revoked', expires_at: null,
  target: { engine_id: '12', version: 'v1', segments: [{ name: 'outdoor' }, { name: 'activities' }] },
  created_at: '2026-10-21T11:44:18Z', recorded_at: '2026-10-21T11:44:20Z', deadline: '2026-10-21T11:49:20Z',
  ...outcome
}))

async function openRecords(page, { fail = false } = {}) {
  const writes = [], unexpected = [], errors = [], details = [], requests = []
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'catalog-handling-records-token', expires_in: 900 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: '9007199254740993', username: 'outdoor-develop', display_name: '户外开发者' } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { id: '9007199254740993', type: 'user' }, context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions: ['catalog.entry.read', 'system.engine_access_fulfillment.create'] }] }
    } })
    if (request.method() !== 'GET') writes.push(`${request.method()} ${path}`)
    if (request.headers().authorization !== 'Bearer catalog-handling-records-token') errors.push(`wrong user token: ${path}`)
    if (path === `/api/v1/catalog/entries/${entryID}`) return route.fulfill({ json: {
      id: entryID, display_name: '户外活动', entry_type: 'data_item', entry_status: 'active', governance_status: 'curated', visibility: 'tenant', version: 3,
      source: { source_module: 'meta', source_status: 'active', source_identity: 'b'.repeat(64), observed_snapshot: {} }, responsibilities: [], semantic_links: [], components: []
    } })
    if (path === `/api/v1/catalog/me/entries/${entryID}/marks`) return route.fulfill({ json: { favorite: false, following: false } })
    if (path === `/api/v1/catalog/entries/${entryID}/data-dictionary`) return route.fulfill({ json: { fields: [] } })
    if (path === `/api/v1/catalog/entries/${entryID}/sharing_fulfillments`) {
      requests.push(url.searchParams.get('page'))
      if (fail) return route.fulfill({ status: 503, json: { error: '办理结果查询暂不可用' } })
      const second = url.searchParams.get('page') === '2'
      return route.fulfill({ json: { data: second ? [{ ...rows[0], request_id: id(200), recipient_name: undefined, recipient_code: undefined, recipient_label_status: 'not_found', expiry_mode: 'at_time', expires_at: '2026-10-31T08:00:00Z' }] : rows,
        total: 21, page: second ? 2 : 1, page_size: 20, total_pages: 2 } })
    }
    const row = rows.find(row => path === `/api/v1/catalog/entries/${entryID}/sharing_fulfillments/${row.request_id}`)
    if (row) { details.push(row.request_id); return route.fulfill({ json: row }) }
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_request' } })
  })
  await page.goto(`/catalog/entries/${entryID}?tab=sharing`)
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(catalog.getByTestId('catalog-entry-detail')).toHaveAttribute('data-load-state', 'loaded')
  const records = catalog.getByTestId('sharing-request-records')
  await expect(records).toBeVisible()
  return { catalog, records, details, requests, verify() { expect(writes).toEqual([]); expect(unexpected).toEqual([]); expect(errors).toEqual([]) } }
}

test('my handling records show recipients and each authoritative outcome without technical IDs or per-row browser reads', async ({ page }) => {
  const fixture = await openRecords(page), { records } = fixture
  await expect(records.getByRole('columnheader')).toHaveText(['接收方', '办理时间', '批准使用期限', '办理结果', ''])
  const tableRows = records.locator('.el-table__body tbody tr')
  await expect(tableRows).toHaveCount(6)
  const outcomes = ['已签发授权', '已受理，尚未签发', '结果待确认', '已有授权，本次未重复签发', '签发窗口已结束，未签发', '已关闭']
  for (let index = 0; index < outcomes.length; index++) {
    await expect(tableRows.nth(index).locator('td').nth(3)).toHaveText(outcomes[index])
    await expect(tableRows.nth(index)).not.toContainText(rows[index].request_id)
  }
  await expect(tableRows.nth(0)).toContainText('户外开发者 · outdoor-develop (当前账号)')
  await expect(tableRows.nth(3)).toContainText('户外分析项目组 · outdoor-analysis')
  await expect(tableRows.nth(3)).not.toContainText('当前账号')
  expect(fixture.details).toEqual([])
  await page.getByRole('button', { name: 'CN', exact: true }).click()
  await page.getByRole('menuitem', { name: 'English', exact: true }).click()
  await expect(tableRows.first().locator('td').nth(3)).toHaveText('Authorization issued')
  await expect(tableRows.nth(2).locator('td').nth(3)).toHaveText('Outcome awaiting confirmation')
  await expect(records.getByRole('columnheader').nth(2)).toHaveText('Approved use period')
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await page.getByRole('menuitem', { name: '中文', exact: true }).click()
  await records.locator('.el-pagination').getByRole('button', { name: '下一页', exact: true }).click()
  await expect(tableRows).toHaveCount(1)
  await expect(tableRows.first()).toContainText('接收方已不存在')
  await expect(tableRows.first()).not.toContainText('9007199254740993')
  expect(fixture.requests).toContain('2')
  fixture.verify()
})

test('handling detail keeps readable information first, copies exact IDs on demand and reloads using only GET', async ({ page, context }) => {
  // Release each iframe write only after the host has read the old native
  // clipboard. This deterministically exercises click-before-write completion,
  // without faking clipboard contents or depending on a machine-speed delay.
  await page.addInitScript(() => {
    const clipboard = navigator.clipboard
    if (!clipboard) return
    if (window === window.top) {
      const probe = window.__catalogClipboardTiming = { pending: [], started: [], completed: [], staleReads: [] }
      const readText = clipboard.readText.bind(clipboard)
      clipboard.readText = async () => {
        const previous = await readText()
        const waiting = probe.pending.splice(0)
        if (waiting.length) probe.staleReads.push(previous)
        waiting.forEach(release => release())
        return previous
      }
    } else {
      const writeText = clipboard.writeText.bind(clipboard)
      clipboard.writeText = async value => {
        const probe = window.top.__catalogClipboardTiming
        probe.started.push(value)
        await new Promise(resolve => probe.pending.push(resolve))
        await writeText(value)
        probe.completed.push(value)
      }
    }
  })
  const fixture = await openRecords(page), { catalog, records } = fixture
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.evaluate(() => navigator.clipboard.writeText('before-catalog-copy'))
  await records.getByTestId('sharing-open-request').first().click()
  const detail = catalog.getByTestId('sharing-request-detail')
  await expect(detail).toContainText('户外开发者 · outdoor-develop (当前账号)')
  await expect(detail).toContainText('批准使用期限')
  await expect(detail).toContainText('已签发授权')
  await expect(detail.getByTestId('sharing-copy-request-id')).toBeHidden()
  await detail.getByRole('button', { name: '技术信息', exact: true }).click()
  await detail.getByTestId('sharing-copy-request-id').click()
  await expect(catalog.locator('.el-message--success')).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(rows[0].request_id)
  await detail.getByTestId('sharing-copy-decision-id').click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(decisionID)
  expect(await page.evaluate(() => {
    const { started, completed, staleReads } = window.__catalogClipboardTiming
    return { started, completed, staleReads }
  })).toEqual({
    started: [rows[0].request_id, decisionID], completed: [rows[0].request_id, decisionID],
    staleReads: ['before-catalog-copy', rows[0].request_id]
  })
  await page.reload()
  await expect(detail).toContainText('户外开发者 · outdoor-develop (当前账号)')
  await expect(detail.getByTestId('sharing-copy-request-id')).toBeHidden()
  await catalog.getByTestId('sharing-back-records').click()
  await expect(records).toBeVisible()
  fixture.verify()
})

test('handling history dependency failure remains an error instead of empty or pending results', async ({ page }) => {
  const fixture = await openRecords(page, { fail: true })
  await expect(fixture.records).toContainText('办理结果查询暂不可用')
  await expect(fixture.records).not.toContainText('暂无本人的授权办理记录')
  await expect(fixture.records.locator('.el-table__body tbody tr')).toHaveCount(0)
  fixture.verify()
})
