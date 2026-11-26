import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import { gribScreenshotColors } from './grib-pixels.js'
import { managerAuthContext } from './managerAuthContext.js'

for (const kind of ['temperature', 'precipitation', 'noaa-precipitation']) {
  test(`GRIB ${kind} map stays bound to the selected message`, async ({ page }) => {
    await verifyMap(page, kind)
  })
}

async function verifyMap(page, kind) {
  const noaa = kind === 'noaa-precipitation'
  const precipitation = kind !== 'temperature'
  const fixture = precipitation ? readFileSync(new URL('./fixtures/grib-precipitation-mask.tif', import.meta.url)) : null
  const locator = 'addp://engine/14/path/forecast.grib2?type=file&item_id=30'
  const rootLocator = 'addp://engine/14/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'forecast.grib2', type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'GRIB fixture', type: 'server', hasChildren: true, children: [item] }
  const version = 'sha256:' + 'a'.repeat(64)
  const profile = noaa ? 'noaa_gfs_025_precipitation/v1' : `ecmwf_ifs_01_${kind}/v1`
  const children = [1, 2].map((number) => ({ name: `message-${number}`, format: 'grib', child_kind: 'array', data_type: 'media', native: {
    map_profile: profile, value_mode: 'decoded_native_units', type: 'Float64', slice_supported: true,
    dimensions: [{ name: 'y', size: '1801' }, { name: 'x', size: '3600' }],
    message: { time: noaa
      ? { start_utc: `2026-11-17T0${(number - 1) * 6}:00:00+00:00`, interval_length: 15 - number * 6, end_utc: '2026-11-17T09:00:00+00:00' }
      : { start_utc: '2026-11-17T00:00:00+00:00', interval_length: (number - 1) * 6, end_utc: `2026-11-17T0${(number-1)*6}:00:00+00:00` } }
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
      if (!fixture) return route.fulfill({ status: 404, json: {} })
      const range = request.headers().range?.match(/^bytes=(\d+)-(\d+)$/)
      if (!range) return route.fulfill({ status: 200, contentType: 'image/tiff', body: fixture })
      const start = Number(range[1])
      const end = Math.min(Number(range[2]), fixture.length - 1)
      if (start > end) return route.fulfill({ status: 416 })
      return route.fulfill({ status: 206, contentType: 'image/tiff',
        headers: { 'Content-Range': `bytes ${start}-${end}/${fixture.length}` }, body: fixture.subarray(start, end + 1) })
    }
    return json({})
  })
  await page.goto('/data-explorer?locator=' + encodeURIComponent(locator))
  const map = page.getByTestId('grib-field-map')
  const generate = page.getByTestId('generate-grib-map')
  await expect(map).toHaveAttribute('data-message', 'message-1')
  if (precipitation) await expect(map.locator('.grib-map-toolbar')).toContainText(noaa
    ? 'Accumulation 2026-11-17T00:00:00+00:00 to 2026-11-17T09:00:00+00:00 · 9 h'
    : 'Forecast baseline · 2026-11-17T00:00:00+00:00 · 0 mm')
  await generate.click()
  await expect.poll(() => requests).toContain('message-1')
  if (precipitation) {
    const legend = map.getByTestId('grib-precipitation-legend')
    await expect(legend).toContainText('No precipitation · 0 mm')
    await expect(legend).toContainText('No data')
    await expect(legend).toContainText('>5–10 mm')
    await map.getByRole('slider').press('End')
    const preview = map.locator('.raster-tiff-quick-view')
    await expect.poll(async () => {
      const canvas = preview.locator('.raster-map canvas').first()
      if (!(await canvas.isVisible())) return false
      const { counts } = await gribScreenshotColors(page, canvas, legend.locator('.grib-swatch:not(.grib-missing)'))
      return counts[0] > 100 && counts[noaa ? 1 : 3] > 100
    }).toBe(true)
    await expect.poll(async () => {
      const pixels = await preview.evaluate(element => {
        const map = element.__vueParentComponent.setupState.map
        const layer = map?.getLayers().getArray().at(-1)
        if (layer?.getSource()?.getState() !== 'ready') return null
        return new Promise(resolve => {
          layer.once('postrender', event => {
            const gl = event.context
            const [width, height] = map.getSize()
            resolve([-90, 0, 90].map(longitude => {
              const [x, y] = map.getPixelFromCoordinate([longitude, 0])
              const pixel = new Uint8Array(4)
              gl.readPixels(Math.floor(x * gl.drawingBufferWidth / width),
                gl.drawingBufferHeight - 1 - Math.floor(y * gl.drawingBufferHeight / height),
                1, 1, gl.RGBA, gl.UNSIGNED_BYTE, pixel)
              return [...pixel]
            }))
          })
          map.renderSync()
        })
      })
      if (!pixels) return false
      const [dry, missing, wet] = pixels
      return dry[3] > 200 && missing[3] === 0 && wet[3] > 200 && dry.join() !== wet.join()
    }).toBe(true)
  }

  async function select(message) {
    await page.locator('.grid-container-preview .child-select').click()
    await page.getByRole('option', { name: message, exact: true }).click()
    await expect(map).toHaveAttribute('data-message', message)
  }
  await select('message-2')
  if (precipitation) await expect(map.locator('.grib-map-toolbar')).toContainText(noaa
    ? 'Accumulation 2026-11-17T06:00:00+00:00 to 2026-11-17T09:00:00+00:00 · 3 h'
    : 'Accumulation 2026-11-17T00:00:00+00:00 to 2026-11-17T06:00:00+00:00 · 6 h')
  await expect(map.locator('.raster-tiff-quick-view')).toHaveCount(0)
  await generate.click()
  await expect.poll(() => requests).toContain('message-2')
  const count = requests.length
  await select('message-1')
  await expect.poll(() => requests.slice(count)).toContain('message-1')
  expect(generated).toEqual(['message-1', 'message-2'])
}
