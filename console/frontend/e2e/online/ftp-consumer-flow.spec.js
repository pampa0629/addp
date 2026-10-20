import { expect, request, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { identity, json, login } from './transfer-browser-support.js'

const required = name => {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('FTP account root, scoped rescan, shared previews and byte-exact downloads through Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_FTP_EXPECTATIONS'))
  const token = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'), required('ADDP_ONLINE_TEST_USER_PASSWORD'), `/meta/scan?engine_id=${expected.engine_id}`)
  const api = await request.newContext({ baseURL: required('GATEWAY_URL'), extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'FTP browser identity'))
    expect(auth.principalType).toBe('user')
    expect(auth.contextType).toBe('tenant')
    expect(auth.tenantID).toBe(String(expected.tenant_id))
    expect(auth.principalID).toBe(expected.principal_id)
    expect([...auth.permissions].sort()).toEqual(['system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read', 'manager.data_item.read', 'manager.content.read'].sort())
    const meta = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(meta.locator('.left-panel .el-table__body-wrapper tr').filter({ hasText: 'Hosted FTP' })).toHaveCount(1)
    const examples = meta.locator('.right-panel .el-table__body-wrapper tr').filter({ hasText: 'examples' })
    await expect(examples).toHaveCount(1)
    const submitted = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/meta/scan/run/manual' && response.request().method() === 'POST')
    await examples.getByRole('button', { name: /^(重新扫描|Rescan)$/i }).click()
    const dialog = meta.getByRole('dialog', { name: /^(确认扫描范围与深度|Confirm scan scope and depth)$/i })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('examples')
    await dialog.getByRole('button', { name: /^(确认并开始扫描|Confirm and start scan)$/i }).click()
    const response = await submitted
    expect(response.request().postDataJSON().catalog_paths).toEqual(['examples'])
    const scan = await json(response, 'FTP UI scan')
    await expect.poll(async () => {
      const execution = await json(await api.get(`/api/v1/meta/executions/${encodeURIComponent(scan.execution_id)}`), 'FTP UI execution')
      if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error('FTP UI scan failed')
      return execution.status
    }, { timeout: 120_000 }).toBe('success')
    await expect(meta.locator('.scan-status')).toContainText(/所选范围扫描已完成|The selected scope has been scanned/i)
    const items = await json(await api.get(`/api/v1/meta/engines/${expected.engine_id}/items`), 'FTP rescanned files')
    const downloads = []
    for (const sample of expected.samples) {
      expect(items.filter(item => item.full_name === sample.full_name).map(item => item.id)).toEqual([sample.item_id])
      const received = page.waitForResponse(value => new URL(value.url()).pathname === '/api/v1/manager/preview' && new URL(value.url()).searchParams.get('locator') === sample.locator)
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(sample.locator)}`)
      const preview = await json(await received, 'FTP file preview')
      expect(preview.preview_type).toBe('table')
      expect(preview.data.columns.slice().sort()).toEqual(['amount', 'id', 'name'])
      expect(preview.data.rows).toHaveLength(3)
      const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
      const rows = manager.locator('.table-preview .el-table__body-wrapper tr')
      await expect(rows).toHaveCount(3)
      const observed = []
      for (const row of await rows.all()) {
        const cells = row.locator('td .scalar-cell')
        await expect(cells).toHaveCount(3)
        const values = await cells.allTextContents()
        const record = Object.fromEntries(preview.data.columns.map((column, i) => [column, values[i]]))
        observed.push({ id: Number(record.id), name: record.name, amount: Number(record.amount) })
      }
      expect(observed.sort((a, b) => a.id - b.id)).toEqual([
        { id: 1, name: '长沙客户', amount: 100.25 }, { id: 2, name: '株洲客户', amount: 200.5 }, { id: 3, name: '湘潭客户', amount: 300.75 }
      ])
      const downloaded = page.waitForEvent('download')
      await manager.getByRole('button', { name: /^(下载|Download)$/i }).click()
      const download = await downloaded
      expect(await download.failure()).toBeNull()
      expect(download.suggestedFilename()).toBe(sample.full_name.split('/').at(-1))
      const binary = readFileSync(await download.path())
      expect(binary.length).toBe(sample.size)
      expect(createHash('sha256').update(binary).digest('hex')).toBe(sample.sha256)
      downloads.push(Object.fromEntries(['format', 'item_id', 'size', 'sha256'].map(key => [key, sample[key]])))
    }
    writeFileSync(required('ADDP_ONLINE_FTP_BROWSER_REPORT'), JSON.stringify({
      run_id: expected.run_id, engine_id: expected.engine_id, tenant_id: expected.tenant_id,
      principal_id: auth.principalID, meta_ui_scan: true, previews: 3, downloads
    }))
  } finally {
    await api.dispose()
  }
})
