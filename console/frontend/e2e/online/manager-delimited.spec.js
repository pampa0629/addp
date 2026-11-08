import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

const columns = ['id', '名称𠀀😀', '备注']
const dataRows = Array.from({ length: 25 }, (_, i) => [String(i), `城市𠀀😀${i}`, '第一行\n第二行,含逗号'])

test('ordinary user applies GB18030 and preserves read settings for four CSV/TSV sources', async ({ page }) => {
  test.setTimeout(420_000)
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_DELIMITED_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.map(s => `${s.storage}/${s.spec.format}`)).toEqual(['minio/csv', 'minio/tsv', 'nfs/csv', 'nfs/tsv'])
  const identity = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(identity.permissions.size).toBe(10)
  const cases = []
  const pageErrors = []
  page.on('pageerror', error => pageErrors.push(error.message))
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of context.cases) {
      const observed = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1/meta/items/${source.item_id}`)
      observed.catch(() => {})
      const route = '/manager/data-explorer?locator=' + encodeURIComponent(source.locator)
      await page.goto(route)
      const api = await browserAPIFromResponse(await observed, env.GATEWAY_URL, identity)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        const selector = frame.locator('.csv-encoding-select')
        const header = frame.locator('.csv-header-select')
        const table = frame.locator('.table-preview')
        const next = frame.locator('.preview-panel .el-pagination .btn-next')
        const refreshed = () => page.waitForResponse(response => {
          const u = new URL(response.url())
          return response.request().method() === 'POST' && /\/api\/v1\/manager\/engines\/[1-9][0-9]*\/items\/refresh$/.test(u.pathname)
            && u.searchParams.get('locator') === source.locator
        })
        const metadata = async hasHeader => {
          const detail = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'delimited metadata')
          const phase = hasHeader ? 'header' : 'headerless'
          writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `delimited-${source.storage}-${source.spec.format}-${phase}-meta-observed.json`), JSON.stringify(detail, null, 2) + '\n')
          expect(detail.fingerprint).toBe(source.fingerprint)
          expect(detail).not.toHaveProperty('row_count')
          expect(Object.keys(detail.attributes).sort()).toEqual(['item', 'schema_version', 'type_info'])
          expect(detail.attributes.schema_version).toBe(1)
          expect(detail.attributes.type_info.table).not.toHaveProperty('row_count')
          expect(detail.attributes.type_info.table).not.toHaveProperty('native')
          expect(detail.attributes.type_info.table.fields.map(f => f.name)).toEqual(hasHeader ? columns : ['Column1', 'Column2', 'Column3'])
          const content = await json(await api.get('/api/v1/manager/preview', {
            params: { locator: source.locator, page: 1, page_size: 20 }
          }), 'delimited guarded content metadata')
          writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `delimited-${source.storage}-${source.spec.format}-${phase}-content-observed.json`), JSON.stringify(content, null, 2) + '\n')
          expect(content.preview_type).toBe('table')
          expect(content.data.total).toBe(hasHeader ? 25 : 26)
          const observed = content.data.item_meta
          expect(observed.fingerprint).toBe(source.fingerprint)
          expect(observed.row_count).toBe(hasHeader ? 25 : 26)
          expect(Array.isArray(observed.attributes)).toBe(true)
          expect(new Set(observed.attributes.map(a => a.key)).size).toBe(observed.attributes.length)
          const attrs = Object.fromEntries(observed.attributes.map(a => [a.key, a.value]))
          expect(attrs.item).toEqual({ format: source.spec.format, data_type: 'table', layout: 'single' })
          expect(attrs.format_info[source.spec.format].encoding).toBe('gb18030')
          expect(attrs.type_info.table.row_count).toBe(hasHeader ? 25 : 26)
          expect(attrs.type_info.table.native.has_header).toBe(hasHeader)
          expect(attrs.type_info.table.fields.map(f => f.name)).toEqual(hasHeader ? columns : ['Column1', 'Column2', 'Column3'])
          expect(attrs.storage.content_hash).toBe(source.spec.sha256)
          expect(attrs.access_index?.table).toBeUndefined()
        }
        const cells = async (hasHeader, offset, count) => {
          await expect(table.locator('.el-table__header-wrapper th')).toHaveText(hasHeader ? columns : ['Column1', 'Column2', 'Column3'])
          await expect(table.getByRole('cell')).toHaveCount(count * 3)
          const rows = hasHeader ? dataRows : [columns, ...dataRows]
          expect((await table.getByRole('cell').allTextContents()).map(s => s.trim())).toEqual(rows.slice(offset, offset + count).flat())
        }
        const apply = async hasHeader => {
          await header.locator('.el-select__wrapper').click()
          await frame.getByRole('option', { name: hasHeader ? /首行为表头|First row is header/ : /首行为数据|First row is data/, exact: true }).click()
          const ready = refreshed()
          ready.catch(() => {})
          await frame.getByRole('button', { name: /应用读取设置并刷新|Apply read settings and refresh/ }).click()
          const response = await ready
          expect(response.status()).toBe(200)
          expect(response.request().postDataJSON()).toEqual({ encoding: 'gb18030', has_header: hasHeader })
          await metadata(hasHeader)
          await cells(hasHeader, 0, 20)
        }
        const ordinaryRefresh = async hasHeader => {
          const node = frame.locator('.el-tree-node.is-current')
          await expect(node).toContainText(source.spec.name)
          const ready = refreshed()
          ready.catch(() => {})
          await node.locator('.node-action').click()
          const response = await ready
          expect(response.status()).toBe(200)
          expect(response.request().postData()).toBeNull()
          await metadata(hasHeader)
        }
        const downloadOriginal = async phase => {
          const ready = page.waitForEvent('download')
          await frame.getByRole('button', { name: /^(下载|Download)$/ }).click()
          const download = await ready
          try {
            expect(download.suggestedFilename()).toBe(source.spec.name)
            const chunks = []
            for await (const chunk of await download.createReadStream()) chunks.push(chunk)
            const raw = Buffer.concat(chunks)
            const sha = createHash('sha256').update(raw).digest('hex')
            expect(raw.length).toBe(source.spec.size_bytes)
            expect(sha).toBe(source.spec.sha256)
            writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `delimited-button-${source.storage}-${source.spec.format}-${phase}.${source.spec.format}`), raw)
            return { size: raw.length, sha }
          } finally { await download.delete() }
        }
        await expect(selector).toContainText('UTF-8')
        await selector.locator('.el-select__wrapper').click()
        await frame.getByRole('option', { name: 'GB18030', exact: true }).click()
        await apply(true)
        await expect(next).toBeEnabled()
        await next.click()
        await cells(true, 20, 5)
        await expect(next).toBeDisabled()
        await frame.locator('.preview-panel').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `delimited-${source.storage}-${source.spec.format}-header.png`) })
        const downloaded = await downloadOriginal('header')
        await ordinaryRefresh(true)
        await page.goto(route)
        await expect(selector).toContainText('GB18030')
        await cells(true, 0, 20)
        await apply(false)
        await expect(next).toBeEnabled()
        await next.click()
        await cells(false, 20, 6)
        await expect(next).toBeDisabled()
        await frame.locator('.preview-panel').screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `delimited-${source.storage}-${source.spec.format}-headerless.png`) })
        expect(await downloadOriginal('headerless')).toEqual(downloaded)
        await ordinaryRefresh(false)
        await page.reload()
        await expect(selector).toContainText('GB18030')
        await expect(header).toContainText(/首行为数据|First row is data/)
        await cells(false, 0, 20)
        await apply(true)
        cases.push({ locator: source.locator, encoding_request_verified: true, header_mode_verified: true,
          ordinary_refresh_verified: true, reopened: true, header_restored: true,
          metadata_projection_verified: true, content_metadata_verified: true,
          original_button_download_verified: true, download_sha256: downloaded.sha, download_size: downloaded.size })
      } finally { await api.dispose() }
    }
    expect(pageErrors).toEqual([])
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-delimited-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-delimited-browser/v1', result: passed ? 'passed' : 'failed',
      run_id: context.run_id, cases, page_errors: pageErrors,
    }, null, 2) + '\n')
  }
})
