import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

const contextPath = '/api/v1/system/auth/context'

test('Redis live catalog and scan converge to keyspace contents and native previews through Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_REDIS_EXPECTATIONS'))
  const gateway = required('GATEWAY_URL')
  const api = await request.newContext({ baseURL: gateway, extraHTTPHeaders: {
    Authorization: `Bearer ${required('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')}`
  } })
  const apiIdentity = identity(await json(await api.get(contextPath), 'API AuthContext'))
  expect(apiIdentity.principalType).toBe('user')
  expect(apiIdentity.tenantID).toBe(String(expected.tenant_id))
  expect(apiIdentity.principalID).toBe(String(expected.principal_id))
  await api.dispose()
  // Meta restores the selected engine and loads its catalog during navigation.
  const catalogResponse = page.waitForResponse(response => response.request().method() === 'POST' &&
    new URL(response.url()).pathname === `/api/v1/system/engines/${expected.engine_id}/catalog/children` &&
    response.request().postDataJSON()?.path?.segments?.length > 0)
  const browserToken = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'),
    required('ADDP_ONLINE_TEST_USER_PASSWORD'), `/meta/scan?engine_id=${expected.engine_id}`)
  const browserAPI = await request.newContext({ baseURL: gateway, extraHTTPHeaders: { Authorization: `Bearer ${browserToken}` } })
  try {
    const browserIdentity = identity(await json(await browserAPI.get(contextPath), 'browser AuthContext'))
    expect(browserIdentity.principalID).toBe(apiIdentity.principalID)
    expect(browserIdentity.tenantID).toBe(apiIdentity.tenantID)
    expect(browserIdentity.contextType).toBe('tenant')
    expect([...browserIdentity.permissions].sort()).toEqual([...apiIdentity.permissions].sort())

    const meta = page.frameLocator('iframe[data-testid="module-iframe"]')
    const engineRow = meta.locator('.left-panel .el-table__body-wrapper tr').filter({ hasText: 'Hosted Redis' })
    await expect(engineRow).toHaveCount(1)
    const catalog = await json(await catalogResponse, 'Meta UI live System catalog')
    expect(catalog.nodes.map(node => node.name).sort()).toEqual(['keyspace'])
    await expect(meta.locator('.right-panel .el-table__body-wrapper tr')).toHaveCount(1)
    const submitted = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/meta/scan/run/manual' &&
      response.request().method() === 'POST')
    await meta.getByRole('button', { name: /^(重新扫描引擎|Rescan Engine)$/i }).click()
    const scan = await json(await submitted, 'Meta UI manual scan')
    expect(scan.execution_id).toBeTruthy()
    await expect.poll(async () => {
      const execution = await json(await browserAPI.get(`/api/v1/meta/executions/${encodeURIComponent(scan.execution_id)}`), 'Meta UI execution')
      if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error('Meta UI Redis scan failed')
      return execution.status
    }, { timeout: 120_000 }).toBe('success')
    await expect(meta.locator('.scan-status')).toContainText(/(扫描完成|completed)/i)
    await expect(meta.locator('.right-panel .el-table__body-wrapper tr')).toHaveCount(1)
    // Rescanning must retain the identities used by links, not create new DataItems.
    const rescanned = await json(await browserAPI.get(`/api/v1/meta/engines/${expected.engine_id}/items`), 'rescanned keys')
    expect(rescanned).toHaveLength(1)
    expect(rescanned[0].full_name).toBe('keyspace')
    expect(rescanned[0].id).toBe(expected.samples[0].item_id)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'redis-meta-console.png') })

    // Opening one deep link expands the real Meta-backed resource tree.
    const initial = expected.samples.at(-1)
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(initial.locator)}`)
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(manager.getByTestId('keyspace-keys')).toBeVisible()
    await expect(manager.locator('.explorer-tree .tree-node.keyspace')).toHaveCount(1)
    for (const sample of expected.samples) {
      // Click the real tree: its locator must retain type=keyspace and the scanned item_id.
      const previewResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
        new URL(response.url()).searchParams.get('locator') === sample.locator && new URL(response.url()).searchParams.get('key_name') === sample.key)
      const rawKey = Buffer.from(sample.key.slice(2), 'base64url')
      const label = sample.sample === 'binary' ? `Base64: ${rawKey.toString('base64')}` : JSON.stringify(rawKey.toString('utf8'))
      await manager.getByTestId('keyspace-keys').getByRole('button', { name: label, exact: true }).click()
      const payload = await json(await previewResponse, 'Console native Redis preview')
      expect(payload.preview_type).toBe('key_value')
      expect(payload.data.mode).toBe('key_value')
      const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
      const native = frame.getByTestId('key-value-preview')
      await expect(native).toBeVisible()
      await expect(native.locator('.el-descriptions')).toContainText(sample.native_type)
      if (sample.value) {
        await expect(native.getByTestId('key-value-string')).toHaveText(sample.value.value)
        await expect(native.locator('.el-tag')).toHaveText(sample.value.encoding === 'base64' ? 'Base64' : 'UTF-8')
      } else {
        const rows = native.locator('.el-table').last().locator('.el-table__body-wrapper tr')
        await expect(rows).toHaveCount(sample.entries.length)
        for (let index = 0; index < sample.entries.length; index += 1) {
          const entry = sample.entries[index]
          const row = ['hash', 'set'].includes(sample.sample)
            ? rows.filter({ hasText: entry.field?.value || entry.value.value }) : rows.nth(index)
          if (entry.field) await expect(row).toContainText(`UTF-8: ${entry.field.value}`)
          if (entry.value) await expect(row).toContainText(`UTF-8: ${entry.value.value}`)
          if (entry.score) await expect(row.locator('td').last()).toHaveText(entry.score)
          if (entry.fields) {
            await expect(row).toContainText(entry.id)
            await expect(row.locator('.stream-fields li')).toHaveText(entry.fields.map(field => `UTF-8: ${field.name.value} : UTF-8: ${field.value.value}`))
          }
        }
      }
      if (sample.sample === 'ttl') {
        const ttl = payload.data.key_value.facts.ttl_millis
        expect(ttl).toBeGreaterThan(0)
        expect(ttl).toBeLessThanOrEqual(3_600_000)
        await expect(native.locator('.el-descriptions')).toContainText(`${ttl} ms`)
      }
      await expect(frame.locator('.preview-container .el-pagination')).toHaveCount(0)
      await expect(frame.getByRole('tab', { name: /数据画像|Data Profile/i })).toHaveCount(0)
      await expect(frame.locator('.explorer-tree .tree-node.keyspace')).toHaveCount(1)
      await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `redis-${sample.sample}-console.png`) })
    }
    const prefixInput = manager.getByRole('textbox', { name: /^(键名前缀|Key prefix)$/ })
    const keys = manager.getByTestId('keyspace-keys')
    const filter = manager.getByRole('button', { name: /^(筛选|Filter)$/ })
    const filtered = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('key_prefix') === 'addp:sample:counter')
    await prefixInput.fill('addp:sample:counter')
    await filter.click()
    const filteredPayload = await json(await filtered, 'Console prefix filter')
    expect(filteredPayload.data.keyspace.keys.map(key => key.key)).toEqual([expected.samples.find(sample => sample.sample === 'counter').key])
    await expect(keys.locator('.el-table__body-wrapper tr')).toHaveCount(1)
    await expect(manager.getByTestId('key-value-string')).toHaveCount(0)
    const refreshed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('key_prefix') === 'addp:sample:counter' &&
      new URL(response.url()).searchParams.get('key_cursor') === '')
    await manager.getByRole('button', { name: /^(刷新|Refresh)$/ }).last().click()
    await json(await refreshed, 'Console filtered refresh')
    await expect(keys.locator('.el-table__body-wrapper tr')).toHaveCount(1)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'redis-prefix-console.png') })
    await prefixInput.fill('addp:sample:*')
    await filter.click()
    await expect(keys.locator('.el-table__body-wrapper tr')).toHaveCount(0)
    await expect(keys).toContainText(/当前批次没有可见键|No visible keys in this batch/)
    await expect(manager.getByRole('button', { name: /^(下一批|Next batch)$/ })).toBeDisabled()
    await prefixInput.fill('')
    await prefixInput.press('Enter')
    await expect(keys.locator('.el-table__body-wrapper tr')).toHaveCount(9)
    writeFileSync(required('ADDP_ONLINE_REDIS_BROWSER_REPORT'), JSON.stringify({
      run_id: required('ADDP_ONLINE_TEST_RUN_ID'), engine_id: expected.engine_id, tenant_id: expected.tenant_id,
      principal_id: browserIdentity.principalID, samples: expected.samples.map(sample => sample.sample), meta_ui_scan: true, prefix_filter: true
    }))
  } finally {
    await browserAPI.dispose()
  }
})
