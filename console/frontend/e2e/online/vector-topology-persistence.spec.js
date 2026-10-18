import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

test.use({ locale: 'zh-CN' })

function canonical(points, ring = false) {
  const vertices = ring ? points.slice(0, -1) : points
  const sequences = [vertices, [...vertices].reverse()]
  return (ring ? sequences.flatMap(sequence => sequence.map((_, i) => [...sequence.slice(i), ...sequence.slice(0, i)])) : sequences)
    .map(sequence => JSON.stringify(sequence)).sort()[0]
}

test('同一普通User核对构面双输出、四面、四条诊断余线与真实自交面修复地图', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE || '{}')
  for (const key of ['run_id', 'principal_id', 'tenant_id', 'execution_id']) expect(evidence[key]).toBeTruthy()
  expect(evidence.outputs.map(output => output.port)).toEqual(['faces', 'diagnostics', 'repaired'])
  const expectedIdentity = { principalID: evidence.principal_id, tenantID: evidence.tenant_id,
    permissions: evidence.permissions_verified }
  const executionRead = page.waitForResponse(response => response.request().method() === 'GET' &&
    new URL(response.url()).pathname === `/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME,
    process.env.ADDP_ONLINE_TEST_USER_PASSWORD, `/develop/executions/${evidence.execution_id}`)
  const executionResponse = await executionRead
  const executionAPI = await browserAPIFromResponse(executionResponse, process.env.GATEWAY_URL, expectedIdentity)
  await executionAPI.dispose()
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功', { exact: true })).toBeVisible()
  for (const output of evidence.outputs) await expect(frame.locator('.workflow-result')).toContainText(output.name)
  await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-execution.png') })
  const sourceLineage = { schema_version: 'addp.topology-source-lineage/v1', run_id: evidence.run_id, execution_id: evidence.execution_id, observations: [] }
  expect(evidence.sources).toHaveLength(2)
  for (const output of evidence.outputs) {
    const locator = `${output.locator}&item_id=${output.item_id}`
    const previewRead = page.waitForResponse(response => response.request().method() === 'GET' &&
      new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('locator') === locator)
    await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
    const previewResponse = await previewRead
    const preview = await json(previewResponse, 'topology saved preview')
    const previewAPI = await browserAPIFromResponse(previewResponse, process.env.GATEWAY_URL, expectedIdentity)
    try {
      const sources = []
      for (const source of evidence.sources) {
        const current = await json(await previewAPI.get(`/api/v1/meta/items/${source.id}`), 'unchanged source Meta')
        expect(current).toEqual(source.before)
        sources.push(current)
      }
      const graph = await json(await previewAPI.get(`/api/v1/meta/lineage/graph?subject_kind=data_item&item_id=${output.item_id}&direction=upstream`), 'current output lineage')
      expect(graph.truncated).not.toBe(true)
      const sourceID = evidence.sources[output.port === 'repaired' ? 1 : 0].id
      expect(graph.edges.some(edge => edge.source?.item_id === sourceID && edge.target?.item_id === output.item_id &&
        edge.evidence?.execution_id === evidence.execution_id && edge.status === 'active' && edge.relation_kind === 'derive')).toBe(true)
      sourceLineage.observations.push({ output_item_id: output.item_id, sources, graph })
      writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-source-lineage-raw.json'), JSON.stringify(sourceLineage, null, 2))
    } finally {
      await previewAPI.dispose()
    }
    const data = preview.data
    expect(preview.preview_type).toBe('table')
    expect([data.total, data.source_crs, data.transform_status, data.preview_hint]).toEqual([output.port === 'repaired' ? 2 : 4, 'OGC:CRS84', 'not_transformed', 'direct_renderable'])
    expect(data.source_srid === undefined || data.source_srid === 0).toBe(true)
    expect(data.geometry_columns).toEqual(['geometry'])
    expect([...data.columns].sort()).toEqual(output.port === 'faces' ? ['geometry'] : output.port === 'diagnostics' ? ['geometry', 'issue_type'] : ['after_reason','after_valid','geometry','geometry_reason','geometry_valid','grade','source_name'])
    const actual = data.rows.map(row => {
      const geometry = row[data.geometry_column]
      if (output.port === 'faces') {
        expect(geometry.type).toBe('Polygon'); expect(geometry.coordinates).toHaveLength(1)
        return canonical(geometry.coordinates[0], true)
      }
      if (output.port === 'repaired') {
        expect(geometry.type).toBe('MultiPolygon'); expect(geometry.coordinates).toHaveLength(2)
        expect(row.geometry_valid).toBe(false); expect(row.geometry_reason).toMatch(/^Self-intersection/)
        expect(row.after_valid).toBe(true); expect(row.after_reason).toBe('Valid Geometry')
        return JSON.stringify({ name: row.source_name, grade: row.grade,
          rings: geometry.coordinates.map(part => { expect(part).toHaveLength(1); return canonical(part[0], true) }).sort() })
      }
      expect(geometry.type).toBe('LineString'); expect(row.issue_type).toBe('dangle')
      return canonical(geometry.coordinates)
    }).sort()
    const expected = output.port === 'faces'
      ? [120,122].flatMap(x => [30,32].map(y => canonical([[x,y],[x+2,y],[x+2,y+2],[x,y+2],[x,y]], true)))
      : output.port === 'repaired' ? [120,124].map((x,i) => JSON.stringify({ name: `bow-${i}`, grade: 7+i, rings: [canonical([[x,30],[x+2,30],[x+1,31],[x,30]], true), canonical([[x,32],[x+2,32],[x+1,31],[x,32]], true)].sort() }))
      : [[[119,32],[120,32]], [[124,32],[125,32]], [[122,29],[122,30]], [[122,34],[122,35]]].map(points => canonical(points))
    expect(actual).toEqual(expected.sort())
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
    })).toEqual(Array(output.port === 'repaired' ? 2 : 4).fill(output.port === 'faces' ? 'Polygon' : output.port === 'repaired' ? 'MultiPolygon' : 'LineString'))
    await expect.poll(async () => renderer.locator('canvas').evaluateAll(canvases => canvases.reduce((count, canvas) => {
      const pixels = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
      for (let i = 0; i < pixels.length; i += 4) if (pixels[i+3] > 0 && pixels[i+1] > pixels[i] * 1.1 && pixels[i+1] > pixels[i+2] * 1.1) count++
      return count
    }, 0))).toBeGreaterThan(30)
    if (output.port === 'diagnostics') expect((await table.getByRole('cell').allTextContents()).map(value => value.trim())).toEqual(Array(4).fill('dangle'))
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `topology-${output.port}-map.png`) })
  }
  writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-browser.json'), JSON.stringify({
    ...evidence, schema_version: 'addp.topology-browser/v2', result: 'passed'
  }, null, 2))
})
