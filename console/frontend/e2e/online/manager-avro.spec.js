import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

function expectedRow(i) {
  const value = (9007199254740993n + BigInt(i)) * (i % 2 ? -1n : 1n)
  const date = i < 4 ? ['0001-01-01', '9999-12-31', '2000-02-29', '1969-12-31'][i]
    : new Date(Date.UTC(1970, 0, 1) + (i - 1251) * 86400000).toISOString().slice(0, 10)
  const unscaled = (10n ** 38n - 10n ** 7n + BigInt(i) * 100n) * (i % 2 ? -1n : 1n)
  const decimal = n => {
    const digits = (n < 0n ? -n : n).toString().padStart(7, '0')
    return (n < 0n ? '-' : '') + digits.slice(0, -6) + '.' + digits.slice(-6)
  }
  const timestamps = {
    stamp_ms: [3, [0n, -1n, 1n, 946720800123n, -62135596800000n, 253402300799999n, 1730611800123n], 29],
    stamp_us: [6, [0n, -1n, 1n, 946720800123456n, -62135596800000000n, 253402300799999999n, 9007199254740993n, -9007199254740993n], 31],
    stamp_ns: [9, [0n, -1n, 1n, 946720800123456789n, -9223372036854775808n, 9223372036854775807n, 9007199254740993n, -9007199254740993n], 37],
  }
  const timestampValues = Object.fromEntries(Object.entries(timestamps).map(([name, [digits, ticks, period]]) => {
    if (i > 7 && i % period === 0) return [name, null]
    const count = ticks[i % ticks.length]
    const rate = 10n ** BigInt(digits)
    let seconds = count / rate
    let fraction = count % rate
    if (fraction < 0n) { seconds -= 1n; fraction += rate }
    // Only whole seconds enter Date; the exact fractional tick is formatted
    // separately, so the minimum int64 ns remains a valid instant.
    return [name, new Date(Number(seconds) * 1000).toISOString().slice(0, 19)
      + '.' + fraction.toString().padStart(digits, '0') + 'Z']
  }))
  return { id: value.toString(), small: i - 1251,
    flag: i % 7 === 0 ? null : i % 2 === 0,
    label: i % 11 === 0 ? null : i % 5 === 0 ? '' : `设备-${String(i).padStart(4, '0')}`,
    ratio: i / 4, value: i === 0 ? 0 : -i / 2,
    payload: i % 13 === 0 ? null : i % 3 === 0 ? '' : Buffer.from([0, i % 256, 255]).toString('base64'),
    day: i > 3 && i % 17 === 0 ? null : date,
    amount: i % 19 === 0 ? null : decimal(unscaled), fixed_amount: i % 23 === 0 ? null : decimal(-unscaled), ...timestampValues }
}

test('ordinary user pages and downloads six Avro scalar sources', async ({ page }) => {
  test.setTimeout(300_000)
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_AVRO_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.map(s => `${s.storage}/${s.codec}`)).toEqual([
    'minio/null', 'minio/deflate', 'minio/snappy', 'nfs/null', 'nfs/deflate', 'nfs/snappy',
  ])
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(identity.permissions.size).toBe(11)
  const cases = []
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of context.cases) {
      const observed = page.waitForResponse(response => {
        const u = new URL(response.url())
        return u.pathname === '/api/v1/manager/preview' && u.searchParams.get('locator') === source.locator
      })
      observed.catch(() => {})
      await page.goto('/manager/data-explorer?locator=' + encodeURIComponent(source.locator))
      const response = await observed
      expect(response.status()).toBe(200)
      const api = await browserAPIFromResponse(response, env.GATEWAY_URL, identity)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        await expect(frame.getByRole('columnheader', { name: 'id', exact: true })).toBeVisible()
        await expect(frame.getByText('9007199254740993', { exact: true }).first()).toBeVisible()
        const next = page.waitForResponse(response => {
          const u = new URL(response.url())
          return u.pathname === '/api/v1/manager/preview' && u.searchParams.get('locator') === source.locator && u.searchParams.get('page') === '2'
        })
        next.catch(() => {})
        await frame.locator('.el-pagination .btn-next').click()
        const second = await json(await next, 'Avro actual page button')
        expect(second.preview_type).toBe('table')
        expect(second.data.page).toBe(2)
        expect(second.data.total).toBe(2503)
        const offset = second.data.page_size
        expect(second.data.rows).toEqual(Array.from({ length: second.data.rows.length }, (_, n) => expectedRow(offset + n)))
        await expect(frame.getByText(expectedRow(offset).id, { exact: true }).first()).toBeVisible()
        for (const field of ['day', 'amount', 'fixed_amount', 'stamp_ms', 'stamp_us', 'stamp_ns']) {
          const value = second.data.rows.find(row => row[field] !== null)[field]
          const cell = frame.getByText(value, { exact: true }).first()
          await cell.scrollIntoViewIfNeeded()
          await expect(cell).toBeVisible()
        }
        if (source.storage === 'nfs') {
          const actions = await json(await api.get('/api/v1/manager/resource-actions', {
            params: { locator: source.locator }
          }), 'Avro current browser download capability')
          expect(actions.actions.download.supported).toBe(false)
          await expect(frame.getByRole('button', { name: /^(下载|Download)$/ })).toHaveCount(0)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-${source.codec}-page2.png`) })
          cases.push({ locator: source.locator, page_button_verified: true, values_verified: true, original_download_disabled: true })
          continue
        }
        const ready = page.waitForEvent('download')
        await frame.getByRole('button', { name: /^(下载|Download)$/ }).click()
        const download = await ready
        try {
          expect(download.suggestedFilename()).toBe(source.spec.name)
          const stream = await download.createReadStream()
          const chunks = []
          for await (const chunk of stream) chunks.push(chunk)
          const raw = Buffer.concat(chunks)
          const hash = createHash('sha256').update(raw).digest('hex')
          expect(raw.length).toBe(source.spec.size_bytes)
          expect(hash).toBe(source.spec.sha256)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-${source.codec}-page2.png`) })
          cases.push({ locator: source.locator, page_button_verified: true, values_verified: true,
            download_sha256: hash, download_size: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-avro-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-avro-browser/v3', result: passed ? 'passed' : 'failed',
      run_id: context.run_id, cases,
    }, null, 2) + '\n')
  }
})
