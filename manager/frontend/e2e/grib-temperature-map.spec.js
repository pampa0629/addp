import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

test('GRIB temperature map keeps generation and ready COG bound to the selected message', async ({ page }) => {
  const locator = 'addp://engine/14/path/forecast.grib2?type=file&item_id=30'
  const rootLocator = 'addp://engine/14/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'forecast.grib2', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'GRIB fixture', type: 'server', hasChildren: true, children: [item] }
  const version = 'sha256:' + 'a'.repeat(64)
  const profile = 'ecmwf_ifs_01_temperature/v1'
  const children = [1, 2].map((number) => ({ name: `message-${number}`, format: 'grib', child_kind: 'array', data_type: 'media', native: {
    map_profile: profile, value_mode: 'decoded_native_units', type: 'Float64', slice_supported: true,
    dimensions: [{ name: 'y', size: '1801' }, { name: 'x', size: '3600' }],
    message: { time: { end_utc: `2026-11-17T0${(number-1)*6}:00:00+00:00` } }
  } }))
  const generated = []
  const requests = []
  const ready = new Set()
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'en'))
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: ['/plugins/grid-file-preview.js'] } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const json = value => route.fulfill({ json: value })
    if (path.endsWith('/system/refresh')) return json({ access_token: 'map-e2e', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json({ id: '1', display_name: 'test', local_account: { username: 'test' } })
    if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
    if (path.endsWith('/manager/engines')) return json({ data: [{ id: 14, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/14') || path.endsWith('/meta/resource-tree/14/node')) return json(root)
    if (path.endsWith('/manager/preview')) return json({ preview_type: 'object', data: { mode: 'object', object: {
      attributes: { item: { format: 'grib', data_type: 'container', layout: 'single' }, format_info: { grib: { source_version: version } }, type_info: { container: { child_count: 2, children } } },
      content: { kind: 'container', json: { format: 'grib', children } }
    } } })
    if (path.endsWith('/manager/quick-view/actions')) {
      const body = request.postDataJSON()
      generated.push(body.raster_message)
      ready.add(body.raster_message)
      return json({ execution_id: body.raster_message })
    }
    if (path.includes('/manager/executions/')) return json({ status: 'success' })
    if (path.endsWith('/manager/quick-view/capability')) {
      const message = url.searchParams.get('raster_message')
      if (!message) return json({})
      const cached = ready.has(message)
      return json({ can_use_quick_view: cached, render_source: cached ? 'client_cog_render' : '', available_actions: ['generate_raster_cog'],
        quick_view: cached ? { preview_url: `/api/v1/manager/raster_cog/${message}/content?source_version=${version}`, extent: [-180, -90, 180, 90],
          map_profile: profile, raster_message: message, raster_source_version: version } : {} })
    }
    if (path.includes('/manager/raster_cog/')) {
      requests.push(path.split('/').at(-2))
      // This case asserts selecting the correct protected URL. Rendering the
      // temperature COG pixels is covered by the native and deployment cases.
      return route.fulfill({ status: 404, json: {} })
    }
    return json({})
  })
  await page.goto('/data-explorer?locator=' + encodeURIComponent(locator))
  const map = page.getByTestId('grib-temperature-map')
  const generate = page.getByTestId('generate-grib-map')
  await expect(map).toHaveAttribute('data-message', 'message-1')
  await generate.click()
  await expect.poll(() => requests).toContain('message-1')

  async function select(message) {
    await page.locator('.grid-container-preview .child-select').click()
    await page.getByRole('option', { name: message, exact: true }).click()
    await expect(map).toHaveAttribute('data-message', message)
  }
  await select('message-2')
  await expect(map.locator('.raster-tiff-quick-view')).toHaveCount(0)
  await generate.click()
  await expect.poll(() => requests).toContain('message-2')
  const count = requests.length
  await select('message-1')
  await expect.poll(() => requests.slice(count)).toContain('message-1')
  expect(generated).toEqual(['message-1', 'message-2'])
})
