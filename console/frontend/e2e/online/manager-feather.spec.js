import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromNavigation, json, login } from './transfer-browser-support.js'

function expectedRow(i) {
  const days = ['1969-12-31', '1970-01-01', '2000-02-29']
  const amounts = ['-99999999999999999999.123456789012345678', '1.230000000000000000', null]
  const wideAmounts = ['9'.repeat(38) + '.' + '7'.repeat(38), '-0.' + '0'.repeat(37) + '1', '0.' + '0'.repeat(38)]
  return { id: ((9007199254740993n + BigInt(i)) * (i % 2 ? -1n : 1n)).toString(),
    tiny: i % 256 - 128, small: i % 17 === 0 ? null : i - 1251, number: i * 100000 - 125100000,
    flag: i % 7 === 0 ? null : i % 2 === 0,
    label: i % 11 === 0 ? null : i % 5 === 0 ? '' : `设备𠀀😀-${String(i).padStart(4, '0')}`,
    ratio: i / 4, value: (0 - i) / 2,
    payload: i % 13 === 0 ? null : i % 3 === 0 ? '' : Buffer.from([0, i % 256, 255]).toString('base64'),
    day: days[i % 3], legacy_day: i % 3 === 0 ? null : days[(i + 1) % 3],
    amount: amounts[i % 3], wide_amount: wideAmounts[i % 3],
    clock_second: ['00:00:00', '23:59:59', '00:00:01'][i % 3],
    clock_nano: ['00:00:00.000000001', '23:59:59.999999999', null][i % 3],
    local_stamp: ['1969-12-31T23:59:59.999999999', '1677-09-21T00:12:43.145224192', '1970-01-01T00:00:00.000000000'][i % 3],
    zone_stamp: ['2021-11-07T05:30:00.000000001Z', '2021-11-07T06:30:00.000000001Z', null][i % 3],
    offset_stamp: ['1969-12-31T23:59:59Z', '1970-01-01T00:00:00Z', '2038-01-19T03:14:08Z'][i % 3] }
}

test('ordinary user pages and downloads six Feather V2 sources', async ({ page }) => {
  test.setTimeout(420_000)
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_FEATHER_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.map(s => `${s.storage}/${s.codec}`)).toEqual([
    'minio/uncompressed', 'minio/lz4', 'minio/zstd', 'nfs/uncompressed', 'nfs/lz4', 'nfs/zstd',
  ])
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(identity.permissions.size).toBe(11)
  const cases = [], pageErrors = []
  page.on('pageerror', error => pageErrors.push(error.message))
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of context.cases) {
      const matchesPreview = response => response.request().method() === 'GET' &&
        new URL(response.url()).pathname === '/api/v1/manager/preview' &&
        new URL(response.url()).searchParams.get('locator') === source.locator
      const api = await browserAPIFromNavigation(page, env.GATEWAY_URL, identity,
        () => page.goto('/manager/data-explorer?locator=' + encodeURIComponent(source.locator)), matchesPreview)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        await expect(frame.getByText(expectedRow(0).id, { exact: true }).first()).toBeVisible()
        const first = await json(await api.get('/api/v1/manager/preview', {
          params: { locator: source.locator, page: 1, page_size: 20 }
        }), 'Feather current browser first page')
        writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `feather-${source.storage}-${source.codec}-browser-page1.json`), JSON.stringify(first, null, 2) + '\n')
        expect(first.data.rows).toEqual(Array.from({ length: 20 }, (_, i) => expectedRow(i)))
        const next = page.waitForResponse(response => matchesPreview(response) &&
          new URL(response.url()).searchParams.get('page') === '2')
        next.catch(() => {})
        await frame.locator('.el-pagination .btn-next').click()
        const second = await json(await next, 'Feather actual next page')
        writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `feather-${source.storage}-${source.codec}-browser-page2.json`), JSON.stringify(second, null, 2) + '\n')
        expect(second.preview_type).toBe('table')
        expect(second.data.page).toBe(2)
        expect(second.data.total).toBe(2503)
        expect(second.data.rows).toEqual(Array.from({ length: second.data.rows.length }, (_, n) => expectedRow(second.data.page_size + n)))
        await expect(frame.getByText(expectedRow(second.data.page_size).id, { exact: true }).first()).toBeVisible()
        await frame.locator('.preview-panel').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `feather-${source.storage}-${source.codec}-page2.png`) })
        const ready = page.waitForEvent('download')
        await frame.getByRole('button', { name: /^(下载|Download)$/ }).click()
        const download = await ready
        try {
          expect(download.suggestedFilename()).toBe(source.spec.name)
          const chunks = []
          for await (const chunk of await download.createReadStream()) chunks.push(chunk)
          const raw = Buffer.concat(chunks), hash = createHash('sha256').update(raw).digest('hex')
          expect(raw.length).toBe(source.spec.size_bytes)
          expect(hash).toBe(source.spec.sha256)
          writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `feather-button-${source.storage}-${source.codec}.feather`), raw)
          cases.push({ locator: source.locator, page_button_verified: true, values_verified: true,
            original_button_download_verified: true, download_sha256: hash, download_size: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    expect(pageErrors).toEqual([])
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-feather-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-feather-browser/v1', result: passed ? 'passed' : 'failed',
      run_id: context.run_id, cases, page_errors: pageErrors,
    }, null, 2) + '\n')
  }
})
