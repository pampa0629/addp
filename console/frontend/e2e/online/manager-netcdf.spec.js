import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

test('ordinary user selects classic and NetCDF-4 time slices and downloads exact originals', async ({ page }) => {
  test.setTimeout(300000)
  const env = process.env, context = JSON.parse(env.ADDP_ONLINE_MANAGER_NETCDF_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.length).toBe(14)
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id, permissions: new Set(context.identity.permissions_verified) }
  expect(identity.permissions.size).toBe(6)
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
      const response = await observe
      expect(response.status()).toBe(200)
      const api = await browserAPIFromResponse(response, env.GATEWAY_URL, identity)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        await expect(frame.getByTestId('grid-variable-details')).toContainText(source.spec.native_type)
        await frame.locator('.child-select .el-select__wrapper').click()
        await frame.getByRole('option', { name: '/time', exact: true }).click()
        await expect(frame.getByTestId('grid-variable-details')).toContainText('360_day')
        await expect(frame.getByTestId('read-grid-slice')).toHaveCount(0)
        await frame.locator('.child-select .el-select__wrapper').click()
        await frame.getByRole('option', { name: '/temperature', exact: true }).click()
        for (const layer of [0, 1]) {
          if (layer) {
            const input = frame.locator('.grid-controls .el-input-number').first().locator('input')
            await input.fill('1'); await input.press('Tab')
            await expect(frame.getByTestId('grid-slice-result')).toHaveCount(0)
          }
          const observed = page.waitForResponse(reply => {
            const url = new URL(reply.url())
            return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === source.locator && url.searchParams.has('grid_slice')
          }); observed.catch(() => {})
          await frame.getByTestId('read-grid-slice').click()
          const result = (await json(await observed, 'NetCDF actual slice button')).data.object.content.json
          const base = BigInt(source.spec.value_base)
          const values = Array.from({ length: 3 }, (_, y) => Array.from({ length: 4 }, (_, x) => layer === 0 && y === 1 && x === 2 ? source.spec.fill : String(base + BigInt(layer * 12 + y * 4 + x))))
          expect(result.selection).toEqual({ variable: '/temperature', indices: [layer], x: 0, y: 0, width: 4, height: 3 })
          expect(result.values).toEqual(values)
          expect(result.valid).toEqual(Array.from({ length: 3 }, (_, y) => Array.from({ length: 4 }, (_, x) => !(layer === 0 && y === 1 && x === 2))))
          await expect(frame.locator('.grid-window tbody tr')).toHaveCount(3)
          for (let y = 0; y < 3; y++) for (let x = 0; x < 4; x++) {
            const cell = frame.locator('.grid-window tbody tr').nth(y).locator('td').nth(x)
            await expect(cell).toHaveAttribute('data-raw', values[y][x])
            await expect(cell).toHaveAttribute('data-valid', String(result.valid[y][x]))
            if (result.valid[y][x]) await expect(cell).toHaveText(values[y][x])
          }
          const grid = frame.getByTestId('grid-slice-result')
          expect(await grid.evaluate(element => element.clientHeight >= element.querySelector('table').getBoundingClientRect().height)).toBe(true)
          await grid.scrollIntoViewIfNeeded()
          await expect(grid.locator('td').last()).toBeInViewport()
          if (source.spec.projected) {
            expect(result.spatial.transform).toEqual([499995, 10, 0, 2999995, 0, 10])
            await expect(frame.getByTestId('grid-no-location')).toHaveCount(0)
            await expect(frame.locator('.geojson-preview')).toBeVisible()
          } else {
            expect(result.spatial).toBeNull()
            await expect(frame.getByTestId('grid-no-location')).toBeVisible()
          }
        }
        if (source.spec.projected) {
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `netcdf-${source.source_case}-${source.spec.case_id}-grid.png`) })
          await frame.locator('.geojson-preview').scrollIntoViewIfNeeded()
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
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `netcdf-${source.source_case}-${source.spec.case_id}-page.png`) })
          outcomes.push({ locator: source.locator, values_verified: true, mask_verified: true, time_index_button_verified: true, download_sha256: digest, download_size: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    expect(browserErrors).toEqual([])
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-netcdf-browser.json'), JSON.stringify({ schema_version: 'addp.manager-netcdf-browser/v1', run_id: context.run_id, result: passed ? 'passed' : 'failed', cases: outcomes, browser_errors: browserErrors }, null, 2) + '\n')
  }
})
