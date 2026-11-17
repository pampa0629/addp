import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

test('ordinary user reads independent GRIB messages with native units and downloads originals', async ({ page }) => {
  test.setTimeout(480000)
  const env = process.env, context = JSON.parse(env.ADDP_ONLINE_MANAGER_GRIB_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases).toHaveLength(12)
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id, permissions: new Set(context.identity.permissions_verified) }
  expect([...identity.permissions].sort()).toEqual(['manager.content.read', 'manager.data_item.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read', 'monitor.execution.read'])
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
        for (const field of source.spec.fields) {
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
          if (source.spec.packing === 'complex-spatial') {
            await expect(details).toContainText('GRIB2_DECODE_PRECISION')
            await expect(details).toContainText('float32_scaling_float64_output')
            await expect(details).toContainText('GRIB2_PACKING')
            await expect(details).toContainText('GRIB2_TIME')
            if (field.product_template === 8) {
              await expect(details).toContainText('accumulation')
              await expect(details).toContainText('2026-01-02T03:00:00+00:00')
              await expect(details).toContainText('2026-01-02T09:00:00+00:00')
            }
          }
          await expect(frame.getByTestId('grid-native-transform')).toContainText(String(field.west - 0.5))
          await expect(frame.locator('.grid-container-preview')).toContainText(/不自动转换单位|no automatic unit conversion/)
          const observed = page.waitForResponse(reply => {
            const url = new URL(reply.url())
            return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === source.locator && url.searchParams.has('grid_slice')
          }); observed.catch(() => {})
          await frame.getByTestId('read-grid-slice').click()
          const result = (await json(await observed, 'GRIB actual slice button')).data.object.content.json
          const valid = Array.from({ length: field.height }, (_, y) => Array.from({ length: field.width }, (_, x) => !(field.missing && y === 1 && x === 1)))
          const values = valid.map((row, y) => row.map((ok, x) => !ok ? 'NaN' : field.missing && y === 0 && x === 0 ? '9999.0' : (() => { let value = field.base + (field.constant ? 0 : y * field.width + x) * (field.step ?? 1); if (field.packing === 'complex-spatial') { const integer = (value * 10 ** field.decimal_scale - field.reference) / 2 ** field.binary_scale; value = Math.fround(Math.fround(Math.fround(Math.fround(integer) * Math.fround(2 ** field.binary_scale)) + Math.fround(field.reference)) * Math.fround(10 ** -field.decimal_scale)) } return String(value) + (Number.isInteger(value) ? '.0' : '') })()))
          expect(result.selection).toEqual({ variable: `message-${field.number}`, indices: [], x: 0, y: 0, width: field.width, height: field.height })
          expect(result.values).toEqual(values); expect(result.valid).toEqual(valid)
          expect(result.value_mode).toBe('decoded_native_units'); expect(result.spatial).toBeNull()
          expect(result.variable.grid.transform).toEqual([field.west - 0.5, 1, 0, field.north + 0.5, 0, -1])
          await expect(frame.locator('.grid-window tbody tr')).toHaveCount(field.height)
          for (let y = 0; y < field.height; y++) for (let x = 0; x < field.width; x++) {
            const cell = frame.locator('.grid-window tbody tr').nth(y).locator('td').nth(x)
            await expect(cell).toHaveAttribute('data-raw', values[y][x])
            await expect(cell).toHaveAttribute('data-valid', String(valid[y][x]))
            if (valid[y][x]) await expect(cell).toHaveText(values[y][x])
          }
          const grid = frame.getByTestId('grid-slice-result')
          await grid.scrollIntoViewIfNeeded()
          await expect(grid.locator('td').first()).toBeInViewport({ ratio: 1 })
          await expect(grid.locator('td').last()).toBeInViewport({ ratio: 1 })
          await expect(grid.locator('td').last()).toHaveText(values.at(-1).at(-1))
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `grib-${source.source_case}-${source.spec.case_id}-message${field.number}-slice.png`) })
          await expect(frame.locator('.geojson-preview')).toHaveCount(0)
          await expect(frame.getByTestId('grid-no-location')).toContainText(/暂不提供地图预览|map preview is not available/)
        }
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
          outcomes.push({ locator: source.locator, values_verified: true, mask_verified: true, message_switch_verified: true, grid_verified: true, native_units_verified: true, viewport_grid_verified: true, visible_message_count: source.spec.fields.length, original_download_disabled: true, original_download_denied_verified: true })
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
          outcomes.push({ locator: source.locator, values_verified: true, mask_verified: true, message_switch_verified: true, grid_verified: true, native_units_verified: true, viewport_grid_verified: true, visible_message_count: source.spec.fields.length, download_sha256: digest, download_size: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    expect(browserErrors).toEqual([]); passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-grib-browser.json'), JSON.stringify({ schema_version: 'addp.manager-grib-browser/v2', run_id: context.run_id, result: passed ? 'passed' : 'failed', cases: outcomes, browser_errors: browserErrors }, null, 2) + '\n')
  }
})
