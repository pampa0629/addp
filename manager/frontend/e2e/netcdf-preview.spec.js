import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

for (const lang of ['zh-cn', 'en']) test(`NetCDF slices preserve exact values, indices and source-change rejection (${lang})`, async ({ page }) => {
  const locator = 'addp://engine/14/path/grid.nc?type=file&item_id=30'
  const rootLocator = 'addp://engine/14/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'grid.nc', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'NetCDF fixture', type: 'server', hasChildren: true, children: [item] }
  const variable = { value_mode: 'raw_storage', name: '/temperature', type: 'Int64', dimensions: [{ name: '/time', size: '2' }, { name: '/y', size: '3' }, { name: '/x', size: '4' }], attributes: { units: { values: ['K'] }, calendar: { values: ['360_day'] } }, slice_supported: true }
  // Meta owns the canonical child name and removes duplicated protocol keys
  // from native. The Runtime's slice response still has a variable name.
  const { name: _name, ...nativeVariable } = variable
  const spatial = lang === 'en' ? { footprint: { type: 'Feature', properties: {}, geometry: { type: 'Polygon', coordinates: [[[117, 27], [117.001, 27], [117.001, 27.001], [117, 27.001], [117, 27]]] } } } : null
  const slices = []
  const parentReads = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), lang)
  const json = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
  if (lang !== 'en') await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/grid-file-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url()), path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'netcdf-e2e', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'test', local_account: { username: 'test' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 14, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/14') || path.endsWith('/meta/resource-tree/14/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      if (url.searchParams.has('grid_slice')) {
        expect([...url.searchParams.keys()].sort()).toEqual(['grid_slice', 'locator'])
        const selection = JSON.parse(url.searchParams.get('grid_slice')); slices.push(selection)
        if (slices.length === 3) return json(route, { error: lang === 'en' ? 'Source changed; refresh metadata' : '源文件已变化，请刷新元数据', error_code: 'preview_source_refresh_required' }, 409)
        const values = Array.from({ length: 3 }, (_, y) => Array.from({ length: 4 }, (_, x) => String(9007199254740993n + BigInt(selection.indices[0] * 12 + y * 4 + x))))
        const valid = [[true, true, true, true], [true, true, false, true], [true, true, true, true]]
        return json(route, { preview_type: 'object', data: { mode: 'object', object: { content: { kind: 'grid_slice', json: { selection, values, valid, variable, spatial, value_mode: 'raw_storage' } } } } })
      }
      parentReads.push(url.searchParams.get('child_name') || '')
      if (url.searchParams.has('child_name')) return json(route, { error: 'Array reads require an explicit grid_slice', error_code: 'grid_slice_selection_invalid' }, 400)
      return json(route, { preview_type: 'object', data: { mode: 'object', object: { attributes: { item: { format: 'netcdf', data_type: 'container', layout: 'single' }, type_info: { container: { child_count: 1, children: [{ name: '/temperature', child_kind: 'array', data_type: 'media', native: nativeVariable }] } } }, content: { kind: 'container', json: { format: 'netcdf', default_child: lang === 'en' ? '/temperature' : '', children: [{ name: '/temperature', key: '/temperature', child_kind: 'array', data_type: 'media', native: nativeVariable }] } } } } })
    }
    if (path.endsWith('/quick-view')) return json(route, { is_scanned: true, data_type: 'container', format: 'netcdf', actions: [] })
    return json(route, {})
  })
  await page.goto('/data-explorer?locator=' + encodeURIComponent(locator))
  await expect(page.getByTestId('grid-variable-details')).toContainText('360_day')
  await page.getByTestId('read-grid-slice').click()
  await expect(page.locator('td[data-raw="9007199254740993"]')).toHaveText('9007199254740993')
  await expect(page.locator('td[data-raw="9007199254740999"]')).toHaveAttribute('data-valid', 'false')
  if (spatial) await expect(page.locator('.geojson-preview')).toBeVisible()
  else await expect(page.getByTestId('grid-no-location')).toBeVisible()
  const grid = page.getByTestId('grid-slice-result')
  expect(await grid.evaluate(element => element.clientHeight >= element.querySelector('table').getBoundingClientRect().height)).toBe(true)
  await grid.scrollIntoViewIfNeeded()
  await expect(grid.locator('td').last()).toBeInViewport()
  const indexInput = page.locator('.grid-controls .el-input-number').first().locator('input')
  await indexInput.fill('1'); await indexInput.press('Tab')
  await expect(page.getByTestId('grid-slice-result')).toHaveCount(0)
  await page.getByTestId('read-grid-slice').click()
  await expect(page.locator('td[data-raw="9007199254741005"]')).toHaveText('9007199254741005')
  expect(slices[1].indices).toEqual([1])
  await page.getByTestId('read-grid-slice').click()
  await expect(page.getByTestId('grid-slice-result')).toHaveCount(0)
  await expect(page.locator('.grid-controls').locator('..')).toContainText(lang === 'en' ? 'Source changed' : '源文件已变化')
  expect(parentReads).toEqual([''])
  expect(errors).toEqual([])
})
