import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

test('switches workbook schemas, resets pagination and clears rows for an empty sheet', async ({ page }) => {
  const locator = 'addp://engine/2/path/doc/sheets.xlsx?type=file&item_id=8'
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'sheets.xlsx', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'Workbook test engine', type: 'server', hasChildren: true, children: [item] }
  const requests = []
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))
  page.on('console', message => {
    if (message.type() === 'error') browserErrors.push(message.text())
  })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/container-preview.js', '/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'sheet-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'sheet-e2e', local_account: { username: 'sheet-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      const child = url.searchParams.get('child_name') || ''
      const currentPage = Number(url.searchParams.get('page') || 1)
      const pageSize = Number(url.searchParams.get('page_size') || 20)
      requests.push({ child, page: currentPage })
      if (!child) return json(route, { preview_type: 'object', data: { object: { content: { kind: 'container', json: {
        format: 'excel', default_child: 'Cities', active_child: 'Cities',
        children: ['Cities', 'Readings', 'Empty'].map(name => ({ name, key: name, child_kind: 'sheet', data_type: 'table' })),
        summary: { child_count: 3, sampled_children: 3 }
      } } } } })
      const data = child === 'Cities'
        ? { columns: ['city_id', 'city_name'], total: 25, rows: Array.from({ length: Math.min(pageSize, 25 - (currentPage - 1) * pageSize) }, (_, i) => ({ city_id: (currentPage - 1) * pageSize + i + 1, city_name: `City-${(currentPage - 1) * pageSize + i + 1}` })) }
        : child === 'Readings'
          ? { columns: ['sensor', 'temperature'], total: 1, rows: [{ sensor: 'Sensor-A', temperature: 18.5 }] }
          : { columns: [], total: 0, rows: [] }
      return json(route, { preview_type: 'table', data: { mode: 'table', page: currentPage, page_size: pageSize, ...data } })
    }
    return json(route, {})
  })

  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  const container = page.locator('.container-preview')
  await expect(container.getByRole('cell', { name: 'City-1', exact: true })).toBeVisible()
  await container.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(container.getByRole('cell', { name: 'City-21', exact: true })).toBeVisible()

  await container.locator('.child-select').click()
  await page.getByRole('option', { name: 'Readings', exact: true }).click()
  await expect(container.getByRole('cell', { name: 'Sensor-A', exact: true })).toBeVisible()
  await expect(container.getByRole('columnheader', { name: 'temperature', exact: true })).toBeVisible()
  await expect(container.getByRole('columnheader', { name: 'city_name', exact: true })).toHaveCount(0)
  expect(requests.at(-1)).toEqual({ child: 'Readings', page: 1 })

  await container.locator('.child-select').click()
  await page.getByRole('option', { name: 'Empty', exact: true }).click()
  await expect(container.locator('.el-table__empty-block')).toBeVisible()
  await expect(container.getByRole('cell', { name: 'Sensor-A', exact: true })).toHaveCount(0)
  await expect(container.getByRole('columnheader', { name: 'temperature', exact: true })).toHaveCount(0)
  await expect(container.getByRole('button', { name: '下一页', exact: true })).toHaveCount(0)
  expect(requests.at(-1)).toEqual({ child: 'Empty', page: 1 })

  await container.locator('.child-select').click()
  await page.getByRole('option', { name: /^Cities/ }).click()
  await expect(container.getByRole('cell', { name: 'City-1', exact: true })).toBeVisible()
  expect(requests.at(-1)).toEqual({ child: 'Cities', page: 1 })
  expect(browserErrors).toEqual([])
})

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
