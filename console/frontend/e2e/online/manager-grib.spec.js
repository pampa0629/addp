import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { gribScreenshotColors } from '../../../../manager/frontend/e2e/grib-pixels.js'
import { modelPixels } from '../../../../manager/frontend/e2e/model-preview-support.js'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

test('ordinary user reads independent GRIB messages with native units and downloads originals', async ({ page }) => {
  test.setTimeout(480000)
  const env = process.env, context = JSON.parse(env.ADDP_ONLINE_MANAGER_GRIB_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases).toHaveLength(22)
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id, permissions: new Set(context.identity.permissions_verified) }
  expect([...identity.permissions].sort()).toEqual(['manager.content.read', 'manager.data_item.read', 'manager.derived_artifact.create', 'manager.derived_artifact.delete', 'manager.derived_artifact.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read', 'monitor.execution.read'])
  const outcomes = [], browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of context.cases) {
      const observe = page.waitForResponse(response => {
        const url = new URL(response.url())
        return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === source.locator && !url.searchParams.has('grid_slice')
      }); observe.catch(() => {})
      await page.goto('/manager/data-explorer?locator=' + encodeURIComponent(source.locator))
      const response = await observe; expect(response.status()).toBe(200)
      const api = await browserAPIFromResponse(response, env.GATEWAY_URL, identity)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        const sameEnd = source.spec.case_id === 'noaa-gfs-intervals'
        const highResolution = source.spec.case_id === 'ecmwf-precipitation-0p1'
        const utc = hour => new Date(((source.spec.reference_time ?? 1767322800) + hour * 3600) * 1000).toISOString().replace('.000Z', '+00:00')
        for (const field of source.spec.fields) {
          const { selection, values, valid } = source.expected_windows[field.number - 1]
          const screenshot = `grib-${source.source_case}-${source.spec.case_id}-message${field.number}-slice`
          if (field.number > 1) {
            await frame.locator('.child-select .el-select__wrapper').click()
            await frame.getByRole('option', { name: `message-${field.number}`, exact: true }).click()
            await expect(frame.getByTestId('grid-slice-result')).toHaveCount(0)
          }
          const details = frame.getByTestId('grid-variable-details')
          await expect(details).toContainText('Float64')
          await expect(details).toContainText(field.unit ?? '[K]')
          if (source.spec.edition === 1) {
            await expect(details).toContainText('GRIB1_TIME')
            await expect(details).toContainText('GRIB1_LEVEL')
            await expect(details).toContainText(field.level === 101 ? 'kPa' : field.level === 100 ? 'hPa' : 'ground')
          } else await expect(details).toContainText(field.short_name ?? '2-HTGL')
          await expect(details).toContainText(String((field.start_hour ?? field.forecast) * 3600))
          if (source.spec.packing === 'ccsds') {
            await expect(details).toContainText('GRIB2_LOCAL')
            await expect(details).toContainText('mars_stream')
            const initialZero = field.product_template === 8 && field.forecast === 0
            await expect(details).toContainText(initialZero ? 'exact_zero_float64_output' : 'float32_scaling_float64_output')
            if (field.product_template === 8) {
              await expect(details).toContainText('Total precipitation [m]')
              const timeCell = details.getByText('GRIB2_TIME', { exact: true }).locator('xpath=following-sibling::td[1]')
              await expect.poll(async () => JSON.parse(await timeCell.textContent())).toMatchObject({
                operation: 'accumulation', reference_utc: utc(0), start_utc: utc(0), end_utc: utc(field.forecast),
                interval_length: field.forecast, increment_unit: 13, increment_value: field.increment_seconds ?? 450
              })
              if (highResolution) {
                const packingCell = details.getByText('GRIB2_PACKING', { exact: true }).locator('xpath=following-sibling::td[1]')
                await expect.poll(async () => JSON.parse(await packingCell.textContent())).toMatchObject({
                  data_template: 42, bits_per_value: initialZero ? 0 : 12
                })
                await timeCell.scrollIntoViewIfNeeded()
                await expect(timeCell).toBeInViewport({ ratio: 1 })
                await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${screenshot}-time.png`) })
              }
            }
          }
          if (source.spec.packing === 'complex-spatial') {
            await expect(details).toContainText('GRIB2_DECODE_PRECISION')
            await expect(details).toContainText('float32_scaling_float64_output')
            await expect(details).toContainText('GRIB2_PACKING')
            await expect(details).toContainText('GRIB2_TIME')
            if (field.product_template === 8) {
              const timeCell = details.getByText('GRIB2_TIME', { exact: true }).locator('xpath=following-sibling::td[1]')
              await expect.poll(async () => JSON.parse(await timeCell.textContent())).toMatchObject({
                operation: 'accumulation', reference_utc: utc(0), start_utc: utc(field.start_hour),
                end_utc: utc(field.forecast), interval_length: field.forecast - field.start_hour
              })
              if (sameEnd) {
                await timeCell.scrollIntoViewIfNeeded()
                await expect(timeCell).toBeInViewport({ ratio: 1 })
                await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${screenshot}-time.png`) })
              }
            }
          }
          const spacing = field.spacing ?? 1
          await expect(frame.getByTestId('grid-native-transform')).toContainText(String(field.west - spacing / 2))
          await expect(frame.locator('.grid-container-preview')).toContainText(/不自动转换单位|no automatic unit conversion/)
          if (field.sample) {
            for (const [key, name] of Object.entries({ x: /^(Start column|列起点)$/, y: /^(Start row|行起点)$/, width: /^(Columns|列数)$/, height: /^(Rows|行数)$/ })) {
              const input = frame.getByRole('spinbutton', { name })
              await input.fill(String(selection[key]))
              await input.press('Tab')
            }
          }
          const observed = page.waitForResponse(reply => {
            const url = new URL(reply.url())
            return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === source.locator && url.searchParams.has('grid_slice')
          }); observed.catch(() => {})
          await frame.getByTestId('read-grid-slice').click()
          const result = (await json(await observed, 'GRIB actual slice button')).data.object.content.json
          expect(selection.variable).toBe(`message-${field.number}`)
          expect(result.selection).toEqual(selection)
          expect(result.values).toEqual(values); expect(result.valid).toEqual(valid)
          expect(result.value_mode).toBe('decoded_native_units'); expect(result.spatial).toBeNull()
          const transform = [field.west - spacing / 2, spacing, 0, field.north + spacing / 2, 0, -spacing]
          if (highResolution) {
            expect(Math.abs(result.variable.grid.transform[1] - spacing)).toBeLessThanOrEqual(Number.EPSILON * spacing)
            transform[1] = result.variable.grid.transform[1]
          }
          expect(result.variable.grid.transform).toEqual(transform)
          await expect(frame.locator('.grid-window tbody tr')).toHaveCount(selection.height)
          for (let y = 0; y < selection.height; y++) for (let x = 0; x < selection.width; x++) {
            const cell = frame.locator('.grid-window tbody tr').nth(y).locator('td').nth(x)
            await expect(cell).toHaveAttribute('data-raw', values[y][x])
            await expect(cell).toHaveAttribute('data-valid', String(valid[y][x]))
            if (valid[y][x]) await expect(cell).toHaveText(values[y][x])
          }
          const grid = frame.getByTestId('grid-slice-result')
          await grid.scrollIntoViewIfNeeded()
          const first = grid.locator('td').first(), last = grid.locator('td').last()
          const precipitation = source.spec.packing === 'ccsds' && field.product_template === 8
          if (precipitation) await first.scrollIntoViewIfNeeded()
          await expect(first).toBeInViewport({ ratio: 1 })
          if (precipitation) {
            await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${screenshot}-start.png`) })
            await last.scrollIntoViewIfNeeded()
          }
          await expect(last).toBeInViewport({ ratio: 1 })
          await expect(last).toHaveText(values.at(-1).at(-1))
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `${screenshot}.png`) })
          await expect(frame.locator('.geojson-preview')).toHaveCount(0)
          await expect(frame.getByTestId('grid-no-location')).toContainText(/暂不提供地图预览|map preview is not available/)
        }
        const intervalProof = sameEnd ? { distinct_accumulation_windows_verified: true } : {}
        if (highResolution) intervalProof.high_resolution_accumulation_verified = true
        if (source.source_case === 'nfs') {
          const actions = await json(await api.get('/api/v1/manager/resource-actions', { params: { locator: source.locator } }), 'GRIB current download capability')
          expect(actions.actions.download.supported).toBe(false)
          await expect(frame.getByRole('button', { name: /^(下载|Download)$/ })).toHaveCount(0)
          const denied = await api.get('/api/v1/manager/downloads/file?locator=' + encodeURIComponent(source.locator))
          expect(denied.status()).toBe(403)
          expect(denied.headers()['content-type']).toContain('application/json')
          expect(denied.headers()['content-disposition']).toBeUndefined()
          const error = await denied.json()
          expect(Object.keys(error)).toEqual(['error'])
          expect(typeof error.error).toBe('string'); expect(error.error.length).toBeGreaterThan(0)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `grib-${source.source_case}-${source.spec.case_id}-page.png`) })
          outcomes.push({ ...intervalProof, locator: source.locator, values_verified: true, mask_verified: true, message_switch_verified: true, grid_verified: true, native_units_verified: true, viewport_grid_verified: true, visible_message_count: source.spec.fields.length, original_download_disabled: true, original_download_denied_verified: true })
          continue
        }
        const ready = page.waitForEvent('download')
        await frame.getByRole('button', { name: /^(下载|Download)$/ }).click()
        const download = await ready
        try {
          expect(download.suggestedFilename()).toBe(source.spec.name)
          const stream = await download.createReadStream(), chunks = []
          for await (const chunk of stream) chunks.push(chunk)
          const raw = Buffer.concat(chunks), digest = createHash('sha256').update(raw).digest('hex')
          expect(digest).toBe(source.spec.sha256); expect(raw.length).toBe(source.spec.size_bytes)
          const original = await api.get('/api/v1/manager/downloads/file?locator=' + encodeURIComponent(source.locator))
          expect(original.status()).toBe(200); expect(await original.body()).toEqual(raw)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `grib-${source.source_case}-${source.spec.case_id}-page.png`) })
          outcomes.push({ ...intervalProof, locator: source.locator, values_verified: true, mask_verified: true, message_switch_verified: true, grid_verified: true, native_units_verified: true, viewport_grid_verified: true, visible_message_count: source.spec.fields.length, download_sha256: digest, download_size: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    expect(browserErrors).toEqual([]); passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-grib-browser.json'), JSON.stringify({ schema_version: 'addp.manager-grib-browser/v2', run_id: context.run_id, result: passed ? 'passed' : 'failed', cases: outcomes, browser_errors: browserErrors }, null, 2) + '\n')
  }
})

// In this global, unrotated geographic view the painted pole-to-pole
// height is also the horizontal distance from Greenwich to the date line.
async function displayedWorld(page, viewport) {
  const png = await viewport.screenshot()
  return page.evaluate(async encoded => {
    const image = await createImageBitmap(await (await fetch('data:image/png;base64,' + encoded)).blob())
    const canvas = document.createElement('canvas')
    canvas.width = image.width
    canvas.height = image.height
    const ctx = canvas.getContext('2d')
    ctx.drawImage(image, 0, 0)
    image.close()
    const { width, height } = canvas
    const pixels = ctx.getImageData(0, 0, width, height).data
    const colored = (x, y) => {
      const i = (y * width + x) * 4
      const channels = [pixels[i], pixels[i + 1], pixels[i + 2]]
      return Math.max(...channels) - Math.min(...channels) > 30
    }
    const rows = []
    for (let y = 0; y < height; y++) {
      let samples = 0
      for (let n = 1; n <= 17; n++) samples += Number(colored(Math.floor(width * n / 18), y))
      if (samples >= 13) rows.push(y)
    }
    const north = rows[0], south = rows.at(-1)
    const center = Math.floor(width / 2), equator = Math.floor((north + south) / 2)
    const seamColors = [-5, -1, 0, 1, 5].every(dx => colored(center + dx, equator))
    return { north, south, width, height, seamColors }
  }, png.toString('base64'))
}

async function verifyGRIBMaps(page, storage, kind) {
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_GRIB_JSON)
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')

  const caseName = `${storage}-${kind}`
  const source = context.maps[caseName]
  const path = resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-grib-${caseName}-map-browser.json`)
  const proof = { run_id: context.run_id, case: caseName, locator: source.locator, source_version: source.version,
    generations: [], visits: [], result: 'incomplete' }
  const save = () => writeFileSync(path, JSON.stringify(proof, null, 2) + '\n')
  const actions = '/api/v1/manager/quick-view/actions'
  const posts = []
  page.on('request', request => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === actions) posts.push(request.postDataJSON())
  })
  const ranges = []
  page.on('response', response => {
    const url = new URL(response.url())
    if (/^\/api\/v1\/manager\/raster_cog\/\d+\/content$/.test(url.pathname) && response.status() === 206) {
      ranges.push({ path: url.pathname, version: url.searchParams.get('source_version'),
        range: response.headers()['content-range'], requested: response.request().headers().range })
    }
  })
  const byMessage = new Map()
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const map = frame.getByTestId('grib-field-map')
  const canvas = map.locator('.raster-map canvas').first()
  try {
    for (const [step, number] of [1, 2, 1, 2].entries()) {
      const message = 'message-' + number
      const rangeStart = ranges.length
      const capability = page.waitForResponse(async response => {
        const url = new URL(response.url())
        if (url.pathname !== '/api/v1/manager/quick-view/capability' ||
            url.searchParams.get('locator') !== source.locator || url.searchParams.get('raster_message') !== message ||
            response.status() !== 200) return false
        return (await response.json()).can_use_quick_view === true
      }, { timeout: 180000 })
      capability.catch(() => {})
      if (step === 0) await page.goto('/manager/data-explorer?locator=' + encodeURIComponent(source.locator))
      else {
        await frame.locator('.child-select .el-select__wrapper').click()
        await frame.getByRole('option', { name: message, exact: true }).click()
      }
      await expect(map).toHaveAttribute('data-message', message)
      await expect(map).toHaveAttribute('data-source-version', source.version)
      await expect(map).toContainText('2026-11-17T' + (number === 1 ? '00' : '06') + ':00:00+00:00')
      if (kind === 'precipitation') {
        const toolbar = map.locator('.grib-map-toolbar')
        if (number === 1) {
          await expect(toolbar).toContainText(/起报基线|Forecast baseline/)
          await expect(toolbar).toContainText('0 mm')
        } else {
          await expect(toolbar).toContainText('2026-11-17T00:00:00+00:00')
          await expect(toolbar).toContainText(/6 小时|6 h/)
        }
      }
      if (step < 2) {
        const action = page.waitForResponse(response => new URL(response.url()).pathname === actions &&
          response.request().method() === 'POST')
        action.catch(() => {})
        await map.getByTestId('generate-grib-map').click()
        const response = await action
        expect(response.status()).toBe(202)
        const execution = await response.json()
        proof.generations.push({ message, task_id: execution.task_id, execution_id: execution.execution_id })
        save()
        expect(response.request().postDataJSON()).toMatchObject({ locator: source.locator, action: 'generate_raster_cog', raster_message: message })
        expect(execution.task_type).toBe('raster_cog_generation')
      }
      const info = (await (await capability).json()).quick_view
      expect(info).toMatchObject({ raster_message: message, raster_source_version: source.version,
        map_profile: source.profile, extent: [-180, -90, 180, 90] })
      const url = new URL(info.preview_url, page.url())
      expect(url.pathname).toMatch(/^\/api\/v1\/manager\/raster_cog\/[1-9]\d*\/content$/)
      expect(url.searchParams.get('source_version')).toBe(source.version)
      if (step < 2) byMessage.set(message, info.preview_url)
      else expect(info.preview_url).toBe(byMessage.get(message))
      await expect.poll(() => ranges.slice(rangeStart).some(r => r.path === url.pathname &&
        r.version === source.version && /^bytes \d+-\d+\/\d+$/.test(r.range) && /^bytes=/.test(r.requested))).toBe(true)
      await expect(canvas).toBeVisible({ timeout: 30000 })
      await canvas.scrollIntoViewIfNeeded({ timeout: 10000 })
      let precipitationColors
      if (kind === 'precipitation') {
        const legend = map.getByTestId('grib-precipitation-legend')
        await expect(legend).toContainText(/缺测|No data/)
        await expect(legend).toContainText('0 mm')
        await expect(legend).toContainText('>50 mm')
        await map.getByRole('slider').press('End')
        const swatches = legend.locator('.grib-swatch:not(.grib-missing)')
        await expect.poll(async () => {
          precipitationColors = await gribScreenshotColors(page, canvas, swatches)
          const { counts, total } = precipitationColors
          const rain = counts.slice(1).reduce((sum, count) => sum + count, 0) / total
          return counts[0] / total > 0.1 && (number === 1 ? rain === 0 : rain > 0.03)
        }).toBe(true)
      } else {
        await expect.poll(async () => {
          const pixels = await modelPixels(page, canvas)
          return pixels.colored / pixels.total
        }).toBeGreaterThan(0.1)
      }
      const screenshot = `grib-${caseName}-map-${step + 1}.png`
      await map.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, screenshot) })
      if (step === 1 && kind === 'temperature') {
        const viewport = map.locator('.raster-map')
        await expect.poll(async () => {
          const view = await displayedWorld(page, viewport)
          return view.north > 1 && view.south < view.height - 2 && view.south - view.north > 100
        }).toBe(true)
        const world = await displayedWorld(page, viewport)
        const rect = await viewport.boundingBox()
        const halfWorld = (world.south - world.north + 1) * rect.height / world.height
        const x = rect.x + rect.width * 0.8, y = rect.y + rect.height * 0.5
        await page.mouse.move(x, y)
        await page.mouse.down()
        await page.mouse.move(x - halfWorld, y, { steps: 20 })
        // Hold the final pointer position to avoid kinetic panning on release.
        await expect.poll(async () => (await displayedWorld(page, viewport)).seamColors).toBe(true)
        await page.mouse.up()
        await expect.poll(async () => (await displayedWorld(page, viewport)).seamColors).toBe(true)
        await viewport.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `grib-${caseName}-map-date-line.png`) })
        proof.date_line = { initial_view: world, pan_css_pixels: halfWorld, final_view: await displayedWorld(page, viewport) }
      }
      proof.visits.push({ message, preview_url: info.preview_url, screenshot,
        ...(precipitationColors ? { precipitation_style_verified: true, precipitation_colors: precipitationColors } : {}),
        ranges: ranges.slice(rangeStart).filter(r => r.path === url.pathname) })
      save()
    }
    expect(new Set(byMessage.values()).size).toBe(2)
    expect(posts.map(body => body.raster_message)).toEqual(['message-1', 'message-2'])
    proof.result = 'passed'
  } finally { save() }
}

for (const storage of ['minio', 'nfs']) {
  for (const kind of ['temperature', 'precipitation']) {
    test(`ordinary user generates two ${kind} maps and reuses each interval from ${storage}`, async ({ page }) => {
      test.setTimeout(240000)
      await verifyGRIBMaps(page, storage, kind)
    })
  }
}
