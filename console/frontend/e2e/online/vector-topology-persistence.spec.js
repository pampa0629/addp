import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { decodePreviewEWKB } from '../../../../manager/frontend/e2e/geometry-preview-support.js'
import { webMercatorPreviewCoordinate } from '../../../../manager/frontend/e2e/geometry-preview-coordinate.js'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

test.use({ locale: 'zh-CN' })

function canonical(points, ring = false) {
  const vertices = ring ? points.slice(0, -1) : points
  const sequences = [vertices, [...vertices].reverse()]
  return (ring ? sequences.flatMap(sequence => sequence.map((_, i) => [...sequence.slice(i), ...sequence.slice(0, i)])) : sequences)
    .map(sequence => JSON.stringify(sequence)).sort()[0]
}

const snapCoordinates = [[[.5,0],[10,0]],[[0,10],[10,10]],[[-.5,20],[10,20]],[[5,30],[5,35]],[[50,40],[52,40]]]

function polygonParts(coordinates) {
  return coordinates.map(part => part.map(ring => {
    expect(ring.at(-1)).toEqual(ring[0])
    return canonical(ring, true)
  }).sort()).map(value => JSON.stringify(value)).sort()
}

function gapRings(rings) {
  expect(rings.length).toBeGreaterThan(0)
  for (const ring of rings) expect(ring.at(-1)).toEqual(ring[0])
  return JSON.stringify([canonical(rings[0], true), rings.slice(1).map(ring => canonical(ring, true)).sort()])
}

test('同一普通User核对构面、修复、严格吸附、面叠面缝悬点线叠自叠线交自交地图及塌缩失败', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE || '{}')
  for (const key of ['run_id', 'principal_id', 'tenant_id', 'execution_id']) expect(evidence[key]).toBeTruthy()
  expect(evidence.outputs.map(output => output.port)).toEqual(['faces', 'diagnostics', 'repaired', 'snapped', 'overlaps', 'gaps', 'dangling', 'line_overlaps', 'line_self_overlaps', 'line_intersections', 'line_self_intersections'])
  expect(evidence.overlap_expected).toHaveLength(7)
  expect(evidence.gap_expected).toHaveLength(8)
  expect(evidence.dangling_expected).toHaveLength(17)
  expect(evidence.line_overlap_expected).toHaveLength(9)
  expect(evidence.line_self_overlap_expected).toHaveLength(8)
  expect(evidence.line_intersection_expected).toHaveLength(10)
  expect(evidence.line_self_intersection_expected).toHaveLength(15)
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
  const sourceLineage = { schema_version: 'addp.topology-source-lineage/v9', run_id: evidence.run_id, execution_id: evidence.execution_id, observations: [] }
  expect(evidence.sources).toHaveLength(12)
  for (const output of evidence.outputs) {
    const locator = `${output.locator}&item_id=${output.item_id}`
    const previewRead = page.waitForResponse(response => response.request().method() === 'GET' &&
      new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('locator') === locator &&
      (!output.layer_name || new URL(response.url()).searchParams.get('child_name') === output.layer_name))
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
      for (const index of output.port === 'line_self_intersections' ? [11] : output.port === 'line_intersections' ? [10] : output.port === 'line_self_overlaps' ? [9] : output.port === 'line_overlaps' ? [8] : output.port === 'dangling' ? [7] : output.port === 'gaps' ? [5,6] : output.port === 'overlaps' ? [4] : output.port === 'snapped' ? [2,3] : output.port === 'repaired' ? [1] : [0]) {
        const sourceID = evidence.sources[index].id
        expect(graph.edges.some(edge => edge.source?.item_id === sourceID && edge.target?.item_id === output.item_id &&
          edge.evidence?.execution_id === evidence.execution_id && edge.status === 'active' && edge.relation_kind === 'derive')).toBe(true)
      }
      if (['gaps','dangling','line_overlaps','line_self_overlaps','line_intersections','line_self_intersections'].includes(output.port)) {
        const currentSources = graph.edges.filter(edge => edge.target?.item_id === output.item_id &&
          edge.evidence?.execution_id === evidence.execution_id && edge.status === 'active' && edge.relation_kind === 'derive')
          .map(edge => edge.source?.item_id).sort((a,b) => a-b)
        expect(currentSources).toEqual((output.port === 'line_self_intersections' ? [evidence.sources[11].id] : output.port === 'line_intersections' ? [evidence.sources[10].id] : output.port === 'line_self_overlaps' ? [evidence.sources[9].id] : output.port === 'line_overlaps' ? [evidence.sources[8].id] : output.port === 'dangling' ? [evidence.sources[7].id] : [evidence.sources[5].id, evidence.sources[6].id]).sort((a,b) => a-b))
      }
      sourceLineage.observations.push({ output_item_id: output.item_id, sources, graph, preview })
      writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-source-lineage-raw.json'), JSON.stringify(sourceLineage, null, 2))
    } finally {
      await previewAPI.dispose()
    }
    const data = preview.data
    expect(preview.preview_type).toBe('table')
    if (output.layer_name) {
      expect([data.total, data.source_srid, data.source_crs, data.transform_status, data.preview_hint])
        .toEqual([output.port === 'line_self_intersections' ? 15 : output.port === 'line_intersections' ? 10 : output.port === 'line_self_overlaps' ? 8 : output.port === 'line_overlaps' ? 9 : output.port === 'dangling' ? 17 : output.port === 'gaps' ? 8 : output.port === 'overlaps' ? 7 : 5, 3857, 'EPSG:3857', 'not_transformed', 'frontend_transform_required'])
      expect(data.geometry_columns).toEqual([output.geometry_column])
      if (['line_overlaps','line_self_overlaps'].includes(output.port)) {
        const selfOverlap = output.port === 'line_self_overlaps'
        const identities = selfOverlap ? ['source_id'] : ['source_id_a','source_id_b']
        const lineExpected = selfOverlap ? evidence.line_self_overlap_expected : evidence.line_overlap_expected
        expect(output.fid_column).toBeTruthy()
        expect([...data.columns].sort()).toEqual([output.fid_column, output.geometry_column, ...identities, 'overlap_length'].sort())
        expect(data.rows.map(row => row[output.fid_column])).toEqual(Array.from({length:lineExpected.length},(_,index) => index+1))
        const actual = data.rows.map(row => {
          const geometry = decodePreviewEWKB(row[data.geometry_column])
          expect(geometry.type).toBe('MultiLineString')
          return { ...Object.fromEntries(identities.map(name => [name,row[name]])), overlap_length:row.overlap_length,
            parts:geometry.coordinates.map(part => canonical(part)).sort() }
        })
        expect(actual).toEqual(lineExpected.map(row => ({...row.properties,
          parts:row.geometry.coordinates.map(part => canonical(part)).sort()})))
      } else if (['dangling','line_intersections','line_self_intersections'].includes(output.port)) {
        const identities = output.port === 'line_intersections' ? ['source_id_a','source_id_b'] : ['source_id']
        const pointExpected = output.port === 'dangling' ? evidence.dangling_expected : output.port === 'line_self_intersections' ? evidence.line_self_intersection_expected : evidence.line_intersection_expected
        expect(output.fid_column).toBeTruthy()
        expect([...data.columns].sort()).toEqual([output.fid_column, output.geometry_column, ...identities].sort())
        expect(data.rows.map(row => row[output.fid_column])).toEqual(Array.from({length:pointExpected.length},(_,index) => index+1))
        const actual = data.rows.map(row => ({ properties:Object.fromEntries(identities.map(name => [name,row[name]])), geometry:decodePreviewEWKB(row[data.geometry_column]) }))
        for (const feature of actual) expect(feature.geometry.type).toBe('Point')
        expect(actual).toEqual(pointExpected)
      } else if (output.port === 'gaps') {
        expect(output.fid_column).toBeTruthy()
        expect([...data.columns].sort()).toEqual([output.fid_column, output.geometry_column, 'gap_area'].sort())
        expect(data.rows.map(row => row[output.fid_column])).toEqual([1,2,3,4,5,6,7,8])
        const actual = data.rows.map(row => {
          const geometry = decodePreviewEWKB(row[data.geometry_column])
          expect(geometry.type).toBe('Polygon')
          return { gap_area: row.gap_area, rings: gapRings(geometry.coordinates) }
        })
        expect(actual).toEqual(evidence.gap_expected.map(row => ({ ...row.properties, rings: gapRings(row.geometry.coordinates) })))
      } else if (output.port === 'overlaps') {
        expect(output.fid_column).toBeTruthy()
        expect([...data.columns].sort()).toEqual([output.fid_column, output.geometry_column, 'source_id_a', 'source_id_b', 'overlap_area'].sort())
        expect(data.rows.map(row => row[output.fid_column])).toEqual([1,2,3,4,5,6,7])
        const actual = data.rows.map(row => {
          const geometry = decodePreviewEWKB(row[data.geometry_column])
          expect(geometry.type).toBe('MultiPolygon')
          return { source_id_a: row.source_id_a, source_id_b: row.source_id_b,
            overlap_area: row.overlap_area, parts: polygonParts(geometry.coordinates) }
        })
        expect(actual).toEqual(evidence.overlap_expected.map(row => ({ ...row.properties, parts: polygonParts(row.geometry.coordinates) })))
      } else {
        expect(data.rows.map(row => ({ name: row.source_name, grade: row.grade, geometry: decodePreviewEWKB(row[data.geometry_column]) })))
          .toEqual(snapCoordinates.map((coordinates,index) => ({ name:`snap-${index}`, grade:10+index, geometry:{type:'LineString',coordinates} })))
      }
    } else {
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
    }
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
    })).toEqual(Array(output.port === 'line_self_intersections' ? 15 : output.port === 'line_intersections' ? 10 : output.port === 'line_self_overlaps' ? 8 : output.port === 'line_overlaps' ? 9 : output.port === 'dangling' ? 17 : output.port === 'gaps' ? 8 : output.port === 'overlaps' ? 7 : output.port === 'snapped' ? 5 : output.port === 'repaired' ? 2 : 4).fill(['line_overlaps','line_self_overlaps'].includes(output.port) ? 'MultiLineString' : ['dangling','line_intersections','line_self_intersections'].includes(output.port) ? 'Point' : ['faces','gaps'].includes(output.port) ? 'Polygon' : ['repaired','overlaps'].includes(output.port) ? 'MultiPolygon' : 'LineString'))
    if (['dangling','line_intersections','line_self_intersections'].includes(output.port)) {
      const identities = output.port === 'line_intersections' ? ['source_id_a','source_id_b'] : ['source_id']
      const pointExpected = output.port === 'dangling' ? evidence.dangling_expected : output.port === 'line_self_intersections' ? evidence.line_self_intersection_expected : evidence.line_intersection_expected
      for (const name of identities) await expect(table.locator('thead')).toContainText(name)
      // Sort both complete identity records in Node; browser locale must not
      // determine matching order or erase duplicate coordinates from two sources.
      const byIdentity = (a,b) => {
        const left = JSON.stringify(a), right = JSON.stringify(b)
        return left < right ? -1 : left > right ? 1 : 0
      }
      const expected = pointExpected.map(row => ({...row.properties, coordinates:webMercatorPreviewCoordinate(row.geometry.coordinates)}))
        .sort(byIdentity)
      await expect.poll(async () => {
        const actual = await renderer.evaluate((element, identities) => {
          const value = element.__vueParentComponent?.setupState?.mapInstance
          const map = value?.value || value
          if (!map || map.getView().getAnimating() || map.getView().getProjection().getCode() !== 'EPSG:3857') return []
          return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
            .map(feature => ({...Object.fromEntries(identities.map(name => [name,feature.get(name)])), coordinates:feature.getGeometry().getCoordinates()}))
        }, identities)
        return actual.sort(byIdentity)
      }).toEqual(expected)
    }
    if (output.port === 'gaps') {
      await expect(table.locator('thead')).toContainText('gap_area')
      const expected = evidence.gap_expected.map(row => gapRings(row.geometry.coordinates.map(ring => ring.map(webMercatorPreviewCoordinate)))).sort()
      await expect.poll(async () => renderer.evaluate(element => {
        const value = element.__vueParentComponent?.setupState?.mapInstance
        const map = value?.value || value
        if (!map || map.getView().getAnimating() || map.getView().getProjection().getCode() !== 'EPSG:3857') return []
        return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
          .map(feature => feature.getGeometry().getCoordinates())
      }).then(geometries => geometries.map(gapRings).sort())).toEqual(expected)
    }
    if (output.port === 'overlaps') {
      await expect(table.locator('thead')).toContainText('source_id_a')
      await expect(table.locator('thead')).toContainText('source_id_b')
      const expected = evidence.overlap_expected.map(row => polygonParts(row.geometry.coordinates.map(part => part.map(ring => ring.map(webMercatorPreviewCoordinate))))).map(value => JSON.stringify(value)).sort()
      await expect.poll(async () => renderer.evaluate(element => {
        const value = element.__vueParentComponent?.setupState?.mapInstance
        const map = value?.value || value
        if (!map || map.getView().getAnimating() || map.getView().getProjection().getCode() !== 'EPSG:3857') return []
        return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
          .map(feature => feature.getGeometry().getCoordinates())
      }).then(geometries => geometries.map(polygonParts).map(value => JSON.stringify(value)).sort())).toEqual(expected)
    }
    if (['line_overlaps','line_self_overlaps'].includes(output.port)) {
      const selfOverlap = output.port === 'line_self_overlaps'
      const identities = selfOverlap ? ['source_id'] : ['source_id_a','source_id_b']
      const lineExpected = selfOverlap ? evidence.line_self_overlap_expected : evidence.line_overlap_expected
      for (const name of [...identities,'overlap_length']) await expect(table.locator('thead')).toContainText(name)
      const record = row => JSON.stringify({...Object.fromEntries(identities.map(name => [name,row[name]])),
        overlap_length:row.overlap_length,parts:row.coordinates.map(part => canonical(part)).sort()})
      const expected = lineExpected.map(row => record({...row.properties,
        coordinates:row.geometry.coordinates.map(part => part.map(webMercatorPreviewCoordinate))})).sort()
      await expect.poll(async () => renderer.evaluate((element, identities) => {
        const value = element.__vueParentComponent?.setupState?.mapInstance
        const map = value?.value || value
        if (!map || map.getView().getAnimating() || map.getView().getProjection().getCode() !== 'EPSG:3857') return []
        return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
          .map(feature => ({...Object.fromEntries(identities.map(name => [name,feature.get(name)])),
            overlap_length:feature.get('overlap_length'),coordinates:feature.getGeometry().getCoordinates()}))
      }, identities).then(rows => rows.map(record).sort())).toEqual(expected)
    }
    if (output.port === 'snapped') {
      for (let index = 0; index < snapCoordinates.length; index++) {
        const row = table.getByRole('row').filter({ hasText:`snap-${index}` })
        await expect(row.getByRole('cell', { name:String(10+index), exact:true })).toBeVisible()
      }
      await expect.poll(async () => renderer.evaluate(element => {
        const value = element.__vueParentComponent?.setupState?.mapInstance
        const map = value?.value || value
        if (!map || map.getView().getAnimating() || map.getView().getProjection().getCode() !== 'EPSG:3857') return []
        return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
          .map(feature => feature.getGeometry().getCoordinates())
          .sort((a,b) => JSON.stringify(a).localeCompare(JSON.stringify(b)))
      })).toEqual(snapCoordinates.map(line => line.map(webMercatorPreviewCoordinate))
        .sort((a,b) => JSON.stringify(a).localeCompare(JSON.stringify(b))))
    }
    let featurePixels = 0
    const pointMap = ['dangling','line_intersections','line_self_intersections'].includes(output.port)
    await expect.poll(async () => featurePixels = await renderer.locator('canvas').evaluateAll((canvases, pointMap) => canvases.reduce((count, canvas) => {
      const pixels = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
      for (let i = 0; i < pixels.length; i += 4) {
        const foreground = pointMap
          ? pixels[i+2] > pixels[i+1] * 1.1 && pixels[i+1] > pixels[i] * 1.1
          : pixels[i+1] > pixels[i] * 1.1 && pixels[i+1] > pixels[i+2] * 1.1
        if (pixels[i+3] > 0 && foreground) count++
      }
      return count
    }, 0), pointMap)).toBeGreaterThan(30)
    const rendered = await renderer.evaluate(element => {
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      return { projection: map.getView().getProjection().getCode(), features: map.getLayers().getArray()
        .flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
        .map(feature => ({ type: feature.getGeometry().getType(), coordinates: feature.getGeometry().getCoordinates(),
          ...(feature.getGeometry().getType() === 'MultiLineString' ? {source_id:feature.get('source_id'),source_id_a:feature.get('source_id_a'),source_id_b:feature.get('source_id_b'),overlap_length:feature.get('overlap_length')} : {}),
          ...(feature.getGeometry().getType() === 'Point' ? { source_id:feature.get('source_id'), source_id_a:feature.get('source_id_a'), source_id_b:feature.get('source_id_b') } : {}) })) }
    })
    sourceLineage.observations.at(-1).rendered = { ...rendered, ...(pointMap ? { point_pixels: featurePixels } : { green_pixels: featurePixels }) }
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-source-lineage-raw.json'), JSON.stringify(sourceLineage, null, 2))
    if (output.port === 'diagnostics') expect((await table.getByRole('cell').allTextContents()).map(value => value.trim())).toEqual(Array(4).fill('dangle'))
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `topology-${output.port}-map.png`) })
  }
  const failedRead = page.waitForResponse(response => response.request().method() === 'GET' &&
    new URL(response.url()).pathname === `/api/v1/develop/executions/${evidence.failed_execution_id}`)
  await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${evidence.failed_execution_id}`)
  const failedResponse = await failedRead
  const failedAPI = await browserAPIFromResponse(failedResponse, process.env.GATEWAY_URL, expectedIdentity)
  try {
    const failed = await json(failedResponse, 'collapsed snap execution')
    expect(failed.status).toBe('failed')
    expect(failed.outputs || {}).toEqual({})
    for (const source of evidence.sources) {
      expect(await json(await failedAPI.get(`/api/v1/meta/items/${source.id}`), 'unchanged source after failed snap')).toEqual(source.before)
    }
  } finally { await failedAPI.dispose() }
  await expect(frame.getByText('失败', { exact:true })).toBeVisible()
  await page.screenshot({ path:resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-snap-failed.png') })
  writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-browser.json'), JSON.stringify({
    ...evidence, schema_version: 'addp.topology-browser/v10', result: 'passed'
  }, null, 2))
})
