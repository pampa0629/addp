import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test('HDFS 扫描、四个文件预览及正式 Spark 结果通过 Console 收敛', async ({ page }) => {
  const expected = JSON.parse(process.env.ADDP_ONLINE_HDFS_EXPECTATIONS)
  const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME,
    process.env.ADDP_ONLINE_TEST_USER_PASSWORD, `/meta/scan?engine_id=${expected.engine_id}`)
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  const screenshot = name => page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `hdfs-${name}-console.png`) })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(auth.principalType).toBe('user')
    expect(auth.contextType).toBe('tenant')
    expect(auth.tenantID).toBe(String(expected.tenant_id))
    expect(auth.principalID).toBe(expected.principal_id)
    const meta = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(meta.locator('.left-panel .el-table__body-wrapper tr').filter({ hasText: 'Hosted HDFS' })).toHaveCount(1)
    const samples = meta.locator('.right-panel .el-table__body-wrapper tr').filter({ hasText: 'samples' })
    await expect(samples).toHaveCount(1)
    const submitted = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/meta/scan/run/manual' && response.request().method() === 'POST')
    await samples.getByRole('button', { name: /^(重新扫描|Rescan)$/i }).click()
    const scopeDialog = meta.getByRole('dialog', { name: /^(确认扫描范围与深度|Confirm scan scope and depth)$/i })
    await expect(scopeDialog).toBeVisible()
    await expect(scopeDialog).toContainText('Hosted HDFS')
    await expect(scopeDialog).toContainText('samples')
    await expect(scopeDialog.getByRole('radio', { name: /^(基础扫描|Basic Scan)/i })).toBeChecked()
    await expect(scopeDialog.getByRole('checkbox')).toBeChecked()
    await scopeDialog.getByRole('button', { name: /^(确认并开始扫描|Confirm and start scan)$/i }).click()
    const scanResponse = await submitted
    expect(scanResponse.request().postDataJSON()).toEqual({
      engine_id: expected.engine_id, scan_depth: 'basic', force: true,
      trigger_type: 'manual', source: 'meta', catalog_paths: ['samples']
    })
    const scan = await json(scanResponse, 'Meta UI scan')
    await expect.poll(async () => {
      const execution = await json(await api.get(`/api/v1/meta/executions/${scan.execution_id}`), 'Meta execution')
      if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error('HDFS UI scan failed')
      return execution.status
    }, { timeout: 120_000 }).toBe('success')
    await expect(meta.locator('.scan-status')).toContainText(/所选范围扫描已完成|The selected scope has been scanned/i)
    const items = await json(await api.get(`/api/v1/meta/engines/${expected.engine_id}/items`), 'rescanned files')
    for (const [name, locator] of Object.entries(expected.locators)) {
      const path = new URL(locator).pathname.replace(/^\/[0-9]+\/path\//, '').split('/').map(decodeURIComponent).join('/')
      expect(items.filter(item => item.full_name === path).map(item => item.id)).toEqual([expected.item_ids[name]])
    }
    await screenshot('meta')
    for (const [name, locator] of Object.entries(expected.locators)) {
      const received = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
        new URL(response.url()).searchParams.get('locator') === locator)
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
      const preview = await json(await received, 'HDFS file preview')
      expect(preview.preview_type).toBe('table')
      expect(preview.data.rows).toHaveLength(20)
      expect(preview.data.rows.map(row => Number(row.amount)).reduce((a, b) => a + b, 0)).toBe(2100)
      const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
      await expect(manager.locator('.table-preview .el-table__body-wrapper tr')).toHaveCount(20)
      await screenshot(name)
    }
    const currentIdentity = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/auth/context' && response.ok())
    const receivedExecution = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1/develop/executions/${expected.execution_id}` && response.request().method() === 'GET')
    await page.goto(`/develop/executions/${expected.execution_id}`)
    expect(identity(await json(await currentIdentity, 'Develop browser identity'))).toEqual(auth)
    const execution = await json(await receivedExecution, 'Develop page execution')
    expect(execution.status).toBe('success')
    expect(execution.metadata.result.final_result).toEqual(expected.final_result)
    expect(execution.metadata.result.runtime_execution_id).toBe(expected.runtime_execution_id)
    const detail = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.execution-detail-page')
    await expect(detail).toBeVisible()
    await expect(detail).toContainText(expected.execution_id)
    await expect(detail.locator('.toolbar .el-tag').filter({ hasText: /成功|Success/i })).toBeVisible()
    const result = detail.locator('.workflow-final-result-json')
    await expect.poll(async () => JSON.parse(await result.innerText())).toEqual(expected.final_result)
    await screenshot('workflow')
    for (const [prefix, persistence, resourceType, writeMode] of [
      ['', expected.persistence, 'table', 'replace'],
      ['minio-', expected.minio, 'object', 'create']
    ]) {
      const savedPreview = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
        new URL(response.url()).searchParams.get('locator') === persistence.preview_locator)
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(persistence.preview_locator)}`)
      const persisted = await json(await savedPreview, 'persisted table preview')
      expect(persisted.preview_type).toBe('table')
      expect(persisted.data.rows.slice().sort((a, b) => a.region.localeCompare(b.region)))
        .toEqual(persistence.manager_preview_rows.slice().sort((a, b) => a.region.localeCompare(b.region)))
      await expect(page.frameLocator('iframe[data-testid="module-iframe"]').locator('.table-preview .el-table__body-wrapper tr')).toHaveCount(2)
      await screenshot(prefix ? 'minio' : 'persisted')
      for (const [name, id, final] of [
        ['save', persistence.execution_id, persistence.final_result],
        ['reuse', persistence.reuse_execution_id, persistence.reuse_final_result]
      ]) {
        const received = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1/develop/executions/${id}` && response.request().method() === 'GET')
        await page.goto(`/develop/executions/${id}`)
        const value = await json(await received, name + ' execution')
        expect(value.status).toBe('success')
        expect(value.metadata.result.final_result).toEqual(final)
        if (name === 'save') expect(value.outputs.save.resource).toEqual({ locator: persistence.target_locator, type: resourceType, write_mode: writeMode })
        const result = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.workflow-final-result-json')
        await expect.poll(async () => JSON.parse(await result.innerText())).toEqual(final)
        await screenshot(prefix + name)
      }
    }
    const tables = {}
    for (const engine of ['mysql', 'doris']) {
      const facts = expected.tables[engine]
      const received = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/preview' &&
        new URL(response.url()).searchParams.get('locator') === facts.overwrite.preview_locator)
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(facts.overwrite.preview_locator)}`)
      const preview = await json(await received, `${engine} exact preview`)
      expect(preview.preview_type).toBe('table')
      expect(preview.data.columns).toEqual(['id', 'label', 'value'])
      expect(preview.data.rows.every(row => typeof row.id === 'string')).toBe(true)
      const sort = rows => rows.slice().sort((a, b) => BigInt(a.id) < BigInt(b.id) ? -1 : BigInt(a.id) > BigInt(b.id) ? 1 : 0)
      expect(sort(preview.data.rows)).toEqual(facts.physical_after.rows)
      const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
      const rows = manager.locator('.table-preview .el-table__body-wrapper tr')
      await expect(rows).toHaveCount(5)
      // Read real DOM cells without whitespace normalization: trailing spaces,
      // empty strings and null must remain three distinct states.
      const observed = []
      for (const row of await rows.all()) {
        const cells = row.locator('td .scalar-cell')
        await expect(cells).toHaveCount(3)
        const values = []
        for (const cell of await cells.all()) {
          values.push((await cell.getAttribute('class')).split(' ').includes('is-null')
            ? null : await cell.locator(':scope > span').first().textContent())
        }
        observed.push({ id: values[0], label: values[1], value: values[2] })
      }
      expect(sort(observed)).toEqual(facts.physical_after.rows)
      await screenshot(`${engine}-preview`)
      tables[engine] = {}
      for (const phase of ['created', 'overwrite', 'reuse']) {
        const accepted = facts[phase]
        const response = page.waitForResponse(value => new URL(value.url()).pathname === `/api/v1/develop/executions/${accepted.execution_id}` && value.request().method() === 'GET')
        await page.goto(`/develop/executions/${accepted.execution_id}`)
        const execution = await json(await response, `${engine} ${phase} execution`)
        expect(execution.status).toBe('success')
        expect(execution.metadata.result.final_result).toEqual(accepted.final_result)
        expect(execution.metadata.result.runtime_execution_id).toBe(accepted.runtime_status.execution_id)
        if (phase !== 'reuse') {
          expect(execution.outputs.save.resource).toEqual({ locator: accepted.target_locator, type: 'table', write_mode: 'replace' })
          expect(execution.metadata.result.meta_scan_runs[0].execution_id).toBe(accepted.scan_execution_id)
        }
        const detail = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.execution-detail-page')
        await expect(detail).toContainText(accepted.execution_id)
        await expect(detail.locator('.toolbar .el-tag').filter({ hasText: /成功|Success/i })).toBeVisible()
        await expect.poll(async () => JSON.parse(await detail.locator('.workflow-final-result-json').innerText())).toEqual(accepted.final_result)
        await screenshot(`${engine}-${phase}`)
        tables[engine][phase] = accepted.execution_id
      }
    }
    writeFileSync(process.env.ADDP_ONLINE_HDFS_BROWSER_REPORT, JSON.stringify({
      run_id: process.env.ADDP_ONLINE_TEST_RUN_ID, engine_id: expected.engine_id, tenant_id: expected.tenant_id,
      principal_id: auth.principalID, execution_id: expected.execution_id,
      meta_ui_scan: true, previews: 6, develop_result: true,
      persist_execution_id: expected.persistence.execution_id, reuse_execution_id: expected.persistence.reuse_execution_id,
      minio_execution_id: expected.minio.execution_id, minio_reuse_execution_id: expected.minio.reuse_execution_id,
      tables
    }))
  } finally {
    await api.dispose()
  }
})
