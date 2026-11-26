import { expect, request, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test.use({ locale: 'zh-CN' })

async function verifyTileService(page, api, evidence) {
  const serviceName = evidence.run_id.replaceAll('-', '_')
  const locator = `${evidence.target_locator}&item_id=${evidence.target_item_id}`
  const published = await json(await api.post('/api/v1/service/tile', { data: {
    service_name: serviceName, title: 'PMTiles 验收', public_access: false,
    protocols: { xyz: { enabled: true } },
    first_layer: { layer_name: evidence.layer, title: evidence.layer, layer_type: 'static', layer_config: { source: { locator } } }
  } }), 'publish PMTiles')
  expect(published.id).toBeGreaterThan(0)
  expect(published.status).toBe('active')
  expect(published.layers[0].layer_name).toBe(evidence.layer)
  const url = new URL(`/tiles/${serviceName}/${evidence.layer}/5/26/13.mvt`, process.env.GATEWAY_URL).href
  const report = { run_id: evidence.run_id, principal_id: evidence.principal_id, tenant_id: evidence.tenant_id,
    service_id: published.id, item_locator: locator, url, archive_tile_sha256: evidence.tile_sha256, reads: [] }
  const reportPath = resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'pmtiles-service-lifecycle.json')
  const readTile = async (phase, status) => {
    const response = await api.get(url, { maxRedirects: 0, headers: { 'Cache-Control': 'no-cache' } })
    const body = await response.body()
    const read = { phase, status: response.status(), content_type: response.headers()['content-type'] }
    if (response.status() === 200) {
      read.bytes = body.length
      read.sha256 = createHash('sha256').update(body).digest('hex')
    }
    report.reads.push(read)
    writeFileSync(reportPath, JSON.stringify(report, null, 2))
    expect(response.url()).toBe(url)
    expect(read.status).toBe(status)
    if (status === 200) {
      expect(read.content_type).toContain('application/vnd.mapbox-vector-tile')
      expect(read.bytes).toBeGreaterThan(0)
      expect(read.sha256).toBe(evidence.tile_sha256)
    } else {
      expect((await response.json()).error).toBeTruthy()
    }
  }
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const setActive = async (active) => {
    await frame.locator('.page-header').getByRole('button', { name: '编辑', exact: true }).click()
    const control = frame.getByRole('switch', { name: '启用瓦片服务' })
    await expect(control).toBeChecked({ checked: !active })
    await frame.locator('.el-switch').filter({ has: control }).click()
    const saved = page.waitForResponse(resp => new URL(resp.url()).pathname === `/api/v1/service/tile/${published.id}` && resp.request().method() === 'PUT')
    await frame.getByRole('button', { name: '保存', exact: true }).click()
    const result = await json(await saved, 'save service state')
    expect(result.status).toBe(active ? 'active' : 'inactive')
    await expect(frame.locator('.page-header').getByRole('button', { name: '编辑', exact: true })).toBeVisible()
  }
  await readTile('published', 200)
  await page.goto(`/service/tile/${published.id}`)
  await setActive(false)
  await readTile('inactive', 503)
  await setActive(true)
  await readTile('restored', 200)
}

test('PMTiles 真实生成与替换后地图、内容、身份一致', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_PMTILES_EVIDENCE || '{}')
  for (const field of ['run_id', 'principal_id', 'tenant_id', 'execution_id', 'phase', 'source_item_id', 'target_item_id', 'target_locator', 'layer', 'feature_count', 'tile_sha256']) {
    expect(evidence[field], field).toBeTruthy()
  }
  const redirect = `/manager/data-explorer?locator=${encodeURIComponent(`${evidence.target_locator}&item_id=${evidence.target_item_id}`)}`
  // Observe only: no API routes or responses are mocked in Hosted acceptance.
  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    window.pmtilesRequestCaches = []
    const nativeFetch = window.fetch
    window.fetch = (input, options) => {
      if (String(input).includes('/quick-view/tiles/')) window.pmtilesRequestCaches.push(options?.cache)
      return nativeFetch(input, options)
    }
  })
  const responses = []
  page.on('response', response => {
    if (new URL(response.url()).pathname.includes('/manager/quick-view/tiles/')) responses.push(response)
  })
  const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, redirect)
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'PMTiles browser identity'))
    expect(auth.principalID).toBe(evidence.principal_id)
    expect(auth.tenantID).toBe(evidence.tenant_id)
    expect(auth.principalType).toBe('user')
    expect(auth.contextType).toBe('tenant')
    expect([...auth.permissions].sort()).toEqual(evidence.permissions_verified)
    const preview = await json(await api.get(`/api/v1/manager/preview?locator=${encodeURIComponent(evidence.target_locator)}`), 'PMTiles preview')
    const content = preview.data.object.content
    expect(content.kind).toBe('vector_tile')
    expect(content.metadata.render_source).toBe('business_pmtiles')
    expect(content.metadata.min_zoom).toBe(0)
    expect(content.metadata.max_zoom).toBe(5)
    const tileURL = `/api/v1/manager/quick-view/tiles/5/26/13.mvt?locator=${encodeURIComponent(evidence.target_locator)}`
    const tile = await api.get(tileURL, { headers: { 'Cache-Control': 'no-cache' } })
    expect(tile.status()).toBe(200)
    expect(tile.headers()['cache-control']).toBe('no-store')
    expect(createHash('sha256').update(await tile.body()).digest('hex')).toBe(evidence.tile_sha256)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const canvas = frame.locator('.vector-tile-preview .ol-viewport canvas').first()
    await expect(canvas).toBeVisible()
    await expect.poll(async () => canvas.evaluate(element => {
      const data = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
      let count = 0
      for (let offset = 3; offset < data.length; offset += 4) if (data[offset]) count++
      return count
    })).toBeGreaterThan(30)
    await expect(frame.locator('.vector-tile-preview .error-banner')).toHaveCount(0)
    const badge = frame.locator('.vector-tile-preview .render-status-badge')
    await expect(badge).toHaveClass(/is-dynamic/)
    await badge.hover()
    await expect(frame.getByRole('tooltip').filter({ hasText: `当前渲染轮次要素数：${evidence.feature_count}` })).toBeVisible()
    await expect.poll(() => responses.length).toBeGreaterThan(0)
    const caches = await frame.locator('body').evaluate(() => window.pmtilesRequestCaches)
    expect(caches.length).toBeGreaterThan(0)
    expect(caches.every(value => value === 'no-store')).toBe(true)
    for (const response of responses) {
      const parsed = new URL(response.url())
      expect(Number(parsed.pathname.match(/tiles\/(\d+)\//)[1])).toBeLessThanOrEqual(5)
      expect(response.status()).toBe(200)
      expect((await response.allHeaders())['cache-control']).toBe('no-store')
    }
    await canvas.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `pmtiles-${evidence.phase}-map.png`) })
    const execution = await json(await api.get(`/api/v1/monitor/executions/by-execution-id/${evidence.execution_id}`), 'PMTiles Monitor')
    expect(execution.status).toBe('success')
    expect(execution.module).toBe('develop')
    if (evidence.phase === 'replace') await verifyTileService(page, api, evidence)
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `pmtiles-${evidence.phase}-browser.json`), JSON.stringify({
      ...evidence, schema_version: 'addp.pmtiles-browser/v1', result: 'passed'
    }, null, 2))
  } finally {
    await api.dispose()
  }
})
