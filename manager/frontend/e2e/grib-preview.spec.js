import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

for (const lang of ['zh-cn', 'en']) test(`GRIB fields preserve decoded units and independent grids (${lang})`, async ({ page }) => {
  const locator = 'addp://engine/14/path/weather.grb2?type=file&item_id=31'
  const rootLocator = 'addp://engine/14/path/?type=root&node_id=1'
  const item = { id: locator, locator, label: 'weather.grb2', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'GRIB source', type: 'root', hasChildren: true, children: [item] }
  const variables = [4, 3].map((width, index) => ({ name: `message-${index + 1}`, child_kind: 'array', data_type: 'media', format: 'grib', native: {
    type: 'Float64', dimensions: [{ name: 'y', size: '2' }, { name: 'x', size: String(width) }], value_mode: 'decoded_native_units', slice_supported: true,
    attributes: { GRIB_UNIT: { values: ['[K]'] }, GRIB_ELEMENT: { values: ['TMP'] }, GRIB_FORECAST_SECONDS: { values: [String(index * 10800)] } }
  } }))
  const slices = []
  let changed = false
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), lang)
  await page.route('**/api/v1/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    const json = (value, status = 200) => route.fulfill({ json: value, status })
    if (path.endsWith('/system/refresh')) return json({ access_token: 'grib-fixture', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json({ id: 1, display_name: 'GRIB reader', local_account: { username: 'grib-reader' } })
    if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
    if (path.endsWith('/manager/engines')) return json({ data: [{ id: 14, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [root, item] })
    if (path.endsWith('/meta/resource-tree/14') || path.endsWith('/node')) return json(root)
    if (path.endsWith('/manager/preview')) {
      if (url.searchParams.has('grid_slice')) {
        const selection = JSON.parse(url.searchParams.get('grid_slice')); slices.push(selection)
        if (changed) return json({ error: lang === 'en' ? 'Source changed; refresh metadata' : '源文件已变化，请刷新元数据', error_code: 'preview_source_refresh_required' }, 409)
        const width = selection.variable === 'message-1' ? 4 : 3
        expect(selection).toEqual({ variable: selection.variable, indices: [], x: 0, y: 0, width, height: 2 })
        const values = [Array.from({ length: width }, (_, x) => x === 0 ? '9999.0' : '280.25'), Array.from({ length: width }, (_, x) => x === 1 ? 'NaN' : '281.25')]
        const valid = [Array(width).fill(true), Array.from({ length: width }, (_, x) => x !== 1)]
        return json({ preview_type: 'object', data: { mode: 'object', object: { content: { kind: 'grid_slice', json: { value_mode: 'decoded_native_units', selection, values, valid, spatial: null } } } } })
      }
      expect(url.searchParams.has('child_name')).toBe(false)
      return json({ preview_type: 'object', data: { mode: 'object', object: { attributes: { item: { format: 'grib', data_type: 'container', layout: 'single' }, type_info: { container: { child_count: 2, children: variables } } }, content: { kind: 'container', json: { format: 'grib', children: variables.map(v => ({ ...v, key: v.name })) } } } } })
    }
    return json({})
  })
  await page.goto('/data-explorer?locator=' + encodeURIComponent(locator))
  await expect(page.getByTestId('grid-variable-details')).toContainText('[K]')
  await expect(page.locator('.grid-container-preview')).toContainText(lang === 'en' ? 'Decoded values in the file' : '按文件声明单位解码')
  await page.getByTestId('read-grid-slice').click()
  await expect(page.locator('td[data-raw="9999.0"]').first()).toHaveAttribute('data-valid', 'true')
  await expect(page.locator('td[data-raw="NaN"]')).toHaveAttribute('data-valid', 'false')
  await expect(page.locator('.geojson-preview')).toHaveCount(0)
  await page.locator('.grid-container-preview .el-select').click()
  await page.getByRole('option', { name: 'message-2', exact: true }).click()
  await expect(page.getByTestId('grid-slice-result')).toHaveCount(0)
  await page.getByTestId('read-grid-slice').click()
  await expect(page.getByTestId('grid-slice-result').locator('tbody tr').first().locator('td')).toHaveCount(3)
  changed = true
  await page.getByTestId('read-grid-slice').click()
  await expect(page.getByTestId('grid-slice-result')).toHaveCount(0)
  await expect(page.locator('.grid-container-preview')).toContainText(lang === 'en' ? 'Source changed' : '源文件已变化')
  expect(slices.map(s => s.variable)).toEqual(['message-1', 'message-2', 'message-2'])
})
