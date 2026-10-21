import { expect, request, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { identity, json, login } from './transfer-browser-support.js'
import { modelPixels } from '../../../../manager/frontend/e2e/model-preview-support.js'
import { resolve } from 'node:path'

const required = name => {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

async function generateModel(page, api, model) {
  const route = `/manager/data-explorer?locator=${encodeURIComponent(model.locator)}`
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  let generationRequests = 0
  const pageContents = []
  const observe = requestEvent => {
    if (requestEvent.method() !== 'POST') return
    const path = new URL(requestEvent.url()).pathname
    if (path === '/api/v1/manager/quick-view/actions') {
      const body = requestEvent.postDataJSON()
      if (body.locator === model.locator && body.action === 'generate_model_3d_glb') generationRequests++
    } else if (/^\/api\/v1\/manager\/(?:task-provider\/)?tasks\/model_3d_glb_generation(?:\/[1-9]\d*\/execute)?$/.test(path)) {
      generationRequests++
    }
  }
  const observeContent = async response => {
    if (/^\/api\/v1\/manager\/model_3d_glb\/[1-9]\d*\/content$/.test(new URL(response.url()).pathname) && await response.finished() === null) pageContents.push(response)
  }
  page.on('request', observe)
  page.on('response', observeContent)
  try {
    await page.goto(route)
    const generated = page.waitForResponse(response => {
      if (new URL(response.url()).pathname !== '/api/v1/manager/quick-view/actions' || response.request().method() !== 'POST') return false
      const body = response.request().postDataJSON()
      return body.locator === model.locator && body.action === 'generate_model_3d_glb'
    })
    await frame.getByRole('button', { name: /生成 GLB 快显|Generate GLB Quick View/ }).click()
    const response = await generated
    expect(response.status()).toBe(202)
    const started = await json(response, 'FTP model UI generation')
    expect(started.task_type).toBe('model_3d_glb_generation')
    expect(Number.isSafeInteger(started.task_id) && started.task_id > 0).toBe(true)
    expect(started.execution_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
    await expect.poll(async () => {
      const execution = await json(await api.get(`/api/v1/manager/executions/${started.execution_id}`), 'FTP model execution')
      expect(execution.execution_id).toBe(started.execution_id)
      if (['failed', 'timeout', 'cancelled'].includes(execution.status)) throw new Error(`FTP ${model.format} model execution failed`)
      return execution.status
    }, { timeout: 120_000 }).toBe('success')
    const capabilityPath = `/api/v1/manager/quick-view/capability?${new URLSearchParams({ locator: model.locator })}`
    const ready = await json(await api.get(capabilityPath), 'FTP model ready capability')
    expect(ready.can_use_quick_view).toBe(true)
    expect(ready.render_source).toBe('model_3d_glb')
    const result = ready.model_3d
    expect(Number.isSafeInteger(result.result_id) && result.result_id > 0).toBe(true)
    expect(result.task_id).toBe(started.task_id)
    expect(result.last_execution_id).toBe(started.execution_id)
    expect(result.format).toBe(model.format)
    const contentPath = `/api/v1/manager/model_3d_glb/${result.result_id}/content`
    expect(result.preview_url).toBe(contentPath)
    const content = await api.get(contentPath)
    expect(content.status()).toBe(200)
    expect(content.headers()['content-type']).toContain('model/gltf-binary')
    const binary = await content.body()
    expect(binary.subarray(0, 4).toString()).toBe('glTF')
    const checksum = createHash('sha256').update(binary).digest('hex')
    const verifyPageContent = async loaded => {
      expect(new URL(loaded.url()).pathname).toBe(contentPath)
      expect(new URL(loaded.url()).origin).toBe(new URL(loaded.request().frame().url()).origin)
      expect(new URL(loaded.url()).search.length).toBe(0)
      expect(loaded.status()).toBe(200)
      const headers = await loaded.request().allHeaders()
      // Failed diagnostics must never print a Bearer, Cookie or ticket value.
      expect(/^Bearer addp_at_/.test(headers.authorization || '')).toBe(true)
      expect(Object.hasOwn(headers, 'cookie')).toBe(false)
      const bytes = await loaded.body()
      expect(bytes.length).toBe(binary.length)
      expect(createHash('sha256').update(bytes).digest('hex')).toBe(checksum)
      return bytes
    }
    await expect.poll(() => pageContents.filter(value => new URL(value.url()).pathname === contentPath).length,
      { timeout: 60_000 }).toBeGreaterThan(0)
    const actualPageBytes = await verifyPageContent(pageContents.find(value => new URL(value.url()).pathname === contentPath))
    writeFileSync(resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `ftp-${model.format}.glb`), actualPageBytes)
    const assertOnlyOriginalExecution = async () => {
      const task = await json(await api.get(`/api/v1/manager/tasks/model_3d_glb_generation/${started.task_id}`), 'FTP model task execution summary')
      expect(task.id).toBe(started.task_id)
      expect(task.task_type).toBe('model_3d_glb_generation')
      expect(task.semantic_key).toBe(model.fingerprint)
      expect(task.last_execution_id).toBe(started.execution_id)
      expect(task.last_execution_status).toBe('success')
    }
    const viewer = frame.locator('.model-preview')
    const rendered = async () => {
      await expect(viewer.locator('canvas')).toBeVisible({ timeout: 60_000 })
      await expect(viewer.locator('.three-status')).toHaveCount(0, { timeout: 60_000 })
      let pixels
      if (model.format === '3ds') {
        await expect.poll(async () => {
          pixels = await modelPixels(page, viewer.locator('canvas'))
          return pixels.red / pixels.total
        }).toBeGreaterThan(0.001)
        expect(pixels.red / pixels.total).toBeLessThan(0.85)
      } else {
        await expect.poll(async () => {
          pixels = await modelPixels(page, viewer.locator('canvas'))
          return pixels.neutralSolid / pixels.total
        }).toBeGreaterThan(0.01)
        expect(pixels.neutralSolid / pixels.total).toBeLessThan(0.85)
      }
    }
    await rendered()
    await viewer.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `ftp-${model.format}-rendered.png`) })
    expect(generationRequests).toBe(1)
    await assertOnlyOriginalExecution()
    const contentCount = pageContents.length
    await page.reload()
    await expect.poll(() => pageContents.slice(contentCount).filter(value => new URL(value.url()).pathname === contentPath).length,
      { timeout: 60_000 }).toBeGreaterThan(0)
    const reused = pageContents.slice(contentCount).find(value => new URL(value.url()).pathname === contentPath)
    await verifyPageContent(reused)
    await rendered()
    expect(generationRequests).toBe(1)
    await assertOnlyOriginalExecution()
    const current = await json(await api.get(capabilityPath), 'FTP model cached capability')
    expect(current.model_3d.result_id).toBe(result.result_id)
    expect(current.model_3d.last_execution_id).toBe(started.execution_id)
    const source = await api.get(`/api/v1/manager/downloads/file?${new URLSearchParams({ locator: model.locator })}`)
    expect(source.status()).toBe(200)
    const original = await source.body()
    expect(original.length).toBe(model.size)
    expect(createHash('sha256').update(original).digest('hex')).toBe(model.sha256)
    return { format: model.format, item_id: model.item_id, task_id: started.task_id,
      execution_id: started.execution_id, result_id: result.result_id,
      size: actualPageBytes.length, sha256: checksum,
      source_sha256: model.sha256, rendered: true, reopened: true, cached_without_execution: true }
  } finally {
    page.off('request', observe)
    page.off('response', observeContent)
  }
}

async function withdrawnModel(page, api, model, published, latest) {
  const contentPath = `/api/v1/manager/model_3d_glb/${published.result_id}/content`
  const deniedResponses = []
  let contentSuccesses = 0
  let generationRequests = 0
  const isPreview = url => url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === model.locator
  const observeResponse = response => {
    const url = new URL(response.url())
    if (url.pathname === contentPath && response.status() === 200) contentSuccesses++
    if ((url.pathname === contentPath || isPreview(url)) && response.status() === 403) deniedResponses.push(response)
  }
  const observeRequest = event => {
    if (event.method() !== 'POST') return
    const path = new URL(event.url()).pathname
    if (path === '/api/v1/manager/quick-view/actions' || /^\/api\/v1\/manager\/(?:task-provider\/)?tasks\/model_3d_glb_generation(?:\/[1-9]\d*\/execute)?$/.test(path)) generationRequests++
  }
  page.on('response', observeResponse)
  page.on('request', observeRequest)
  try {
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(model.locator)}`)
    await expect.poll(() => deniedResponses.length, { timeout: 60_000 }).toBeGreaterThan(0)
    const denied = deniedResponses[0]
    const payload = await denied.json()
    expect(typeof payload.error).toBe('string')
    expect(payload.error.length).toBeGreaterThan(0)
    expect(payload.error_code).not.toBe('security_protection_required')
    expect(Object.keys(payload).every(key => ['error', 'error_code'].includes(key))).toBe(true)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.locator('[data-testid="preview-failure"], .model-preview .three-status.is-error').first()).toBeVisible()
    if (new URL(denied.url()).pathname === contentPath) {
      const headers = await denied.request().allHeaders()
      expect(/^Bearer addp_at_/.test(headers.authorization || '')).toBe(true)
      expect(Object.hasOwn(headers, 'cookie')).toBe(false)
      expect(new URL(denied.url()).search).toBe('')
    }
    expect(contentSuccesses).toBe(0)
    expect(generationRequests).toBe(0)
    const task = await json(await api.get(`/api/v1/manager/tasks/model_3d_glb_generation/${published.task_id}`), 'FTP withdrawn task execution summary')
    expect(task.last_execution_id).toBe(latest.execution_id)
    expect(task.last_execution_status).toBe(latest.status)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `ftp-${model.format}-withdrawn.png`) })
    return { format: model.format, item_id: model.item_id, result_id: published.result_id,
      status: 403, error_visible: true, content_successes: 0, generation_requests: 0,
      unchanged_execution_id: latest.execution_id, unchanged_execution_status: latest.status }
  } finally {
    page.off('response', observeResponse)
    page.off('request', observeRequest)
  }
}

test('FTP account root, scoped rescan, shared previews and byte-exact downloads through Console', async ({ page }) => {
  test.setTimeout(480_000)
  const expected = JSON.parse(required('ADDP_ONLINE_FTP_EXPECTATIONS'))
  const token = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'), required('ADDP_ONLINE_TEST_USER_PASSWORD'), `/meta/scan?engine_id=${expected.engine_id}`)
  const api = await request.newContext({ baseURL: required('GATEWAY_URL'), extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'FTP browser identity'))
    expect(auth.principalType).toBe('user')
    expect(auth.contextType).toBe('tenant')
    expect(auth.tenantID).toBe(String(expected.tenant_id))
    expect(auth.principalID).toBe(expected.principal_id)
    expect([...auth.permissions].sort()).toEqual(['system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read', 'manager.data_item.read', 'manager.content.read',
      'manager.derived_artifact.create', 'manager.derived_artifact.read', 'manager.derived_artifact.delete', 'manager.data_item.update'].sort())
    if (expected.browser_phase === 'withdrawn') {
      const models = []
      for (const model of expected.models) {
        const published = expected.browser.models.find(value => value.format === model.format)
        const latest = expected.denied_generations?.findLast(row => row.format === model.format) ??
          (expected.failed_generation?.format === model.format ? expected.failed_generation :
            { execution_id: published.execution_id, status: 'success' })
        models.push(await withdrawnModel(page, api, model, published, latest))
      }
      writeFileSync(required('ADDP_ONLINE_FTP_BROWSER_REPORT'), JSON.stringify({
        run_id: expected.run_id, engine_id: expected.engine_id, tenant_id: expected.tenant_id,
        principal_id: auth.principalID, browser_phase: 'withdrawn', models
      }))
      return
    }
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
    const models = []
    expect(expected.models.map(model => model.format)).toEqual(['ifc', '3ds'])
    for (const model of expected.models) models.push(await generateModel(page, api, model))
    writeFileSync(required('ADDP_ONLINE_FTP_BROWSER_REPORT'), JSON.stringify({
      run_id: expected.run_id, engine_id: expected.engine_id, tenant_id: expected.tenant_id,
      principal_id: auth.principalID, meta_ui_scan: true, previews: 3, downloads, models
    }))
  } finally {
    await api.dispose()
  }
})
