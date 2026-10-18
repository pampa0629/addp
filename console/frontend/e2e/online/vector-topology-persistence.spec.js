import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test.use({ locale: 'zh-CN' })

function canonical(points, ring = false) {
  const vertices = ring ? points.slice(0, -1) : points
  const sequences = [vertices, [...vertices].reverse()]
  return (ring ? sequences.flatMap(sequence => sequence.map((_, i) => [...sequence.slice(i), ...sequence.slice(0, i)])) : sequences)
    .map(sequence => JSON.stringify(sequence)).sort()[0]
}

test('同一普通User核对构面双输出、四面和四条诊断余线地图', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE || '{}')
  for (const key of ['run_id', 'principal_id', 'tenant_id', 'execution_id']) expect(evidence[key]).toBeTruthy()
  expect(evidence.outputs.map(output => output.port)).toEqual(['faces', 'diagnostics'])
  const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME,
    process.env.ADDP_ONLINE_TEST_USER_PASSWORD, `/develop/executions/${evidence.execution_id}`)
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'topology browser identity'))
    expect([auth.principalID, auth.tenantID, auth.principalType, auth.contextType])
      .toEqual([evidence.principal_id, evidence.tenant_id, 'user', 'tenant'])
    expect([...auth.permissions].sort()).toEqual(evidence.permissions_verified)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByText('成功', { exact: true })).toBeVisible()
    for (const output of evidence.outputs) await expect(frame.locator('.workflow-result')).toContainText(output.name)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-execution.png') })
    for (const output of evidence.outputs) {
      const preview = await json(await api.get(`/api/v1/manager/preview?locator=${encodeURIComponent(output.locator)}`), 'topology saved preview')
      const data = preview.data
      expect(preview.preview_type).toBe('table')
      expect([data.total, data.source_srid, data.source_crs, data.transform_status]).toEqual([4, 4326, 'EPSG:4326', 'not_transformed'])
      expect(data.geometry_columns).toEqual(['geometry'])
      expect([...data.columns].sort()).toEqual(output.port === 'faces' ? ['geometry'] : ['geometry', 'issue_type'])
      const actual = data.rows.map(row => {
        const geometry = row[data.geometry_column]
        if (output.port === 'faces') {
          expect(geometry.type).toBe('Polygon'); expect(geometry.coordinates).toHaveLength(1)
          return canonical(geometry.coordinates[0], true)
        }
        expect(geometry.type).toBe('LineString'); expect(row.issue_type).toBe('dangle')
        return canonical(geometry.coordinates)
      }).sort()
      const expected = output.port === 'faces'
        ? [120,122].flatMap(x => [30,32].map(y => canonical([[x,y],[x+2,y],[x+2,y+2],[x,y+2],[x,y]], true)))
        : [[[119,32],[120,32]], [[124,32],[125,32]], [[122,29],[122,30]], [[122,34],[122,35]]].map(points => canonical(points))
      expect(actual).toEqual(expected.sort())
      const locator = `${output.locator}&item_id=${output.item_id}`
      await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
      const table = frame.locator('.table-preview')
      await expect(table).toBeVisible()
      await expect(table.locator('.map-placeholder')).toHaveCount(0)
      const renderer = table.locator('.openlayers-map-renderer')
      await expect.poll(async () => renderer.evaluate(element => {
        const value = element.__vueParentComponent?.setupState?.mapInstance
        const map = value?.value || value
        if (!map || map.getView().getAnimating()) return []
        return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
          .map(feature => feature.getGeometry().getType()).sort()
      })).toEqual(Array(4).fill(output.port === 'faces' ? 'Polygon' : 'LineString'))
      await expect.poll(async () => renderer.locator('canvas').evaluateAll(canvases => canvases.reduce((count, canvas) => {
        const pixels = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
        for (let i = 0; i < pixels.length; i += 4) if (pixels[i+3] > 0 && pixels[i+1] > pixels[i] * 1.1 && pixels[i+1] > pixels[i+2] * 1.1) count++
        return count
      }, 0))).toBeGreaterThan(30)
      if (output.port === 'diagnostics') expect((await table.getByRole('cell').allTextContents()).map(value => value.trim())).toEqual(Array(4).fill('dangle'))
      await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `topology-${output.port}-map.png`) })
    }
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-browser.json'), JSON.stringify({
      ...evidence, schema_version: 'addp.topology-browser/v1', result: 'passed'
    }, null, 2))
  } finally { await api.dispose() }
})
