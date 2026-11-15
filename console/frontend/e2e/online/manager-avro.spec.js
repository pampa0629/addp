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

function expectedNestedRow(i) {
  const flat = expectedRow(i)
  const detail = empty => Object.fromEntries(['uid', 'amount', 'fixed_amount', 'day', 'stamp_ms', 'stamp_us', 'stamp_ns', 'payload', 'label']
    .map(name => [name, empty ? null : flat[name === 'uid' ? 'id' : name]]))
  return { id: flat.id, detail: i % 3 === 0 ? null : detail(i % 5 === 0),
    items: i % 7 === 0 ? null : i % 5 === 0 ? [] : [detail(false), null, detail(true)],
    grid: i % 11 === 0 ? null : [[flat.id, null], [], null], fixed_values: [null, flat.fixed_amount] }
}

function expectedEnumRow(i) {
  const symbols = ['z', 'A', 'a']
  const state = symbols[i % 3]
  const optional = i % 7 === 0 ? null : symbols[(i + 1) % 3]
  return { id: expectedRow(i).id, state, optional, detail: { state: symbols[(i + 2) % 3] },
    items: i % 5 === 0 ? [] : [state, null, optional], other: i % 2 === 0 ? 'OFF' : 'ON' }
}

function expectedMapRow(i) {
  const flat = expectedRow(i)
  const id = BigInt(flat.id)
  const keys = ['', 'sensor.A', 'a/b', '中文 ', '__proto__', 'constructor']
  const symbols = ['z', 'A', 'a']
  return { id: flat.id,
    metrics: i % 7 === 0 ? null : i % 5 === 0 ? {} : Object.fromEntries(keys.map((k, n) => [k, n % 3 === 1 ? null : (id + BigInt(n)).toString()])),
    details: { '内容': { uid: flat.id, amount: expectedRow(i % 19 === 0 ? i + 1 : i).amount, day: '1969-12-31',
      stamp: expectedRow(i % 8).stamp_ns, payload: Buffer.from([0, 255, i % 256]).toString('base64') }, none: null },
    states: { z: symbols[i % 3], A: symbols[(i + 1) % 3] }, groups: [{}, { 'a.b': [null, symbols[i % 3]] }],
    nested: { outer: { '': flat.id, nil: null }, empty: {} } }
}

test('ordinary user previews flat, nested, Enum and Map Avro sources', async ({ page }) => {
  test.setTimeout(420_000)
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_AVRO_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.map(s => `${s.storage}/${s.codec}`)).toEqual([
    'minio/null', 'minio/deflate', 'minio/snappy', 'nfs/null', 'nfs/deflate', 'nfs/snappy',
  ])
  expect(context.nested_cases.map(s => `${s.storage}/${s.codec}`)).toEqual(context.cases.map(s => `${s.storage}/${s.codec}`))
  expect(context.map_cases.map(s => `${s.storage}/${s.codec}`)).toEqual(context.cases.map(s => `${s.storage}/${s.codec}`))
  expect(context.enum_cases.map(s => `${s.storage}/${s.codec}`)).toEqual(context.cases.map(s => `${s.storage}/${s.codec}`))
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(identity.permissions.size).toBe(11)
  const cases = []
  const nestedCases = []
  const enumCases = []
  const mapCases = []
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of [...context.cases, ...context.nested_cases, ...context.enum_cases, ...context.map_cases]) {
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
        expect(second.data.rows).toEqual(Array.from({ length: second.data.rows.length }, (_, n) => (source.map ? expectedMapRow : source.enum ? expectedEnumRow : source.nested ? expectedNestedRow : expectedRow)(offset + n)))
        await expect(frame.getByText(expectedRow(offset).id, { exact: true }).first()).toBeVisible()
        if (source.nested) {
          const detailIndex = second.data.rows.findIndex(row => row.detail?.uid && row.items?.length && row.grid?.length && row.fixed_values?.length)
          expect(detailIndex).toBeGreaterThanOrEqual(0)
          const detailRow = frame.locator('.el-table__body tr').nth(detailIndex)
          for (const [buttonIndex, field] of ['detail', 'items', 'grid', 'fixed_values'].entries()) {
            await detailRow.locator('button.structured-cell').nth(buttonIndex).click()
            const dialog = frame.getByRole('dialog', { name: field, exact: true })
            await expect(dialog).toBeVisible()
            await expect(dialog.locator('.structured-value-json')).toHaveText(JSON.stringify(second.data.rows[detailIndex][field], null, 2))
            if (field === 'detail') await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-nested-${source.codec}-detail.png`) })
            await dialog.locator('.el-dialog__headerbtn').click()
            await expect(dialog).not.toBeVisible()
          }
        } else if (source.map) {
          const detailIndex = second.data.rows.findIndex(row => row.metrics !== null && Object.keys(row.metrics).length > 0)
          expect(detailIndex).toBeGreaterThanOrEqual(0)
          const detailRow = frame.locator('.el-table__body tr').nth(detailIndex)
          for (const [index, field] of ['metrics', 'details', 'states', 'groups', 'nested'].entries()) {
            await detailRow.locator('button.structured-cell').nth(index).click()
            const dialog = frame.getByRole('dialog', { name: field, exact: true })
            await expect(dialog).toBeVisible()
            await expect(dialog.locator('.structured-value-json')).toHaveText(JSON.stringify(second.data.rows[detailIndex][field], null, 2))
            if (field === 'metrics') await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-map-${source.codec}-detail.png`), animations: 'disabled' })
            await dialog.locator('.el-dialog__headerbtn').click()
            await expect(dialog).not.toBeVisible()
          }
          await frame.getByRole('tab', { name: /^(属性|Attributes)$/ }).click()
          await expect(frame.getByRole('columnheader', { name: /^(映射值类型|Map value type)$/ })).toBeVisible()
          await frame.getByRole('button', { name: /JSON/ }).click()
          const attrsDialog = frame.locator('.attribute-json')
          await expect(attrsDialog).toBeVisible()
          const attrs = JSON.parse(await attrsDialog.innerText())
          const fields = attrs.type_info.table.fields
          expect(fields[1].map_value).toMatchObject({ type: 'bigint', nullable: true })
          expect(fields[1].fields).toBeUndefined()
          expect(fields[2].map_value.fields[1]).toMatchObject({ type: 'decimal', precision: 38, scale: 6 })
          expect(fields[2].map_value.fields[3].temporal).toEqual({ unit: 'ns', timezone: 'UTC' })
          expect(fields[3].map_value.enum).toEqual({ name: 'demo.MapState', symbols: ['z', 'A', 'a'] })
          expect(fields[4].element.map_value.element.enum).toEqual(fields[3].map_value.enum)
          expect(fields[5].map_value.map_value).toMatchObject({ type: 'bigint', nullable: true })
          expect(attrs.format_info.avro.schema).toEqual(source.spec.schema)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-map-${source.codec}-declaration.png`), animations: 'disabled' })
          await attrsDialog.locator('xpath=ancestor::div[contains(@class,"el-dialog")]').locator('.el-dialog__headerbtn').click()
          await frame.getByRole('tab', { name: /^(预览|Preview)$/ }).click()
          await expect(frame.getByRole('columnheader', { name: 'id', exact: true })).toBeVisible()
        } else if (source.enum) {
          const detailRow = frame.locator('.el-table__body tr').nth(1)
          for (const [index, field] of ['detail', 'items'].entries()) {
            await detailRow.locator('button.structured-cell').nth(index).click()
            const dialog = frame.getByRole('dialog', { name: field, exact: true })
            await expect(dialog).toBeVisible()
            await expect(dialog.locator('.structured-value-json')).toHaveText(JSON.stringify(second.data.rows[1][field], null, 2))
            await dialog.locator('.el-dialog__headerbtn').click()
            await expect(dialog).not.toBeVisible()
          }
          await frame.getByRole('tab', { name: /^(属性|Attributes)$/ }).click()
          await expect(frame.getByRole('columnheader', { name: /^(枚举声明|Enumeration)$/ })).toBeVisible()
          await frame.getByRole('button', { name: /JSON/ }).click()
          const attrsDialog = frame.locator('.attribute-json')
          await expect(attrsDialog).toBeVisible()
          const attrs = JSON.parse(await attrsDialog.innerText())
          const fields = attrs.type_info.table.fields
          const enumeration = { name: 'demo.State', symbols: ['z', 'A', 'a'] }
          for (const f of [fields[1], fields[2], fields[3].fields[0], fields[4].element]) expect(f.enum).toEqual(enumeration)
          expect(fields[5].enum).toEqual({ name: 'other.State', symbols: ['OFF', 'ON'] })
          // Meta's metadata-only API is checked separately by the owner case.
          // This User also has source read access: the authorized Manager preview
          // attaches complete item facts, whose schema must match this source.
          expect(attrs.format_info.avro.schema).toEqual(source.spec.schema)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-enum-${source.codec}-declaration.png`), animations: 'disabled' })
          await attrsDialog.locator('xpath=ancestor::div[contains(@class,"el-dialog")]').locator('.el-dialog__headerbtn').click()
          await frame.getByRole('tab', { name: /^(预览|Preview)$/ }).click()
          await expect(frame.getByRole('columnheader', { name: 'id', exact: true })).toBeVisible()
          await expect(frame.getByText(expectedRow(offset).id, { exact: true }).first()).toBeVisible()
        } else {
          for (const field of ['day', 'amount', 'fixed_amount', 'stamp_ms', 'stamp_us', 'stamp_ns']) {
            const value = second.data.rows.find(row => row[field] !== null)[field]
            const cell = frame.getByText(value, { exact: true }).first()
            await cell.scrollIntoViewIfNeeded()
            await expect(cell).toBeVisible()
          }
        }
        const outcomes = source.map ? mapCases : source.enum ? enumCases : source.nested ? nestedCases : cases
        const nestedProof = source.map ? { map_declaration_verified: true, structured_dialogs_verified: true } : source.enum ? { enum_declaration_verified: true, structured_dialogs_verified: true } : source.nested ? { nested_values_verified: true, structured_dialogs_verified: true } : {}
        if (source.storage === 'nfs') {
          const actions = await json(await api.get('/api/v1/manager/resource-actions', {
            params: { locator: source.locator }
          }), 'Avro current browser download capability')
          expect(actions.actions.download.supported).toBe(false)
          await expect(frame.getByRole('button', { name: /^(下载|Download)$/ })).toHaveCount(0)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-${source.map ? 'map-' : source.enum ? 'enum-' : source.nested ? 'nested-' : ''}${source.codec}-page2.png`) })
          outcomes.push({ ...nestedProof, locator: source.locator, page_button_verified: true, values_verified: true, original_download_disabled: true })
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
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `avro-${source.storage}-${source.map ? 'map-' : source.enum ? 'enum-' : source.nested ? 'nested-' : ''}${source.codec}-page2.png`) })
          outcomes.push({ ...nestedProof, locator: source.locator, page_button_verified: true, values_verified: true,
            download_sha256: hash, download_size: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-avro-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-avro-browser/v6', result: passed ? 'passed' : 'failed',
      run_id: context.run_id, cases, nested_cases: nestedCases, enum_cases: enumCases, map_cases: mapCases,
    }, null, 2) + '\n')
  }
})
