import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

test('keeps JSON record pagination clickable in a narrow preview panel', async ({ page }) => {
  const locator = 'addp://engine/2/path/gis-data/records.json?type=file&item_id=8'
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'records.json', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'JSON test engine', type: 'server', hasChildren: true, children: [item] }
  const requestedPages = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'pagination-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'pagination-e2e', local_account: { username: 'pagination-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      const currentPage = Number(url.searchParams.get('page') || 1)
      const pageSize = Number(url.searchParams.get('page_size') || 20)
      requestedPages.push(currentPage)
      return json(route, { preview_type: 'table', data: {
        mode: 'table', columns: ['id'], total: 73090, page: currentPage, page_size: pageSize,
        rows: Array.from({ length: pageSize }, (_, i) => ({ id: (currentPage - 1) * pageSize + i + 1 }))
      } })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  const next = page.getByRole('button', { name: '下一页', exact: true })
  await expect(next).toBeVisible()
  await expect(page.getByText('最多展示前 50 行数据', { exact: true })).toHaveCount(0)
  // Pointer input must reach the control without first scrolling its own toolbar.
  await expect.poll(() => next.evaluate(button => {
    const r = button.getBoundingClientRect()
    return button.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))
  })).toBe(true)
  await next.click()
  await expect(page.getByRole('cell', { name: '21', exact: true })).toBeVisible()
  expect(requestedPages).toContain(2)
  await page.getByRole('button', { name: '上一页', exact: true }).click()
  await expect(page.getByRole('cell', { name: '1', exact: true })).toBeVisible()
})

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
