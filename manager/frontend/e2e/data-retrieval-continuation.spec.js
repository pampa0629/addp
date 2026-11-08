import { expect, test } from '@playwright/test'

for (const terminalStatus of [409, 410]) {
  test(`retrieval continues empty batches, retries a lost batch and clears invalidated results (${terminalStatus})`, async ({ page }) => {
    const requests = []
    let temporaryFailure = true
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url())
      const path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'continuation-fixture', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '34', local_account: { username: 'retrieval' } })
      if (path.endsWith('/system/auth/context')) return json(route, { context: { type: 'tenant', tenant_id: '1' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['manager.search.execute', 'manager.data_item.read'] }] } })
      if (path.endsWith('/manager/engines')) return json(route, { data: [] })
      if (path.endsWith('/manager/search/history')) return json(route, { data: { items: [] } })
      if (path.endsWith('/manager/search')) {
        const cursor = url.searchParams.get('cursor')
        requests.push(Object.fromEntries(url.searchParams))
        if (!cursor) return json(route, batch([], 'empty'))
        if (cursor === 'empty') return json(route, batch([row('A')], 'partial'))
        if (cursor === 'partial' && temporaryFailure) {
          temporaryFailure = false
          return json(route, { error: '暂不可用', error_code: 'manager_search_failed' }, 503)
        }
        if (cursor === 'partial') return json(route, batch([row('B')], 'invalidated'))
        return json(route, { error: '检索依据已变化', error_code: terminalStatus === 409 ? 'search_context_changed' : 'search_continuation_expired' }, terminalStatus)
      }
      return json(route, {}, 404)
    })
    await page.goto('/data-retrieval')
    await page.locator('.search-input input').fill('roads')
    await page.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(page.getByRole('button', { name: '继续查找', exact: true })).toBeVisible()
    await expect(page.getByText('未找到相关结果', { exact: true })).toHaveCount(0)
    await page.locator('.search-input input').fill('edited but not submitted')
    await page.getByRole('button', { name: '继续查找', exact: true }).click()
    await expect(page.locator('.result-item')).toHaveCount(1)
    await page.getByRole('button', { name: '加载更多', exact: true }).click()
    await expect(page.getByRole('button', { name: '重试', exact: true })).toBeVisible()
    await expect(page.locator('.result-item')).toHaveCount(0)
    await page.getByRole('button', { name: '重试', exact: true }).click()
    await expect(page.locator('.result-item')).toHaveCount(2)
    await expect(page.locator('.result-item').nth(0)).toContainText('A')
    await expect(page.locator('.result-item').nth(1)).toContainText('B')
    await page.getByRole('button', { name: '加载更多', exact: true }).click()
    await expect(page.locator('.result-item')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '加载更多', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '重试', exact: true })).toHaveCount(0)
    expect(requests.map(request => request.cursor)).toEqual([undefined, 'empty', 'partial', 'partial', 'invalidated'])
    expect(requests.every(request => request.q === 'roads' && request.page_size === '10' && !('page' in request))).toBe(true)
  })
}
function row(id) { return { document_id: id, file_name: id, match_methods: ['keyword'] } }
function batch(results, next_cursor) { return { data: { results, next_cursor, exhausted: false, page_size: 10 } } }
function json(route, body, status = 200) { return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) }) }
