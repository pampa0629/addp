import { expect, request, test } from '@playwright/test'
import { observeLineageCanvas, lineageCanvasSnapshot } from '../../../../common-frontend/basic/tests/fixtures/lineageCanvas.js'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { decodePreviewEWKB } from '../../../../manager/frontend/e2e/geometry-preview-support.js'
import { webMercatorPreviewCoordinate, webMercatorPreviewGeometry } from '../../../../manager/frontend/e2e/geometry-preview-coordinate.js'
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

const expectedOutputGeometry = {
  faces: ['Polygon', 4], diagnostics: ['LineString', 4], repaired: ['MultiPolygon', 2],
  snapped: ['LineString', 5], overlaps: ['MultiPolygon', 7], gaps: ['Polygon', 8],
  dangling: ['Point', 17], line_overlaps: ['MultiLineString', 9], line_self_overlaps: ['MultiLineString', 8],
  line_intersections: ['Point', 10], line_self_intersections: ['Point', 15],
  split_lines: ['LineString', 43], line_components: ['LineString', 15],
  line_chains: ['LineString', 12], line_chain_members: ['LineString', 18],
}

function expectedGeometryTypes(port) {
  if (!Object.hasOwn(expectedOutputGeometry, port)) throw new Error(`Unregistered topology output port: ${port}`)
  const [type, count] = expectedOutputGeometry[port]
  return Array(count).fill(type)
}

function expectedOriginalLines(evidence, port) {
  return {split_lines:evidence.split_line_expected, line_components:evidence.line_component_expected,
    line_chains:evidence.line_chain_expected, line_chain_members:evidence.line_chain_members_expected}[port]
}

test('同一普通User核对构面、修复、严格吸附、面叠面缝悬点线叠自叠线交自交及打断连通分组链路合并地图及塌缩失败', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE || '{}')
  for (const key of ['run_id', 'principal_id', 'tenant_id', 'execution_id']) expect(evidence[key]).toBeTruthy()
  expect(evidence.outputs.map(output => output.port)).toEqual(['faces', 'diagnostics', 'repaired', 'snapped', 'overlaps', 'gaps', 'dangling', 'line_overlaps', 'line_self_overlaps', 'line_intersections', 'line_self_intersections', 'split_lines', 'line_components', 'line_chains', 'line_chain_members'])
  expect(evidence.overlap_expected).toHaveLength(7)
  expect(evidence.gap_expected).toHaveLength(8)
  expect(evidence.dangling_expected).toHaveLength(17)
  expect(evidence.line_overlap_expected).toHaveLength(9)
  expect(evidence.line_self_overlap_expected).toHaveLength(8)
  expect(evidence.line_intersection_expected).toHaveLength(10)
  expect(evidence.line_self_intersection_expected).toHaveLength(15)
  expect(evidence.split_line_scope).toBe('all')
  expect(evidence.split_line_expected).toHaveLength(43)
  expect(evidence.line_component_expected).toHaveLength(15)
  expect(evidence.line_chain_expected).toHaveLength(12)
  expect(evidence.line_chain_members_expected).toHaveLength(18)
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
  const sourceLineage = { schema_version: 'addp.topology-source-lineage/v13', split_line_scope: evidence.split_line_scope, run_id: evidence.run_id, execution_id: evidence.execution_id, observations: [] }
  expect(evidence.sources).toHaveLength(15)
  for (const output of evidence.outputs) {
    const expectedTypes = expectedGeometryTypes(output.port)
    const locator = `${output.locator}&item_id=${output.item_id}`
    const previewRead = page.waitForResponse(response => response.request().method() === 'GET' &&
      new URL(response.url()).pathname === '/api/v1/manager/preview' &&
      new URL(response.url()).searchParams.get('locator') === locator &&
      (!output.layer_name || new URL(response.url()).searchParams.get('child_name') === output.layer_name))
    await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
    let previewResponse = await previewRead
    if (output.port === 'split_lines') {
      // Use the real shared pagination control so every new internal fragment is rendered.
      const fullPageRead = page.waitForResponse(response => response.request().method() === 'GET' &&
        new URL(response.url()).pathname === '/api/v1/manager/preview' &&
        new URL(response.url()).searchParams.get('locator') === locator &&
        new URL(response.url()).searchParams.get('child_name') === output.layer_name &&
        new URL(response.url()).searchParams.get('page') === '1' &&
        new URL(response.url()).searchParams.get('page_size') === '50')
      await frame.locator('.table-preview .el-pagination__sizes .el-select').click()
      await frame.getByRole('option', { name: /^50(?:\D|$)/ }).click()
      previewResponse = await fullPageRead
    }
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
      for (const index of ['line_chains','line_chain_members'].includes(output.port) ? [14] : output.port === 'line_components' ? [13] : output.port === 'split_lines' ? [12] : output.port === 'line_self_intersections' ? [11] : output.port === 'line_intersections' ? [10] : output.port === 'line_self_overlaps' ? [9] : output.port === 'line_overlaps' ? [8] : output.port === 'dangling' ? [7] : output.port === 'gaps' ? [5,6] : output.port === 'overlaps' ? [4] : output.port === 'snapped' ? [2,3] : output.port === 'repaired' ? [1] : [0]) {
        const sourceID = evidence.sources[index].id
        expect(graph.edges.some(edge => edge.source?.item_id === sourceID && edge.target?.item_id === output.item_id &&
          edge.evidence?.execution_id === evidence.execution_id && edge.status === 'active' && edge.relation_kind === 'derive')).toBe(true)
      }
      if (['gaps','dangling','line_overlaps','line_self_overlaps','line_intersections','line_self_intersections','split_lines','line_components','line_chains','line_chain_members'].includes(output.port)) {
        const currentSources = graph.edges.filter(edge => edge.target?.item_id === output.item_id &&
          edge.evidence?.execution_id === evidence.execution_id && edge.status === 'active' && edge.relation_kind === 'derive')
          .map(edge => edge.source?.item_id).sort((a,b) => a-b)
        expect(currentSources).toEqual((['line_chains','line_chain_members'].includes(output.port) ? [evidence.sources[14].id] : output.port === 'line_components' ? [evidence.sources[13].id] : output.port === 'split_lines' ? [evidence.sources[12].id] : output.port === 'line_self_intersections' ? [evidence.sources[11].id] : output.port === 'line_intersections' ? [evidence.sources[10].id] : output.port === 'line_self_overlaps' ? [evidence.sources[9].id] : output.port === 'line_overlaps' ? [evidence.sources[8].id] : output.port === 'dangling' ? [evidence.sources[7].id] : [evidence.sources[5].id, evidence.sources[6].id]).sort((a,b) => a-b))
      }
      sourceLineage.observations.push({ output_item_id: output.item_id, sources, graph, preview })
      writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'topology-source-lineage-raw.json'), JSON.stringify(sourceLineage, null, 2))
    } finally {
      await previewAPI.dispose()
    }
    const data = preview.data
    if (output.port === 'split_lines') expect([data.page, data.page_size, data.rows.length]).toEqual([1,50,43])
    expect(preview.preview_type).toBe('table')
    if (output.layer_name) {
      expect([data.total, data.source_srid, data.source_crs, data.transform_status, data.preview_hint])
        .toEqual([expectedTypes.length, 3857, 'EPSG:3857', 'not_transformed', 'frontend_transform_required'])
      expect(data.geometry_columns).toEqual([output.geometry_column])
      if (['split_lines','line_components','line_chains','line_chain_members'].includes(output.port)) {
        const partExpected = expectedOriginalLines(evidence, output.port)
        const splitFields = Object.keys(partExpected[0].properties)
        expect(output.fid_column).toBeTruthy()
        expect([...data.columns].sort()).toEqual([output.fid_column, output.geometry_column, ...splitFields].sort())
        expect(data.rows.map(row => row[output.fid_column])).toEqual(Array.from({ length:partExpected.length }, (_,index) => index+1))
        const actual = data.rows.map(row => ({ properties:Object.fromEntries(splitFields.map(name => [name,row[name]])),
          geometry:decodePreviewEWKB(row[data.geometry_column]) }))
        for (const row of actual) expect(row.geometry.type).toBe('LineString')
        expect(actual).toEqual(partExpected)
      } else if (['line_overlaps','line_self_overlaps'].includes(output.port)) {
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
    })).toEqual(expectedTypes)
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
    if (['split_lines','line_components','line_chains','line_chain_members'].includes(output.port)) {
      const partExpected = expectedOriginalLines(evidence, output.port)
      const splitFields = Object.keys(partExpected[0].properties)
      for (const name of splitFields) await expect(table.locator('thead')).toContainText(name)
      const expected = partExpected.map(row => JSON.stringify({ properties:row.properties,
        coordinates:row.geometry.coordinates.map(webMercatorPreviewCoordinate) })).sort()
      await expect.poll(async () => renderer.evaluate((element, fields) => {
        const value = element.__vueParentComponent?.setupState?.mapInstance
        const map = value?.value || value
        if (!map || map.getView().getAnimating() || map.getView().getProjection().getCode() !== 'EPSG:3857') return []
        return map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
          .map(feature => ({ properties:Object.fromEntries(fields.map(name => [name,feature.get(name)])),
            coordinates:feature.getGeometry().getCoordinates() }))
      }, splitFields).then(rows => rows.map(row => JSON.stringify(row)).sort())).toEqual(expected)
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
    const rendered = await renderer.evaluate((element, {port, fields}) => {
      const value = element.__vueParentComponent.setupState.mapInstance
      const map = value?.value || value
      return { projection: map.getView().getProjection().getCode(), features: map.getLayers().getArray()
        .flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
        .map(feature => ({ type: feature.getGeometry().getType(), coordinates: feature.getGeometry().getCoordinates(),
          ...(feature.getGeometry().getType() === 'MultiLineString' ? {source_id:feature.get('source_id'),source_id_a:feature.get('source_id_a'),source_id_b:feature.get('source_id_b'),overlap_length:feature.get('overlap_length')} : {}),
          ...(['split_lines','line_components','line_chains','line_chain_members'].includes(port) ? { properties:Object.fromEntries(fields.map(name => [name,feature.get(name)])) } : {}),
          ...(feature.getGeometry().getType() === 'Point' ? { source_id:feature.get('source_id'), source_id_a:feature.get('source_id_a'), source_id_b:feature.get('source_id_b') } : {}) })) }
    }, {port:output.port, fields:Object.keys(expectedOriginalLines(evidence, output.port)?.[0]?.properties || {})})
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
    ...evidence, schema_version: 'addp.topology-browser/v14', result: 'passed'
  }, null, 2))
})

function normalizedMapExpectation(features) {
  const project=coordinates=>typeof coordinates[0]==='number' ? webMercatorPreviewCoordinate(coordinates) : coordinates.map(project)
  return features.map(row => JSON.stringify({ properties:row.properties,coordinates:project(row.geometry.coordinates) })).sort()
}

async function verifyNetworkCase(page,caseName,counts,schema) {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE || '{}')
  expect(evidence.case).toBe(caseName)
  expect(evidence.outputs.map(o=>o.port)).toEqual(Object.keys(counts))
  expect(Object.keys(evidence.expected)).toEqual(Object.keys(counts))
  for(const [port,count] of Object.entries(counts))expect(evidence.expected[port]).toHaveLength(count)
  expect(evidence.source.before.id).toBe(evidence.source.id)
  const expectedIdentity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  const executionRead=page.waitForResponse(r=>r.request().method()==='GET' && new URL(r.url()).pathname===`/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,`/develop/executions/${evidence.execution_id}`)
  const response=await executionRead
  const executionAPI=await browserAPIFromResponse(response,process.env.GATEWAY_URL,expectedIdentity)
  try {
    const execution=await json(response,'normalized execution')
    expect(execution.status).toBe('success')
    expect(Object.keys(execution.outputs).sort()).toEqual(Object.keys(counts).map(p=>'save_'+p).sort())
  } finally {await executionAPI.dispose()}
  const frame=page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功',{exact:true})).toBeVisible()
  for(const output of evidence.outputs)await expect(frame.locator('.workflow-result')).toContainText(output.name)
  await page.screenshot({path:resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,`${caseName}-execution.png`)})
  const raw={schema_version:schema+'-lineage/v1',case:evidence.case,run_id:evidence.run_id,execution_id:evidence.execution_id,observations:[]}
  for(const output of evidence.outputs){
    const expected=evidence.expected[output.port], fields=Object.keys(expected[0].properties)
    const locator=`${output.locator}&item_id=${output.item_id}`
    const previewRead=page.waitForResponse(r=>r.request().method()==='GET' &&
      new URL(r.url()).pathname==='/api/v1/manager/preview' && new URL(r.url()).searchParams.get('locator')===locator &&
      new URL(r.url()).searchParams.get('child_name')===output.layer_name)
    await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
    let previewResponse=await previewRead
    if(expected.length>20){
      const fullPageRead=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname==='/api/v1/manager/preview'&&
        new URL(r.url()).searchParams.get('locator')===locator&&new URL(r.url()).searchParams.get('child_name')===output.layer_name&&
        new URL(r.url()).searchParams.get('page')==='1'&&new URL(r.url()).searchParams.get('page_size')==='50')
      await frame.locator('.table-preview .el-pagination__sizes .el-select').click()
      await frame.getByRole('option',{name:/^50(?:\D|$)/}).click()
      previewResponse=await fullPageRead
    }
    const preview=await json(previewResponse,'normalized preview')
    const api=await browserAPIFromResponse(previewResponse,process.env.GATEWAY_URL,expectedIdentity)
    let graph,source,conflictSource
    try {
      source=await json(await api.get(`/api/v1/meta/items/${evidence.source.id}`),'unchanged normalization source')
      expect(source).toEqual(evidence.source.before)
      if(evidence.conflict_source){
        conflictSource=await json(await api.get(`/api/v1/meta/items/${evidence.conflict_source.id}`),'unchanged buffer conflict source')
        expect(conflictSource).toEqual(evidence.conflict_source.before)
      }
      graph=await json(await api.get(`/api/v1/meta/lineage/graph?subject_kind=data_item&item_id=${output.item_id}&direction=upstream`),'normalization lineage')
      expect(graph.truncated).not.toBe(true)
      const edges=graph.edges.filter(edge=>edge.target?.item_id===output.item_id && edge.evidence?.execution_id===evidence.execution_id && edge.status==='active' && edge.relation_kind==='derive')
      expect(edges).toHaveLength(1)
      expect(edges[0].source.item_id).toBe(evidence.source.id)
    } finally {await api.dispose()}
    const data=preview.data
    expect(preview.preview_type).toBe('table')
    expect([data.total,data.source_srid,data.source_crs,data.transform_status,data.preview_hint])
      .toEqual([expected.length,3857,'EPSG:3857','not_transformed','frontend_transform_required'])
    expect(data.geometry_columns).toEqual([output.geometry_column])
    expect(output.fid_column).toBeTruthy()
    expect([...data.columns].sort()).toEqual([output.fid_column,output.geometry_column,...fields].sort())
    expect(data.rows.map(row=>row[output.fid_column])).toEqual(Array.from({length:expected.length},(_,i)=>i+1))
    expect(data.rows.map(row=>({properties:Object.fromEntries(fields.map(k=>[k,row[k]])),geometry:decodePreviewEWKB(row[output.geometry_column])}))).toEqual(expected)
    const table=frame.locator('.table-preview'),renderer=table.locator('.openlayers-map-renderer')
    await expect(table).toBeVisible()
    await expect(table.locator('.map-placeholder')).toHaveCount(0)
    let rendered
    await expect.poll(async()=>{
      rendered=await renderer.evaluate((element,fields)=>{
        const value=element.__vueParentComponent?.setupState?.mapInstance,map=value?.value||value
        if(!map||map.getView().getAnimating())return null
        return {projection:map.getView().getProjection().getCode(),features:map.getLayers().getArray().flatMap(layer=>layer.getSource?.()?.getFeatures?.()||[])
          .map(feature=>({type:feature.getGeometry().getType(),properties:Object.fromEntries(fields.map(k=>[k,feature.get(k)])),coordinates:feature.getGeometry().getCoordinates()}))}
      },fields)
      if(!rendered||rendered.projection!=='EPSG:3857'||rendered.features.some(f=>f.type!==expected[0].geometry.type))return []
      return rendered.features.map(({properties,coordinates})=>JSON.stringify({properties,coordinates})).sort()
    }).toEqual(normalizedMapExpectation(expected))
    let greenPixels=0
    await expect.poll(async()=>greenPixels=await renderer.locator('canvas').evaluateAll(canvases=>canvases.reduce((n,c)=>{
      const pixels=c.getContext('2d').getImageData(0,0,c.width,c.height).data
      for(let i=0;i<pixels.length;i+=4)if(pixels[i+3]>0&&pixels[i+1]>pixels[i]*1.1&&pixels[i+1]>pixels[i+2]*1.1)n++
      return n
    },0))).toBeGreaterThan(30)
    raw.observations.push({output_item_id:output.item_id,source,...(evidence.conflict_source?{conflict_source:conflictSource}:{}),graph,preview,rendered:{...rendered,green_pixels:greenPixels}})
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,'topology-source-lineage-raw.json'),JSON.stringify(raw,null,2))
    await page.screenshot({path:resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,`${caseName}-${output.port}-map.png`)})
  }
  const failedRead=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${evidence.failed_execution_id}`)
  await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${evidence.failed_execution_id}`)
  const failedResponse=await failedRead,api=await browserAPIFromResponse(failedResponse,process.env.GATEWAY_URL,expectedIdentity)
  try {
    const failed=await json(failedResponse,'failed normalization execution')
    expect(failed.status).toBe('failed');expect(failed.outputs||{}).toEqual({})
    expect(await json(await api.get(`/api/v1/meta/items/${evidence.source.id}`),'unchanged source')).toEqual(evidence.source.before)
    if(evidence.conflict_source)expect(await json(await api.get(`/api/v1/meta/items/${evidence.conflict_source.id}`),'unchanged conflict source')).toEqual(evidence.conflict_source.before)
  } finally {await api.dispose()}
  await expect(frame.getByText('失败',{exact:true})).toBeVisible()
  await page.screenshot({path:resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,`${caseName}-failed.png`)})
  writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,'topology-browser.json'),JSON.stringify({...evidence,schema_version:schema+'-browser/v1',result:'passed'},null,2))
}

test('重合规整独立普通User双成果与字段拒绝', async ({page}) => {
  await verifyNetworkCase(page,'line-network-normalize',{result:9,members:17},'addp.topology-normalize')
})

test('重合规整链路合并独立普通User三成果与字段拒绝', async ({page}) => {
  await verifyNetworkCase(page,'line-network-normalize-chains',{chains:9,chain_edges:15,edge_sources:22},'addp.topology-normalize-chains')
})

test('多距缓冲独立普通User单成果与重名拒绝', async ({page}) => {
  await verifyNetworkCase(page,'multi-distance-buffer',{result:6},'addp.topology-buffer')
})

// Literal acceptance expectations: do not derive these from the production formatter.
const inputDiagnosticText = {
"a-id": {
    "zh-cn": [
      "输入图层不符合算子的类型、字段、几何或标识要求。",
      "相交叠加",
      "源图层（input_gdf）",
      "",
      "按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。"
    ],
    "en": [
      "The input layer does not meet the operator type, field, geometry or identifier requirements.",
      "Intersection overlay",
      "Source layer (input_gdf)",
      "",
      "Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution."
    ]
  },
  "b-id": {
    "zh-cn": [
      "参考图层不符合算子的类型、几何或标识要求。",
      "相交叠加",
      "图层 B（gdf_b）",
      "",
      "按算子说明检查参考图层的几何、坐标系和非空唯一标识后重新执行。"
    ],
    "en": [
      "The reference layer does not meet the operator type, geometry or identifier requirements.",
      "Intersection overlay",
      "Input layer B (gdf_b)",
      "",
      "Check the reference geometry, CRS and unique non-null identifiers before submitting a new execution."
    ]
  },
  "crs": {
    "zh-cn": [
      "两个图层须使用相同的已知投影坐标系和匹配的水平单位。",
      "相交叠加",
      "",
      "",
      "先显式统一两个图层的投影坐标系与水平单位，再重新执行。"
    ],
    "en": [
      "Both layers require the same known projected CRS and matching horizontal units.",
      "Intersection overlay",
      "",
      "",
      "Explicitly align both projected CRSs and horizontal units before submitting a new execution."
    ]
  },
  "fields": {
    "zh-cn": [
      "叠加分析的业务字段、固定来源字段或结果几何列发生重名。",
      "相交叠加",
      "",
      "",
      "先在上游手动重命名冲突字段；不会自动加后缀、覆盖或丢弃字段。"
    ],
    "en": [
      "Overlay business fields conflict with each other, fixed source fields or the result geometry column.",
      "Intersection overlay",
      "",
      "",
      "Manually rename conflicting source fields upstream; fields are not suffixed, replaced or discarded automatically."
    ]
  },
  'ring-field-conflict': {
    'zh-cn':['源图层字段 ring_id 与算子的固定输出字段重名。','面界提取','源图层（input_gdf）','ring_id','先在上游手动重命名源字段 ring_id，再重新执行；不会自动改名或覆盖。'],
    en:['Source field ring_id conflicts with a fixed operator output field.','Extract polygon rings','Source layer (input_gdf)','ring_id','Manually rename source field ring_id upstream before submitting a new execution; it is not renamed or overwritten automatically.']
  },
  'ring-geometry-invalid': {
    'zh-cn':['输入图层包含未通过有效性检查的几何。','面界提取','源图层（input_gdf）','','先运行几何检查，确认问题后显式修复；修复结果须满足算子说明，再提交新的执行。'],
    en:['The input layer contains geometry that failed the validity check.','Extract polygon rings','Source layer (input_gdf)','','Run geometry checks first, explicitly repair confirmed issues, and verify the result meets the operator requirements before submitting a new execution.']
  },
  'field-conflict': {
    'zh-cn':['源图层字段 distance 与算子的固定输出字段重名。','多距缓冲','源图层（input_gdf）','distance','先在上游手动重命名源字段 distance，再重新执行；不会自动改名或覆盖。'],
    en:['Source field distance conflicts with a fixed operator output field.','Multi-distance buffer','Source layer (input_gdf)','distance','Manually rename source field distance upstream before submitting a new execution; it is not renamed or overwritten automatically.']
  },
  'number-array': {
    'zh-cn':['距离列表必须为有限数值数组，不能包含布尔值、字符串或空值。','多距缓冲','距离列表（distances）','','检查 distances 数组中的全部元素；保留合法的零值、负值和重复距离。'],
    en:['Distances must be an array of finite numbers without booleans, strings or nulls.','Multi-distance buffer','Distances (distances)','','Check every distances array element; valid zero, negative and duplicate distances remain allowed.']
  },
  'crs-mismatch': {
    'zh-cn':['两个输入的投影坐标系不同。','距离计算','','','先显式统一两个输入的投影坐标系；不能仅因单位相同就直接计算。'],
    en:['The input projected CRSs differ.','Distance calculation','','','Explicitly align the projected CRSs; equal units alone do not make the CRSs equivalent.']
  },
  'positive-number': {
    'zh-cn':['容差必须为以源坐标系水平单位表示的有限正数。','近接检查','容差（tolerance）','','按源坐标系单位设置有限且大于零的容差后重新执行，不自动换算为米。'],
    en:['Tolerance must be a finite positive number in the source CRS horizontal units.','Line proximity check','Tolerance (tolerance)','','Set a finite positive tolerance in source CRS units; it is not converted to metres automatically.']
  }
}

// Frozen independent bilingual expectations; never resolve expected text from product i18n at runtime.
Object.assign(inputDiagnosticText, {
  "dissolve-id-missing": {
    "zh-cn": [
      "输入图层不符合算子的类型、字段、几何或标识要求。",
      "融合几何",
      "源图层（input_gdf）",
      "",
      "按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。"
    ],
    "en": [
      "The input layer does not meet the operator type, field, geometry or identifier requirements.",
      "Dissolve geometry",
      "Source layer (input_gdf)",
      "",
      "Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution."
    ]
  },
  "dissolve-id-duplicate": {
    "zh-cn": [
      "输入图层不符合算子的类型、字段、几何或标识要求。",
      "融合几何",
      "源图层（input_gdf）",
      "",
      "按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。"
    ],
    "en": [
      "The input layer does not meet the operator type, field, geometry or identifier requirements.",
      "Dissolve geometry",
      "Source layer (input_gdf)",
      "",
      "Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution."
    ]
  },
  "dissolve-invalid": {
    "zh-cn": [
      "输入图层不符合算子的类型、字段、几何或标识要求。",
      "融合几何",
      "源图层（input_gdf）",
      "",
      "按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。"
    ],
    "en": [
      "The input layer does not meet the operator type, field, geometry or identifier requirements.",
      "Dissolve geometry",
      "Source layer (input_gdf)",
      "",
      "Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution."
    ]
  },
  "dissolve-crs": {
    "zh-cn": [
      "两个图层须使用相同的已知投影坐标系和匹配的水平单位。",
      "融合几何",
      "",
      "",
      "先显式统一两个图层的投影坐标系与水平单位，再重新执行。"
    ],
    "en": [
      "Both layers require the same known projected CRS and matching horizontal units.",
      "Dissolve geometry",
      "",
      "",
      "Explicitly align both projected CRSs and horizontal units before submitting a new execution."
    ]
  },
  "dissolve-groups-type": {
    "zh-cn": [
      "分组字段列表无效、重复或包含非标量值。",
      "融合几何",
      "分组字段（group_fields）",
      "",
      "显式选择无重复的现有非几何标量字段；[]表示全局融合，空值组会保留。"
    ],
    "en": [
      "The group field list is invalid, duplicated or contains non-scalar values.",
      "Dissolve geometry",
      "Group fields (group_fields)",
      "",
      "Explicitly select distinct existing non-geometry scalar fields; [] means global dissolve and null groups are retained."
    ]
  },
  "dissolve-groups-duplicate": {
    "zh-cn": [
      "分组字段列表无效、重复或包含非标量值。",
      "融合几何",
      "分组字段（group_fields）",
      "",
      "显式选择无重复的现有非几何标量字段；[]表示全局融合，空值组会保留。"
    ],
    "en": [
      "The group field list is invalid, duplicated or contains non-scalar values.",
      "Dissolve geometry",
      "Group fields (group_fields)",
      "",
      "Explicitly select distinct existing non-geometry scalar fields; [] means global dissolve and null groups are retained."
    ]
  },
  "dissolve-groups-missing": {
    "zh-cn": [
      "分组字段列表无效、重复或包含非标量值。",
      "融合几何",
      "分组字段（group_fields）",
      "",
      "显式选择无重复的现有非几何标量字段；[]表示全局融合，空值组会保留。"
    ],
    "en": [
      "The group field list is invalid, duplicated or contains non-scalar values.",
      "Dissolve geometry",
      "Group fields (group_fields)",
      "",
      "Explicitly select distinct existing non-geometry scalar fields; [] means global dissolve and null groups are retained."
    ]
  },
  "dissolve-fields": {
    "zh-cn": [
      "融合固定输出字段与源列重名。",
      "融合几何",
      "",
      "",
      "先在上游手动重命名fusion_id/member_count/source_id重名列，不会自动覆盖。"
    ],
    "en": [
      "Fixed dissolve output fields conflict with source columns.",
      "Dissolve geometry",
      "",
      "",
      "Rename conflicting fusion_id/member_count/source_id columns upstream; they are not replaced automatically."
    ]
  }
})

Object.assign(inputDiagnosticText,{
  "difference-id": {
    "zh-cn": [
      "输入图层不符合算子的类型、字段、几何或标识要求。",
      "几何差集",
      "源图层（input_gdf）",
      "",
      "按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。"
    ],
    "en": [
      "The input layer does not meet the operator type, field, geometry or identifier requirements.",
      "Geometry difference",
      "Source layer (input_gdf)",
      "",
      "Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution."
    ]
  },
  "difference-geometry": {
    "zh-cn": [
      "输入图层不符合算子的类型、字段、几何或标识要求。",
      "几何差集",
      "源图层（input_gdf）",
      "",
      "按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。"
    ],
    "en": [
      "The input layer does not meet the operator type, field, geometry or identifier requirements.",
      "Geometry difference",
      "Source layer (input_gdf)",
      "",
      "Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution."
    ]
  },
  "difference-mask": {
    "zh-cn": [
      "范围图层必须具有唯一活动几何列及有效非空有限二维面或多面几何。",
      "几何差集",
      "图层 B（gdf_b）",
      "",
      "检查范围图层的活动几何、面类型、有效性和二维坐标；修正后重新执行，无需指定范围图层的业务来源标识。"
    ],
    "en": [
      "The range layer requires one active geometry column and valid non-empty finite XY Polygon or MultiPolygon geometries.",
      "Geometry difference",
      "Input layer B (gdf_b)",
      "",
      "Check the range layer active geometry, polygon type, validity and XY coordinates before submitting a new execution; no business identifier is required for the range layer."
    ]
  },
  "difference-crs": {
    "zh-cn": [
      "两个图层须使用相同的已知投影坐标系和匹配的水平单位。",
      "几何差集",
      "",
      "",
      "先显式统一两个图层的投影坐标系与水平单位，再重新执行。"
    ],
    "en": [
      "Both layers require the same known projected CRS and matching horizontal units.",
      "Geometry difference",
      "",
      "",
      "Explicitly align both projected CRSs and horizontal units before submitting a new execution."
    ]
  }
})

function inputDiagnosticPageExpectation(key,locale,executionID) {
  const [reason,operator,parameter,field,action]=inputDiagnosticText[key][locale]
  const stage=locale==='zh-cn'?'执行操作':'Execute operation'
  const node=locale==='zh-cn'?'定义中的第 1 个节点':'Node 1 in the workflow definition'
  return {reason,values:[stage,operator,node,parameter,field,action,'develop.workflow.invalid_params',executionID].filter(Boolean),
    clipboard:[reason,stage,operator,node,parameter,field,action,'develop.workflow.invalid_params',executionID].filter(Boolean).join('\n')}
}

async function assertInputDiagnosticPanel(panel,key,locale,executionID) {
  const expected=inputDiagnosticPageExpectation(key,locale,executionID)
  await expect(panel).toBeVisible()
  await expect(panel.locator('h4')).toHaveText(expected.reason)
  await expect(panel.locator('dd')).toHaveText(expected.values)
  await expect(panel).not.toContainText(/PRIVATE_DIAGNOSTIC|Traceback|site-packages|\/tmp\/|99\.0|parcel_shape|parcel_key|rings-(conflict|invalid)\.gpkg|相邻乙|重复丙|地块属性|普通业务几何/)
  if(['a-id','b-id','crs','fields'].includes(key))await expect(panel).not.toContainText(/PRIVATE_OVERLAY|a_key|b_key|a_shape|b_shape|a_note|overlay-(a|b|crs|fields)\.gpkg/)
  if(key.startsWith('dissolve-'))await expect(panel).not.toContainText(/PRIVATE_DISSOLVE|parcel_key|parcel_shape|dissolve-(source|duplicate-id|invalid|crs|fields)\.gpkg/)
  if(key.startsWith('difference-'))await expect(panel).not.toContainText(/PRIVATE_DIFFERENCE|a_key|a_shape|b_shape|difference-(a|b|invalid|mask|crs)\.gpkg/)
  return expected
}

async function verifyInputDiagnosticCase(page,caseName,supplied=null) {
  const evidence=supplied||JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}')
  expect(evidence.case).toBe(caseName)
  const rings=caseName==='polygon-rings-input-diagnostics',overlay=caseName==='intersection-overlay',dissolve=caseName==='polygon-dissolve',difference=caseName==='geometry-difference'
  const keys=difference?['difference-id','difference-geometry','difference-mask','difference-crs']:dissolve?["dissolve-id-missing", "dissolve-id-duplicate", "dissolve-invalid", "dissolve-crs", "dissolve-groups-type", "dissolve-groups-duplicate", "dissolve-groups-missing", "dissolve-fields"]:overlay?['a-id','b-id','crs','fields']:rings?['ring-field-conflict','ring-geometry-invalid']:['field-conflict','number-array','crs-mismatch','positive-number']
  expect(evidence.executions.map(run=>run.key)).toEqual(keys)
  expect(new Set(evidence.executions.map(run=>run.execution_id)).size).toBe(keys.length)
  const expectedIdentity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  await page.context().grantPermissions(['clipboard-read','clipboard-write'],{origin:process.env.CONSOLE_URL})
  // The combined overlay scene is already logged in; standalone diagnostics still log in here.
  let loggedIn=(overlay||dissolve||difference)&&supplied!==null
  const observations=[],publicationRequests=[]
  page.on('request',request=>{
    const path=new URL(request.url()).pathname
    if(!['GET','HEAD','OPTIONS'].includes(request.method()) &&
      (path.startsWith('/api/v1/develop/executions')||path.startsWith('/api/v1/meta/scan')||path.startsWith('/api/v1/meta/lineage')))
      publicationRequests.push({method:request.method(),path})
  })
  for(const run of evidence.executions)for(const owner of ['develop','monitor']){
    const path=owner==='develop'?`/develop/executions/${run.execution_id}`:`/monitor/executions?execution_id=${run.execution_id}`
    const matches=response=>response.request().method()==='GET'&&new URL(response.url()).pathname===
      (owner==='develop'?`/api/v1/develop/executions/${run.execution_id}`:`/api/v1/monitor/executions/by-execution-id/${run.execution_id}/tree`)
    const observed=page.waitForResponse(matches)
    if(!loggedIn){await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,path);loggedIn=true}
    else await page.goto(`${process.env.CONSOLE_URL}${path}`)
    const first=await observed
    const bound=await browserAPIFromResponse(first,process.env.GATEWAY_URL,expectedIdentity)
    await bound.dispose()
    const frame=page.frameLocator('iframe[data-testid="module-iframe"]')
    const panel=frame.locator(owner==='develop'?'.execution-detail-page > .execution-failure':'.execution-detail-content > .execution-failure')
    for(const locale of ['zh-cn','en']){
      await page.locator('.header-language button').click()
      await page.getByRole('menuitem',{name:locale==='zh-cn'?'中文':'English',exact:true}).click()
      await assertInputDiagnosticPanel(panel,run.key,locale,run.execution_id)
      // Refresh the same UUID. Never retry the failed workflow or reuse a pre-navigation token.
      const reread=page.waitForResponse(matches)
      await page.reload()
      const response=await reread
      const api=await browserAPIFromResponse(response,process.env.GATEWAY_URL,expectedIdentity)
      let raw
      try{raw=await json(response,'operator diagnostic owner page')}finally{await api.dispose()}
      const execution=owner==='develop'?raw:raw.execution
      expect(execution.execution_id).toBe(run.execution_id)
      expect(Number.isInteger(execution.triggered_by)).toBe(true)
      expect(execution.triggered_by).toBe(Number(evidence.principal_id))
      expect(execution.status).toBe('failed')
      expect(execution.error_details).toEqual(run.execution.error_details)
      expect(execution.error_details.diagnostic).toEqual(run.expected_diagnostic)
      const expected=await assertInputDiagnosticPanel(panel,run.key,locale,run.execution_id)
      await page.evaluate(()=>navigator.clipboard.writeText('DIAGNOSTIC_CLIPBOARD_NOT_WRITTEN'))
      await panel.getByRole('button',{name:locale==='zh-cn'?'复制诊断信息':'Copy diagnostics',exact:true}).click()
      await expect.poll(()=>page.evaluate(()=>navigator.clipboard.readText())).toBe(expected.clipboard)
      const clipboard=await page.evaluate(()=>navigator.clipboard.readText())
      const screenshot=`operator-input-${run.key}-${owner}-${locale}.png`
      await page.screenshot({path:resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,screenshot)})
      observations.push({key:run.key,owner,locale,execution_id:run.execution_id,raw,clipboard,screenshot})
    }
  }
  if(dissolve) expect(observations).toHaveLength(32)
  else if(rings) expect(observations).toHaveLength(8)
  else expect(observations).toHaveLength(16)
  expect(publicationRequests).toEqual([])
  if(overlay||dissolve||difference)return observations
  writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,'topology-browser.json'),JSON.stringify({
    ...evidence,schema_version:(rings?'addp.polygon-rings-input-diagnostics':'addp.operator-input-diagnostics')+'-browser/v1',result:'passed',observations},null,2)+'\n')
}

test('算子参数诊断独立普通User双语失败页',async({page})=>verifyInputDiagnosticCase(page,'operator-input-diagnostics'))
test('面界提取参数诊断独立普通User双语失败页',async({page})=>verifyInputDiagnosticCase(page,'polygon-rings-input-diagnostics'))

function proximityPointExpectation(features) {
  return features.map(row=>JSON.stringify({type:'Point',properties:row.properties,coordinates:webMercatorPreviewCoordinate(row.geometry.coordinates)})).sort()
}

async function assertProximityPointMap(renderer,expected) {
  const fields=Object.keys(expected[0].properties)
  let observed
  await expect.poll(async()=>{
    observed=await renderer.evaluate((element,fields)=>{
      const ref=element.__vueParentComponent?.setupState?.mapInstance,map=ref?.value||ref
      if(!map||map.getView().getAnimating())return null
      return {projection:map.getView().getProjection().getCode(),features:map.getLayers().getArray().flatMap(layer=>layer.getSource?.()?.getFeatures?.()||[])
        .map(feature=>({type:feature.getGeometry().getType(),coordinates:feature.getGeometry().getCoordinates(),properties:Object.fromEntries(fields.map(field=>[field,feature.get(field)]))}))}
    },fields)
    if(!observed||observed.projection!=='EPSG:3857'||observed.features.some(feature=>feature.type!=='Point'))return []
    return observed.features.map(({type,properties,coordinates})=>JSON.stringify({type,properties,coordinates})).sort()
  }).toEqual(proximityPointExpectation(expected))
  expect(observed.features).toHaveLength(expected.length)
  expect(new Set(observed.features.map(feature=>JSON.stringify(feature.coordinates))).size).toBe(new Set(expected.map(row=>JSON.stringify(row.geometry.coordinates))).size)
  let paintedPixels=0
  await expect.poll(async()=>paintedPixels=await renderer.evaluate(element=>{
    const ref=element.__vueParentComponent?.setupState?.mapInstance,map=ref?.value||ref
    if(!map)return 0
    const probe=new OffscreenCanvas(1,1).getContext('2d')
    let count=0
    for(const layer of map.getLayers().getArray()){
      const features=layer.getSource?.()?.getFeatures?.()||[]
      if(!features.length||!layer.getVisible()||layer.getOpacity()===0)continue
      const feature=features[0],style=layer.getStyleFunction()?.(feature,map.getView().getResolution())
      const styles=Array.isArray(style)?style:[style]
      const colors=styles.map(value=>value?.getImage()?.getFill()?.getColor()).filter(Boolean).map(color=>{
        probe.clearRect(0,0,1,1);probe.fillStyle=color;probe.fillRect(0,0,1,1)
        return [...probe.getImageData(0,0,1,1).data]
      })
      const context=layer.getRenderer()?.context
      if(!context||!colors.length)continue
      const pixels=context.getImageData(0,0,context.canvas.width,context.canvas.height).data
      for(let i=0;i<pixels.length;i+=4){
        if(pixels[i+3]>0&&colors.some(color=>color[3]>0&&pixels[i]===color[0]&&pixels[i+1]===color[1]&&pixels[i+2]===color[2]))count++
      }
    }
    return count
  })).toBeGreaterThan(30)
  return {...observed,painted_pixels:paintedPixels}
}

function assertProximityLineage(graph,evidence) {
  expect(graph.truncated).toBe(false)
  const edges=graph.edges.filter(edge=>edge.relation_kind==='derive'&&edge.status==='active'&&edge.evidence?.execution_id===evidence.execution_id)
  expect(edges).toHaveLength(2)
  expect(edges.map(edge=>[edge.source.item_id,edge.target.item_id]).sort((a,b)=>a[0]-b[0]))
    .toEqual(evidence.sources.map(source=>[source.id,evidence.output.item_id]).sort((a,b)=>a[0]-b[0]))
}

function persistProximityEvidence(directory,raw,stage,result='incomplete') {
  writeFileSync(resolve(directory,'topology-proximity-browser-raw.json'),JSON.stringify({...raw,stage,result},null,2)+'\n')
}

test('近接检查独立普通User双源Point成果',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}')
  expect(evidence.case).toBe('line-proximity')
  expect(evidence.sources).toHaveLength(2);expect(evidence.expected).toHaveLength(7)
  const output=evidence.output,fields=Object.keys(evidence.expected[0].properties)
  const identity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  const directory=process.env.ADDP_ONLINE_ARTIFACT_DIR
  await observeLineageCanvas(page)
  const writes=[]
  page.on('request',request=>{
    if(request.method()==='POST'&&new URL(request.url()).pathname.startsWith('/api/v1/develop/executions'))writes.push(request.url())
  })
  const executionRead=page.waitForResponse(response=>response.request().method()==='GET'&&new URL(response.url()).pathname===`/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,`/develop/executions/${evidence.execution_id}`)
  const response=await executionRead,executionAPI=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
  const raw={schema_version:'addp.line-proximity-browser-raw/v1',case:evidence.case,run_id:evidence.run_id,execution_id:evidence.execution_id}
  try{
    raw.execution=await json(response,'proximity execution page')
    expect(raw.execution.status).toBe('success');expect(raw.execution.execution_id).toBe(evidence.execution_id)
    expect(raw.execution.triggered_by).toBe(Number(evidence.principal_id))
    expect(raw.execution.outputs).toEqual({save_result:{resource:{locator:output.locator,type:'file',write_mode:'create'}}})
  }finally{await executionAPI.dispose()}
  const frame=page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功',{exact:true})).toBeVisible()
  await expect(frame.locator('.workflow-result')).toContainText(output.name)
  await page.screenshot({path:resolve(directory,'line-proximity-execution.png')})
  persistProximityEvidence(directory,raw,'execution_verified')
  const locator=`${output.locator}&item_id=${output.item_id}`
  const previewRead=page.waitForResponse(response=>{
    const url=new URL(response.url())
    return response.request().method()==='GET'&&url.pathname==='/api/v1/manager/preview'&&url.searchParams.get('locator')===locator&&url.searchParams.get('child_name')===output.layer_name
  })
  await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
  const previewResponse=await previewRead,api=await browserAPIFromResponse(previewResponse,process.env.GATEWAY_URL,identity)
  try{
    raw.preview=await json(previewResponse,'proximity Point preview')
    raw.sources=[]
    for(const source of evidence.sources){
      const item=await json(await api.get(`/api/v1/meta/items/${source.id}`),'unchanged proximity source')
      expect(item).toEqual(source.before);raw.sources.push({item_id:source.id,item})
    }
    raw.output=await json(await api.get(`/api/v1/meta/items/${output.item_id}`),'proximity Meta')
    expect(raw.output).toEqual(output.before)
  }finally{await api.dispose()}
  const preview=raw.preview,data=preview.data
  expect(preview.preview_type).toBe('table')
  expect([data.total,data.source_srid,data.source_crs,data.transform_status,data.preview_hint]).toEqual([7,3857,'EPSG:3857','not_transformed','frontend_transform_required'])
  expect(data.geometry_columns).toEqual([output.geometry_column])
  expect([...data.columns].sort()).toEqual([output.fid_column,output.geometry_column,...fields].sort())
  expect(data.rows.map(row=>row[output.fid_column])).toEqual([1,2,3,4,5,6,7])
  expect(data.rows.map(row=>({properties:Object.fromEntries(fields.map(key=>[key,row[key]])),geometry:decodePreviewEWKB(row[output.geometry_column])}))).toEqual(evidence.expected)
  persistProximityEvidence(directory,raw,'preview_api_verified')
  const table=frame.locator('.table-preview')
  await expect(table).toBeVisible();await expect(table.locator('.map-placeholder')).toHaveCount(0)
  raw.rendered=await assertProximityPointMap(table.locator('.openlayers-map-renderer'),evidence.expected)
  await page.screenshot({path:resolve(directory,'line-proximity-point-map.png')})
  persistProximityEvidence(directory,raw,'point_map_verified')
  await frame.getByRole('tab',{name:'属性',exact:true}).click()
  await expect(frame.locator('.attributes-tab-pane')).toBeVisible()
  await expect(frame.locator('.attributes-tab-pane')).toContainText(output.layer_name)
  await page.screenshot({path:resolve(directory,'line-proximity-attributes.png')})
  persistProximityEvidence(directory,raw,'attributes_verified')
  const lineageRead=page.waitForResponse(response=>{
    const url=new URL(response.url())
    return response.request().method()==='GET'&&url.pathname==='/api/v1/meta/lineage/graph'&&url.searchParams.get('item_id')===String(output.item_id)&&url.searchParams.get('direction')==='both'
  })
  await frame.getByRole('tab',{name:'血缘',exact:true}).click()
  const lineageResponse=await lineageRead,lineageAPI=await browserAPIFromResponse(lineageResponse,process.env.GATEWAY_URL,identity)
  try{raw.graph=await json(lineageResponse,'proximity actual lineage tab')}finally{await lineageAPI.dispose()}
  assertProximityLineage(raw.graph,evidence)
  const viewer=frame.locator('.lineage-viewer'),canvas=viewer.locator('.lineage-canvas canvas').first()
  await expect(canvas).toBeVisible()
  await expect.poll(async()=>{
    raw.lineage_rendered=await viewer.evaluate(element=>{
      const ref=element.__vueParentComponent?.setupState?.graphInstance,graph=ref?.value||ref
      return graph?{nodes:graph.getNodes().map(item=>item.getModel()._node),edges:graph.getEdges().map(item=>item.getModel()._edge)}:null
    })
    return raw.lineage_rendered?.edges?.length||0
  }).toBe(2)
  assertProximityLineage({...raw.graph,edges:raw.lineage_rendered.edges},evidence)
  await expect.poll(async()=>{
    raw.lineage_canvas=await lineageCanvasSnapshot(canvas)
    return [...evidence.sources.map(source=>source.name),output.name].every(name=>raw.lineage_canvas.rows.some(row=>row.text.includes(name)))
  }).toBe(true)
  expect(raw.lineage_canvas.paths.length).toBeGreaterThanOrEqual(2)
  await page.screenshot({path:resolve(directory,'line-proximity-lineage.png')})
  expect(writes).toEqual([])
  persistProximityEvidence(directory,raw,'complete','passed')
  writeFileSync(resolve(directory,'topology-browser.json'),JSON.stringify({...evidence,schema_version:'addp.line-proximity-browser/v1',result:'passed'},null,2)+'\n')
})

async function assertExtensionLineMap(renderer,expected,probe=[2,0]) {
  const fields=Object.keys(expected[0].properties)
  let rendered
  await expect.poll(async()=>{
    rendered=await renderer.evaluate((element,fields)=>{
      const ref=element.__vueParentComponent?.setupState?.mapInstance,map=ref?.value||ref
      if(!map||map.getView().getAnimating())return null
      return {projection:map.getView().getProjection().getCode(),features:map.getLayers().getArray()
        .filter(layer=>layer.getVisible()&&layer.getOpacity()>0).flatMap(layer=>layer.getSource?.()?.getFeatures?.()||[])
        .map(feature=>({type:feature.getGeometry().getType(),coordinates:feature.getGeometry().getCoordinates(),properties:Object.fromEntries(fields.map(key=>[key,feature.get(key)]))}))}
    },fields)
    if(!rendered||rendered.projection!=='EPSG:3857'||rendered.features.some(f=>f.type!==expected[0].geometry.type))return []
    return rendered.features.map(({properties,coordinates})=>JSON.stringify({properties,coordinates})).sort()
  }).toEqual(normalizedMapExpectation(expected))
  let pixels
  await expect.poll(async()=>{
    pixels=await renderer.evaluate((element,coordinate)=>{
      const ref=element.__vueParentComponent?.setupState?.mapInstance,map=ref?.value||ref
      const position=map.getPixelFromCoordinate(coordinate),viewport=map.getViewport().getBoundingClientRect()
      let total=0,added=0
      for(const layer of map.getLayers().getArray()){
        if(!layer.getVisible()||layer.getOpacity()<=0||!layer.getSource?.()?.getFeatures?.()?.length)continue
        const context=layer.getRenderer()?.context
        if(!context)continue
        const canvas=context.canvas,rect=canvas.getBoundingClientRect(),data=context.getImageData(0,0,canvas.width,canvas.height).data
        const x=Math.round((position[0]+viewport.left-rect.left)*canvas.width/rect.width)
        const y=Math.round((position[1]+viewport.top-rect.top)*canvas.height/rect.height)
        for(let py=0;py<canvas.height;py++)for(let px=0;px<canvas.width;px++){
          const i=(py*canvas.width+px)*4
          if(data[i+3]>0&&data[i+1]>data[i]*1.1&&data[i+1]>data[i+2]*1.1){
            total++;if(Math.abs(px-x)<=3&&Math.abs(py-y)<=3)added++
          }
        }
      }
      return {painted_pixels:total,new_segment_pixels:added}
    },webMercatorPreviewCoordinate(probe))
    return pixels.painted_pixels>30&&pixels.new_segment_pixels>0
  }).toBe(true)
  return {...rendered,...pixels}
}

async function assertTrimRemovedSegment(renderer) {
  let removed
  await expect.poll(async()=>{
    removed=await renderer.evaluate(element=>{
      const ref=element.__vueParentComponent.setupState.mapInstance,map=ref?.value||ref
      const position=map.getPixelFromCoordinate([0,0]),viewport=map.getViewport().getBoundingClientRect()
      let pixels=0,observed=0
      for(const layer of map.getLayers().getArray()){
        if(!layer.getVisible()||layer.getOpacity()<=0||!layer.getSource?.()?.getFeatures?.()?.length)continue
        const context=layer.getRenderer()?.context;if(!context)continue
        const canvas=context.canvas,rect=canvas.getBoundingClientRect()
        const x=Math.round((position[0]+viewport.left-rect.left)*canvas.width/rect.width),y=Math.round((position[1]+viewport.top-rect.top)*canvas.height/rect.height)
        if(x<3||y<3||x>=canvas.width-3||y>=canvas.height-3)continue
        observed++;const data=context.getImageData(x-3,y-3,7,7).data
        for(let i=0;i<data.length;i+=4)if(data[i+3]>0&&data[i+1]>data[i]*1.1&&data[i+1]>data[i+2]*1.1)pixels++
      }
      return observed>0?pixels:-1
    })
    return removed
  }).toBe(0)
  return removed
}

async function assertTrimFailurePanel(panel,failure) {
  const reasons={limit:'最大修剪路径距离必须为以源坐标系水平单位表示的有限正数。',
    source:'输入图层不符合算子的类型、字段、几何或标识要求。',reference:'参考图层不符合算子的类型、几何或标识要求。',
    crs:'两个图层须使用相同的已知投影坐标系和匹配的水平单位。',
    self:'源部件存在内部自交或源线正长度重合，不能直接修剪。',overlap:'源部件存在内部自交或源线正长度重合，不能直接修剪。',
    conflict:'两端修剪计划交错或相遇，会使原部件消失。',landing:'执行失败，未记录具体诊断'}
  expect(Object.hasOwn(reasons,failure.key)).toBe(true)
  await expect(panel).toBeVisible();await expect(panel.locator('h4')).toHaveText(reasons[failure.key])
  await expect(panel).toContainText(failure.execution_id)
  if(failure.expected_diagnostic){
    await expect(panel).toContainText('线端修剪');await expect(panel).toContainText('定义中的第 1 个节点')
    await expect(panel).toContainText('develop.workflow.invalid_params')
    const parameter=failure.expected_diagnostic.operator.parameter
    if(parameter)await expect(panel).toContainText(parameter)
  }else await expect(panel).toContainText('develop.workflow.execution_failed')
  await expect(panel).not.toContainText(/source_shape|reference_shape|route_key|ref_key|修剪属性|普通业务几何|Traceback|site-packages|\/tmp\//)
}

function persistExtensionEvidence(directory,raw,stage,result='incomplete') {
  writeFileSync(resolve(directory,raw.case==='line-trimming'?'topology-trim-browser-raw.json':raw.case==='endpoint-snapping'?'topology-endpoint-browser-raw.json':'topology-extension-browser-raw.json'),JSON.stringify({...raw,stage,result},null,2)+'\n')
}

async function verifyLineEditCase(page,evidence,directory) {
  const trim=evidence.case==='line-trimming'
  const endpoint=evidence.case==='endpoint-snapping',prefix=trim?'line-trimming':endpoint?'endpoint':'line-extension',schema=trim?'addp.line-trimming':endpoint?'addp.endpoint-snapping':'addp.line-extension'
  expect(evidence.sources).toHaveLength(trim?11:endpoint?7:3)
  expect(evidence.outputs.map(o=>o.port)).toEqual(['result','details'])
  expect(evidence.expected.result).toHaveLength(trim?15:endpoint?12:7);expect(evidence.expected.details).toHaveLength(trim?9:endpoint?10:8)
  const identity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  const raw={schema_version:schema+'-browser-raw/v1',case:evidence.case,run_id:evidence.run_id,execution_id:evidence.execution_id,observations:[]}
  const persist=(stage,result='incomplete')=>persistExtensionEvidence(directory,raw,stage,result)
  const writes=[]
  page.on('request',request=>{const path=new URL(request.url()).pathname
    if(!['GET','HEAD','OPTIONS'].includes(request.method())&&/^\/api\/v1\/(develop|meta|manager)\//.test(path))writes.push({method:request.method(),path})})
  const read=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,`/develop/executions/${evidence.execution_id}`)
  const response=await read,executionAPI=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
  try{raw.execution=await json(response,'extension success');expect(raw.execution.execution_id).toBe(evidence.execution_id);expect(raw.execution.triggered_by).toBe(Number(evidence.principal_id));expect(raw.execution.status).toBe('success');expect(Object.keys(raw.execution.outputs).sort()).toEqual(['save_details','save_result'])}finally{await executionAPI.dispose()}
  const frame=page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功',{exact:true})).toBeVisible()
  for(const output of evidence.outputs)await expect(frame.locator('.workflow-result')).toContainText(output.name)
  await page.screenshot({path:resolve(directory,`${prefix}-execution.png`)});persist('execution_verified')
  for(const output of evidence.outputs){
    const expected=evidence.expected[output.port],fields=Object.keys(expected[0].properties),locator=`${output.locator}&item_id=${output.item_id}`
    const previewRead=page.waitForResponse(r=>{const url=new URL(r.url());return r.request().method()==='GET'&&url.pathname==='/api/v1/manager/preview'&&url.searchParams.get('locator')===locator&&url.searchParams.get('child_name')===output.layer_name})
    await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
    const previewResponse=await previewRead,api=await browserAPIFromResponse(previewResponse,process.env.GATEWAY_URL,identity)
    const observation={port:output.port,output_item_id:output.item_id,sources:[]};raw.observations.push(observation)
    try{
      observation.preview=await json(previewResponse,'extension actual preview')
      for(const source of evidence.sources){const item=await json(await api.get(`/api/v1/meta/items/${source.id}`),'unchanged extension source');expect(item).toEqual(source.before);observation.sources.push({item_id:source.id,item})}
      observation.output=await json(await api.get(`/api/v1/meta/items/${output.item_id}`),'extension output Meta');expect(observation.output).toEqual(output.before)
    }finally{await api.dispose()}
    persist(output.port+'_preview_observed')
    const preview=observation.preview,data=preview.data
    expect(preview.preview_type).toBe('table');expect([data.total,data.source_srid,data.source_crs,data.transform_status,data.preview_hint]).toEqual([expected.length,3857,'EPSG:3857','not_transformed','frontend_transform_required'])
    expect(data.geometry_columns).toEqual([output.geometry_column]);expect(output.fid_column).toBeTruthy()
    expect([...data.columns].sort()).toEqual([output.fid_column,output.geometry_column,...fields].sort())
    expect(data.rows.map(r=>r[output.fid_column])).toEqual(Array.from({length:expected.length},(_,i)=>i+1))
    expect(data.rows.map(r=>({properties:Object.fromEntries(fields.map(key=>[key,r[key]])),geometry:decodePreviewEWKB(r[output.geometry_column])}))).toEqual(expected)
    const table=frame.locator('.table-preview');await expect(table).toBeVisible();await expect(table.locator('.map-placeholder')).toHaveCount(0)
    const renderer=table.locator('.openlayers-map-renderer')
    if((endpoint||trim)&&output.port==='result'){
      await expect.poll(async()=>renderer.evaluate(element=>{
        const ref=element.__vueParentComponent?.setupState?.mapInstance,map=ref?.value||ref
        if(!map)return false
        map.getView().setCenter([24.25,4]);map.getView().setResolution(.01);map.renderSync();return true
      })).toBe(true)
      if(trim)await renderer.evaluate(element=>{const ref=element.__vueParentComponent.setupState.mapInstance,map=ref?.value||ref;map.getView().setCenter([1.5,1]);map.getView().setResolution(.02);map.renderSync()})
    }
    observation.rendered=(endpoint||trim)&&output.port==='details'?await assertProximityPointMap(renderer,expected):await assertExtensionLineMap(renderer,expected,trim?[1,0]:endpoint?[24.25,4]:[2,0])
    if(trim&&output.port==='result')observation.removed_segment_pixels=await assertTrimRemovedSegment(renderer)
    await page.screenshot({path:resolve(directory,`${prefix}-${output.port}-map.png`)});persist(output.port+'_map_verified')
    await frame.getByRole('tab',{name:'属性',exact:true}).click();await expect(frame.locator('.attributes-tab-pane')).toContainText(output.layer_name)
    await page.screenshot({path:resolve(directory,`${prefix}-${output.port}-attributes.png`)})
    const graphRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/meta/lineage/graph'&&u.searchParams.get('item_id')===String(output.item_id)&&u.searchParams.get('direction')==='both'})
    await frame.getByRole('tab',{name:'血缘',exact:true}).click()
    const graphResponse=await graphRead,graphAPI=await browserAPIFromResponse(graphResponse,process.env.GATEWAY_URL,identity)
    try{observation.graph=await json(graphResponse,'extension actual lineage tab')}finally{await graphAPI.dispose()}
    persist(output.port+'_graph_observed')
    const binding={...evidence,sources:evidence.sources.slice(0,2),output}
    assertProximityLineage(observation.graph,binding)
    expect(observation.graph.edges.filter(e=>e.relation_kind==='derive'&&e.status==='active')).toHaveLength(2)
    const viewer=frame.locator('.lineage-viewer'),canvas=viewer.locator('.lineage-canvas canvas').first();await expect(canvas).toBeVisible()
    await expect.poll(async()=>{
      observation.lineage_rendered=await viewer.evaluate(element=>{const ref=element.__vueParentComponent?.setupState?.graphInstance,graph=ref?.value||ref;return graph?{nodes:graph.getNodes().map(i=>i.getModel()._node),edges:graph.getEdges().map(i=>i.getModel()._edge)}:null})
      return observation.lineage_rendered?.edges?.length||0
    }).toBe(2)
    assertProximityLineage({...observation.graph,edges:observation.lineage_rendered.edges},binding)
    persist(output.port+'_graph_rendered')
    await expect.poll(async()=>{observation.lineage_canvas=await lineageCanvasSnapshot(canvas);return [...binding.sources.map(s=>s.name),output.name].every(name=>observation.lineage_canvas.rows.some(r=>r.text.includes(name)))}).toBe(true)
    expect(observation.lineage_canvas.paths.length).toBeGreaterThanOrEqual(2)
    await page.screenshot({path:resolve(directory,`${prefix}-${output.port}-lineage.png`)});persist(output.port+'_lineage_verified')
  }
  raw.failures=[]
  const failures=endpoint||trim?evidence.failures:[{key:'first-intersection',execution_id:evidence.failed_execution_id}]
  expect(failures).toHaveLength(trim?8:endpoint?4:1)
  expect(new Set(failures.map(f=>f.execution_id)).size).toBe(failures.length)
  for(const failure of failures){
    const failureRead=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${failure.execution_id}`)
    await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${failure.execution_id}`)
    const failedResponse=await failureRead,failedAPI=await browserAPIFromResponse(failedResponse,process.env.GATEWAY_URL,identity)
    const record={key:failure.key,execution_id:failure.execution_id};raw.failures.push(record)
    try{
      record.execution=await json(failedResponse,'line edit failed execution');expect(record.execution.execution_id).toBe(failure.execution_id);expect(record.execution.triggered_by).toBe(Number(evidence.principal_id));expect(record.execution.status).toBe('failed');expect(record.execution.outputs||{}).toEqual({})
      record.monitor=await json(await failedAPI.get(`/api/v1/monitor/executions/by-execution-id/${failure.execution_id}`),'line edit failed Monitor');expect(record.monitor.execution_id).toBe(failure.execution_id);expect(record.monitor.triggered_by).toBe(Number(evidence.principal_id));expect(record.monitor.module).toBe('develop');expect(record.monitor.status).toBe('failed')
      for(const entry of [...evidence.sources,...evidence.outputs])expect(await json(await failedAPI.get(`/api/v1/meta/items/${entry.item_id||entry.id}`),'unchanged item after failure')).toEqual(entry.before)
    }finally{await failedAPI.dispose()}
    await expect(frame.getByText('失败',{exact:true})).toBeVisible()
    if(trim)await assertTrimFailurePanel(frame.locator('.execution-detail-page > .execution-failure'),failure)
    await page.screenshot({path:resolve(directory,endpoint||trim?`${prefix}-${failure.key}-failed.png`:'line-extension-failed.png')})
    if(trim){
      expect(record.execution.error_details).toEqual(failure.error_details);expect(record.monitor.error_details).toEqual(failure.error_details)
      const monitorRead=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/monitor/executions/by-execution-id/${failure.execution_id}/tree`)
      await page.goto(`${process.env.CONSOLE_URL}/monitor/executions?execution_id=${failure.execution_id}`)
      const response=await monitorRead,api=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
      try{record.monitor_page=await json(response,'trim real failed Monitor page')}finally{await api.dispose()}
      expect(record.monitor_page.execution.execution_id).toBe(failure.execution_id);expect(record.monitor_page.execution.triggered_by).toBe(Number(evidence.principal_id));expect(record.monitor_page.execution.status).toBe('failed');expect(record.monitor_page.execution.error_details).toEqual(failure.error_details)
      await assertTrimFailurePanel(frame.locator('.execution-detail-content > .execution-failure'),failure)
      await page.screenshot({path:resolve(directory,`${prefix}-${failure.key}-monitor-failed.png`)})
    }
    if(!endpoint&&!trim){raw.failure=record.execution;raw.failed_monitor=record.monitor}
    persist('failed_'+failure.key+'_verified')
  }
  expect(writes).toEqual([]);persist('complete','passed')
  writeFileSync(resolve(directory,'topology-browser.json'),JSON.stringify({...evidence,schema_version:schema+'-browser/v1',result:'passed'},null,2)+'\n')
}

test('线端延伸独立普通User双成果与精确首次交点拒绝',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}'),directory=process.env.ADDP_ONLINE_ARTIFACT_DIR
  expect(evidence.case).toBe('line-extension')
  await observeLineageCanvas(page)
  await verifyLineEditCase(page,evidence,directory)
})

test('端点吸附独立普通User线与Point双成果及四失败保护',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}'),directory=process.env.ADDP_ONLINE_ARTIFACT_DIR
  expect(evidence.case).toBe('endpoint-snapping')
  await observeLineageCanvas(page)
  await verifyLineEditCase(page,evidence,directory)
})

function assertRingsLineage(graph,evidence) {
  expect(graph.truncated).toBe(false)
  const edges=graph.edges.filter(edge=>edge.relation_kind==='derive'&&edge.status==='active')
  expect(edges.map(edge=>[edge.source.item_id,edge.target.item_id,edge.evidence?.execution_id]))
    .toEqual([[evidence.sources[0].id,evidence.outputs[0].item_id,evidence.execution_id]])
}

function persistRingsEvidence(directory,raw,stage,result='incomplete') {
  writeFileSync(resolve(directory,'topology-rings-browser-raw.json'),JSON.stringify({...raw,stage,result},null,2)+'\n')
}

test('面界提取独立普通User单成果与两失败保护',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}'),directory=process.env.ADDP_ONLINE_ARTIFACT_DIR
  expect(evidence.case).toBe('polygon-rings');expect(evidence.sources).toHaveLength(3);expect(evidence.outputs).toHaveLength(1)
  expect(evidence.failures.map(f=>f.key)).toEqual(['conflict','invalid'])
  const expected=evidence.expected.result,output=evidence.outputs[0],identity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  expect(expected).toHaveLength(8)
  const raw={schema_version:'addp.polygon-rings-browser-raw/v1',case:evidence.case,run_id:evidence.run_id,execution_id:evidence.execution_id}
  const persist=(stage,result='incomplete')=>persistRingsEvidence(directory,raw,stage,result)
  const writes=[]
  page.on('request',request=>{const path=new URL(request.url()).pathname
    if(!['GET','HEAD','OPTIONS'].includes(request.method())&&/^\/api\/v1\/(develop|meta|manager)\//.test(path))writes.push({method:request.method(),path})})
  await observeLineageCanvas(page)
  const read=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,`/develop/executions/${evidence.execution_id}`)
  const response=await read,executionAPI=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
  try{raw.execution=await json(response,'rings success');expect(raw.execution.execution_id).toBe(evidence.execution_id);expect(raw.execution.triggered_by).toBe(Number(evidence.principal_id));expect(raw.execution.status).toBe('success');expect(Object.keys(raw.execution.outputs)).toEqual(['save_result'])}finally{await executionAPI.dispose()}
  const frame=page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功',{exact:true})).toBeVisible();await expect(frame.locator('.workflow-result')).toContainText(output.name)
  await page.screenshot({path:resolve(directory,'polygon-rings-execution.png')});persist('execution_verified')
  const locator=`${output.locator}&item_id=${output.item_id}`
  const previewRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/manager/preview'&&u.searchParams.get('locator')===locator&&u.searchParams.get('child_name')===output.layer_name})
  await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
  const previewResponse=await previewRead,api=await browserAPIFromResponse(previewResponse,process.env.GATEWAY_URL,identity)
  try{
    raw.preview_text=await previewResponse.text();raw.preview=JSON.parse(raw.preview_text);raw.sources=[]
    for(const source of evidence.sources){const item=await json(await api.get(`/api/v1/meta/items/${source.id}`),'unchanged rings source');expect(item).toEqual(source.before);raw.sources.push({item_id:source.id,item})}
    raw.output=await json(await api.get(`/api/v1/meta/items/${output.item_id}`),'rings output Meta');expect(raw.output).toEqual(output.before)
  }finally{await api.dispose()}
  persist('preview_observed')
  const data=raw.preview.data,fields=Object.keys(expected[0].properties)
  expect(raw.preview.preview_type).toBe('table');expect([data.total,data.source_srid,data.source_crs,data.transform_status,data.preview_hint]).toEqual([8,3857,'EPSG:3857','not_transformed','frontend_transform_required'])
  expect(data.geometry_columns).toEqual([output.geometry_column]);expect(output.geometry_column).not.toBe('geometry')
  expect([...data.columns].sort()).toEqual([output.fid_column,output.geometry_column,...fields].sort())
  expect(data.rows.map(r=>r[output.fid_column])).toEqual([1,2,3,4,5,6,7,8])
  const safeFields=fields.filter(f=>f!=='grade') // Exact Int64 checked from preview_text by the Python owner.
  expect(data.rows.map(r=>({properties:Object.fromEntries(safeFields.map(f=>[f,r[f]])),geometry:decodePreviewEWKB(r[output.geometry_column])})))
    .toEqual(expected.map(r=>({...r,properties:Object.fromEntries(safeFields.map(f=>[f,r.properties[f]]))})))
  const renderer=frame.locator('.table-preview .openlayers-map-renderer');await expect(renderer).toBeVisible()
  const mapFields=['parcel_key','source_id','part_no','ring_no','ring_id','is_hole']
  const mapExpected=expected.map(r=>({...r,properties:Object.fromEntries(mapFields.map(f=>[f,r.properties[f]]))}))
  raw.rendered=await assertExtensionLineMap(renderer,mapExpected,[4,0])
  await page.screenshot({path:resolve(directory,'polygon-rings-result-map.png')});persist('map_verified')
  const graphRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/meta/lineage/graph'&&u.searchParams.get('item_id')===String(output.item_id)&&u.searchParams.get('direction')==='both'})
  await frame.getByRole('tab',{name:'血缘',exact:true}).click()
  const graphResponse=await graphRead,graphAPI=await browserAPIFromResponse(graphResponse,process.env.GATEWAY_URL,identity)
  try{raw.graph=await json(graphResponse,'rings actual lineage tab')}finally{await graphAPI.dispose()}
  assertRingsLineage(raw.graph,evidence);persist('graph_observed')
  const viewer=frame.locator('.lineage-viewer'),canvas=viewer.locator('.lineage-canvas canvas').first();await expect(canvas).toBeVisible()
  await expect.poll(async()=>{
    raw.lineage_rendered=await viewer.evaluate(element=>{const ref=element.__vueParentComponent?.setupState?.graphInstance,graph=ref?.value||ref;return graph?{nodes:graph.getNodes().map(i=>i.getModel()._node),edges:graph.getEdges().map(i=>i.getModel()._edge)}:null})
    return raw.lineage_rendered?.edges?.length||0
  }).toBe(1)
  assertRingsLineage({...raw.graph,edges:raw.lineage_rendered.edges},evidence)
  await expect.poll(async()=>{raw.lineage_canvas=await lineageCanvasSnapshot(canvas);return [evidence.sources[0].name,output.name].every(name=>raw.lineage_canvas.rows.some(r=>r.text.includes(name)))}).toBe(true)
  expect(raw.lineage_canvas.paths.length).toBeGreaterThanOrEqual(1);persist('graph_rendered')
  raw.failures=[]
  for(const failure of evidence.failures){
    const failureRead=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${failure.execution_id}`)
    await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${failure.execution_id}`)
    const response=await failureRead,api=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity),record={key:failure.key,execution_id:failure.execution_id};raw.failures.push(record)
    try{
      record.execution=await json(response,'rings failure');expect(record.execution.execution_id).toBe(failure.execution_id);expect(record.execution.triggered_by).toBe(Number(evidence.principal_id));expect(record.execution.status).toBe('failed');expect(record.execution.outputs||{}).toEqual({})
      record.monitor=await json(await api.get(`/api/v1/monitor/executions/by-execution-id/${failure.execution_id}`),'rings failed Monitor');expect(record.monitor.execution_id).toBe(failure.execution_id);expect(record.monitor.triggered_by).toBe(Number(evidence.principal_id));expect(record.monitor.module).toBe('develop');expect(record.monitor.status).toBe('failed')
      for(const entry of [...evidence.sources,output])expect(await json(await api.get(`/api/v1/meta/items/${entry.item_id||entry.id}`),'unchanged rings item after failure')).toEqual(entry.before)
    }finally{await api.dispose()}
    await expect(frame.getByText('失败',{exact:true})).toBeVisible();await page.screenshot({path:resolve(directory,`polygon-rings-${failure.key}-failed.png`)});persist('failure_'+failure.key+'_verified')
  }
  expect(writes).toEqual([]);persist('complete','passed')
  writeFileSync(resolve(directory,'topology-browser.json'),JSON.stringify({...evidence,schema_version:'addp.polygon-rings-browser/v1',result:'passed'},null,2)+'\n')
})


test('线端修剪独立普通User线与Point双成果及八失败保护',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}'),directory=process.env.ADDP_ONLINE_ARTIFACT_DIR
  expect(evidence.case).toBe('line-trimming')
  await observeLineageCanvas(page)
  await verifyLineEditCase(page,evidence,directory)
})


function assertNodesLineage(graph,evidence) {
  expect(graph.truncated).toBe(false)
  const edges=graph.edges.filter(e=>e.relation_kind==='derive'&&e.status==='active'&&e.evidence?.execution_id===evidence.execution_id)
  expect(edges).toHaveLength(1)
  expect(edges.map(e=>[e.source.item_id,e.target.item_id])).toEqual([[evidence.sources[0].id,evidence.output.item_id]])
}

function nodesPanelExpectation(failure,locale,vertices=false) {
  const en=locale==='en',d=failure.expected_diagnostic,field=d.operator.field
  const reason={
    operator_endpoint_network_invalid:en?'Line parts have internal self-intersections, positive-length overlaps or unsplit intersections; the network is not connected only at both endpoints.':'线部件存在内部自交、正长度重合或未打断的交会，未构成仅在双方端点连接的线网。',
    operator_field_conflict:en?`Source field ${field} conflicts with a fixed operator output field.`:`源图层字段 ${field} 与算子的固定输出字段重名。`,
    operator_input_invalid:en?'The input layer does not meet the operator type, field, geometry or identifier requirements.':'输入图层不符合算子的类型、字段、几何或标识要求。',
    operator_projected_crs_required:en?'Both inputs require known projected CRSs with matching finite positive horizontal linear units.':'两个输入均须具有已知投影坐标系及匹配的有限正水平线性单位。',
  }
  const action={
    operator_endpoint_network_invalid:en?'Explicitly split intersections and resolve self-intersections or overlaps before extracting nodes; no automatic splitting, deduplication or repair is performed.':'先显式打断交会并处理自交或重合，再提取节点；不会自动打断、去重或修复。',
    operator_field_conflict:en?`Manually rename source field ${field} upstream before submitting a new execution; it is not renamed or overwritten automatically.`:`先在上游手动重命名源字段 ${field}，再重新执行；不会自动改名或覆盖。`,
    operator_input_invalid:en?'Check the source layer type, active geometry, fields and identifiers against the operator requirements, then submit a new execution.':'按算子说明检查源图层的类型、活动几何列、字段及标识；修正后重新执行。',
    operator_projected_crs_required:en?'Explicitly reproject both inputs to the same suitable projected CRS before submitting a new execution.':'先显式重投影到符合要求的同一投影坐标系，再重新执行。',
  }
  if(vertices){const profile={"en": {"reasons": {"operator_input_invalid": "Vertex extraction requires valid finite XY Point/LineString/Polygon or Multi types and a unique non-null id_field; GeometryCollection, NULL, EMPTY and empty parts/rings are rejected.", "operator_crs_invalid": "Vertex extraction requires a known two-dimensional geographic or projected CRS; unknown, geocentric, 3D and compound CRS are rejected.", "operator_field_conflict": "Fixed vertex field {field} conflicts with an input field."}, "actions": {"operator_input_invalid": "Prepare the source and id_field explicitly with unique column names and unique non-null non-geometry scalar identifiers; reject Z/M without dropping empty rows. Closing and repeated coordinates are retained separately, without computing network nodes.", "operator_crs_invalid": "Set the correct two-dimensional CRS explicitly first; retain original XY and units without projection, conversion or axis swapping.", "operator_field_conflict": "Rename field {field} manually upstream first to preserve attributes; vertex_id is unique only within this result, without automatic replacement or renaming."}}, "zh-cn": {"reasons": {"operator_input_invalid": "顶点提取要求有效有限二维XY点/线/面或Multi类型及唯一非空来源id_field；GeometryCollection、NULL、EMPTY、空部件/环拒绝。", "operator_crs_invalid": "顶点提取要求已知二维地理或投影CRS，拒绝未知、地心、三维和复合CRS。", "operator_field_conflict": "固定顶点字段 {field} 与输入字段冲突。"}, "actions": {"operator_input_invalid": "请显式整理源图层和id_field，列名唯一、来源为非几何唯一非空标量；拒绝Z/M，不丢弃空行。闭环首尾和重复坐标会分别保留，不计算网络节点。", "operator_crs_invalid": "请先显式设置正确的二维CRS；保持原XY和单位，不自动投影、换单位或交换轴。", "operator_field_conflict": "请先在上游手动重命名字段 {field}，保留原属性；vertex_id仅在本次成果内唯一，不自动覆盖或改名。"}}}[locale];for(const [key,value] of Object.entries(profile.reasons))reason[key]=value.replaceAll('{field}',field||'');for(const [key,value] of Object.entries(profile.actions))action[key]=value.replaceAll('{field}',field||'')}
  expect(Object.hasOwn(reason,d.reason)).toBe(true)
  const values=[reason[d.reason],en?'Execute operation':'执行操作',vertices?(en?'Extract vertices':'顶点提取'):(en?'Extract line nodes':'节点提取'),en?'Node 1 in the workflow definition':'定义中的第 1 个节点',d.operator.parameter?(en?'Source layer (input_gdf)':'源图层（input_gdf）'):'',field,action[d.reason],'develop.workflow.invalid_params',failure.execution_id].filter(Boolean)
  return {reason:reason[d.reason],action:action[d.reason],values,clipboard:values.join('\n'),copyButton:en?'Copy diagnostics':'复制诊断信息'}
}

async function verifyNodesCase(page,evidence,directory) {
  return verifyPointExtractionCase(page,evidence,directory,false)
}

function assertVerticesLineage(graph,evidence) {
  expect(graph.permission_filtered).toBe(false)
  assertNodesLineage(graph,evidence)
  expect(graph.edges.filter(e=>e.relation_kind==='derive'&&e.status==='active')).toHaveLength(1)
}

async function verifyVerticesCase(page,evidence,directory) {
  return verifyPointExtractionCase(page,evidence,directory,true)
}

async function verifyPointExtractionCase(page,evidence,directory,vertices) {
  const prefix=vertices?'vertex-extraction':'line-nodes',schema=vertices?'addp.vertex-extraction':'addp.line-nodes'
  const ports=vertices?['point','line','polygon']:['result','members']
  const assertPointLineage=vertices?assertVerticesLineage:assertNodesLineage
  const failureKeys=vertices?['source_id','part_no','ring_no','vertex_no','vertex_id','collection','crs']:['reverse','parts-overlap','tee','cross','self','field','geometry','id','crs']
  expect(evidence.case).toBe(prefix);expect(evidence.sources).toHaveLength(10)
  expect(evidence.outputs.map(o=>o.port)).toEqual(ports)
  if(vertices){expect(ports.map(p=>evidence.expected[p].length)).toEqual([3,8,20]);for(const port of ports)expect(evidence.expected[port].map(r=>r.properties.vertex_id)).toEqual(Array.from({length:evidence.expected[port].length},(_,i)=>i+1))}
  else{expect(evidence.expected.result).toHaveLength(15);expect(evidence.expected.members).toHaveLength(22);expect(evidence.expected.result.reduce((n,r)=>n+r.properties.degree,0)).toBe(22)}
  const identity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  const raw={schema_version:schema+'-browser-raw/v1',case:evidence.case,run_id:evidence.run_id,execution_id:evidence.execution_id,observations:[]}
  const persist=(stage,result='incomplete')=>writeFileSync(resolve(directory,vertices?'topology-vertices-browser-raw.json':'topology-nodes-browser-raw.json'),JSON.stringify({...raw,stage,result},null,2)+'\n')
  const writes=[]
  page.on('request',request=>{const path=new URL(request.url()).pathname
    if(!['GET','HEAD','OPTIONS'].includes(request.method())&&/^\/api\/v1\/(develop|meta|manager)\//.test(path))writes.push({method:request.method(),path})})
  const read=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,`/develop/executions/${evidence.execution_id}`)
  const response=await read,executionAPI=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
  try{raw.execution=await json(response,'extension success');expect(raw.execution.execution_id).toBe(evidence.execution_id);expect(raw.execution.triggered_by).toBe(Number(evidence.principal_id));expect(raw.execution.status).toBe('success');expect(Object.keys(raw.execution.outputs).sort()).toEqual(ports.map(p=>'save_'+p).sort())}finally{await executionAPI.dispose()}
  const frame=page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功',{exact:true})).toBeVisible()
  for(const output of evidence.outputs)await expect(frame.locator('.workflow-result')).toContainText(output.name)
  await page.screenshot({path:resolve(directory,`${prefix}-execution.png`)});persist('execution_verified')
  for(const [outputIndex,output] of evidence.outputs.entries()){
    const expected=evidence.expected[output.port],fields=Object.keys(expected[0].properties),locator=`${output.locator}&item_id=${output.item_id}`
    const previewRead=page.waitForResponse(r=>{const url=new URL(r.url());return r.request().method()==='GET'&&url.pathname==='/api/v1/manager/preview'&&url.searchParams.get('locator')===locator&&url.searchParams.get('child_name')===output.layer_name})
    await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
    let previewResponse=await previewRead
    if(output.port==='members'||vertices&&expected.length>10){
      const fullRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/manager/preview'&&u.searchParams.get('locator')===locator&&u.searchParams.get('child_name')===output.layer_name&&u.searchParams.get('page')==='1'&&u.searchParams.get('page_size')==='50'})
      await frame.locator('.table-preview .el-pagination__sizes .el-select').click()
      await frame.getByRole('option',{name:/^50(?:\D|$)/}).click();previewResponse=await fullRead
    }
    const api=await browserAPIFromResponse(previewResponse,process.env.GATEWAY_URL,identity)
    const observation={port:output.port,output_item_id:output.item_id,sources:[]};raw.observations.push(observation)
    if(vertices)observation.preview_text=await previewResponse.text()
    try{
      observation.preview=await json(previewResponse,'extension actual preview')
      for(const source of evidence.sources){const item=await json(await api.get(`/api/v1/meta/items/${source.id}`),'unchanged extension source');expect(item).toEqual(source.before);observation.sources.push({item_id:source.id,item})}
      observation.output=await json(await api.get(`/api/v1/meta/items/${output.item_id}`),'extension output Meta');expect(observation.output).toEqual(output.before)
    }finally{await api.dispose()}
    persist(output.port+'_preview_observed')
    const preview=observation.preview,data=preview.data
    expect(preview.preview_type).toBe('table');expect([data.total,data.source_srid,data.source_crs,data.transform_status,data.preview_hint]).toEqual([expected.length,3857,'EPSG:3857','not_transformed','frontend_transform_required'])
    if(output.port==='members')expect([data.page,data.page_size,data.rows.length]).toEqual([1,50,22])
    expect(data.geometry_columns).toEqual([output.geometry_column]);expect(output.fid_column).toBeTruthy()
    expect([...data.columns].sort()).toEqual([output.fid_column,output.geometry_column,...fields].sort())
    expect(data.rows.map(r=>r[output.fid_column])).toEqual(Array.from({length:expected.length},(_,i)=>i+1))
    expect(data.rows.map(r=>({properties:Object.fromEntries(fields.map(key=>[key,r[key]])),geometry:decodePreviewEWKB(r[output.geometry_column])}))).toEqual(expected)
    const table=frame.locator('.table-preview');await expect(table).toBeVisible();await expect(table.locator('.map-placeholder')).toHaveCount(0)
    const renderer=table.locator('.openlayers-map-renderer')
    observation.rendered=await assertProximityPointMap(renderer,expected)
    await page.screenshot({path:resolve(directory,`${prefix}-${output.port}-map.png`)});persist(output.port+'_map_verified')
    await frame.getByRole('tab',{name:'属性',exact:true}).click();await expect(frame.locator('.attributes-tab-pane')).toContainText(output.layer_name)
    await page.screenshot({path:resolve(directory,`${prefix}-${output.port}-attributes.png`)})
    const graphRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/meta/lineage/graph'&&u.searchParams.get('item_id')===String(output.item_id)&&u.searchParams.get('direction')==='both'})
    await frame.getByRole('tab',{name:'血缘',exact:true}).click()
    const graphResponse=await graphRead,graphAPI=await browserAPIFromResponse(graphResponse,process.env.GATEWAY_URL,identity)
    try{observation.graph=await json(graphResponse,'extension actual lineage tab')}finally{await graphAPI.dispose()}
    persist(output.port+'_graph_observed')
    const binding={...evidence,sources:vertices?[evidence.sources[outputIndex]]:evidence.sources.slice(0,1),output}
    assertPointLineage(observation.graph,binding)
    expect(observation.graph.edges.filter(e=>e.relation_kind==='derive'&&e.status==='active')).toHaveLength(1)
    const viewer=frame.locator('.lineage-viewer'),canvas=viewer.locator('.lineage-canvas canvas').first();await expect(canvas).toBeVisible()
    await expect.poll(async()=>{
      observation.lineage_rendered=await viewer.evaluate(element=>{const ref=element.__vueParentComponent?.setupState?.graphInstance,graph=ref?.value||ref;return graph?{nodes:graph.getNodes().map(i=>i.getModel()._node),edges:graph.getEdges().map(i=>i.getModel()._edge)}:null})
      return observation.lineage_rendered?.edges?.length||0
    }).toBe(1)
    assertPointLineage({...observation.graph,edges:observation.lineage_rendered.edges},binding)
    persist(output.port+'_graph_rendered')
    await expect.poll(async()=>{observation.lineage_canvas=await lineageCanvasSnapshot(canvas);return [...binding.sources.map(s=>s.name),output.name].every(name=>observation.lineage_canvas.rows.some(r=>r.text.includes(name)))}).toBe(true)
    expect(observation.lineage_canvas.paths.length).toBeGreaterThanOrEqual(1)
    await page.screenshot({path:resolve(directory,`${prefix}-${output.port}-lineage.png`)});persist(output.port+'_lineage_verified')
  }
  raw.failures=[]
  const failures=evidence.failures
  expect(failures.map(f=>f.key)).toEqual(failureKeys)
  expect(new Set([evidence.execution_id,...failures.map(f=>f.execution_id)]).size).toBe(failureKeys.length+1)
  raw.failures=[]
  await page.context().grantPermissions(['clipboard-read','clipboard-write'],{origin:process.env.CONSOLE_URL})
  for(const failure of failures){
    const record={key:failure.key,execution_id:failure.execution_id,observations:[]};raw.failures.push(record)
    for(const owner of ['develop','monitor']){
      const path=owner==='develop'?`/develop/executions/${failure.execution_id}`:`/monitor/executions?execution_id=${failure.execution_id}`
      const matches=r=>r.request().method()==='GET'&&new URL(r.url()).pathname===(owner==='develop'?`/api/v1/develop/executions/${failure.execution_id}`:`/api/v1/monitor/executions/by-execution-id/${failure.execution_id}/tree`)
      const read=page.waitForResponse(matches);await page.goto(`${process.env.CONSOLE_URL}${path}`)
      const first=await read,initial=await browserAPIFromResponse(first,process.env.GATEWAY_URL,identity);await initial.dispose()
      const panel=frame.locator(owner==='develop'?'.execution-detail-page > .execution-failure':'.execution-detail-content > .execution-failure')
      for(const locale of ['zh-cn','en']){
        await page.locator('.header-language button').click();await page.getByRole('menuitem',{name:locale==='zh-cn'?'中文':'English',exact:true}).click()
        const read=page.waitForResponse(matches);await page.reload();const response=await read,api=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
        let value
        try{value=await json(response,'nodes real failure page')}finally{await api.dispose()}
        const execution=owner==='develop'?value:value.execution
        expect(execution.execution_id).toBe(failure.execution_id);expect(Number.isInteger(execution.triggered_by)).toBe(true)
        expect(execution.triggered_by).toBe(Number(evidence.principal_id));expect(execution.status).toBe('failed')
        expect(execution.outputs||{}).toEqual({});expect(execution.metadata?.lineage_facts||null).toBe(null)
        expect(execution.error_details).toEqual(failure.error_details);expect(execution.error_details.diagnostic).toEqual(failure.expected_diagnostic)
        const expected=nodesPanelExpectation(failure,locale,vertices)
        await expect(panel).toBeVisible();await expect(panel.locator('h4')).toHaveText(expected.reason)
        await expect(panel.locator('dd')).toHaveText(expected.values.slice(1));await expect(panel).toContainText(expected.action);await expect(panel).toContainText(vertices?(locale==='en'?'Extract vertices':'顶点提取'):(locale==='en'?'Extract line nodes':'节点提取'))
        await expect(panel).toContainText(failure.execution_id);await expect(panel).toContainText('develop.workflow.invalid_params')
        await expect(panel).toContainText(locale==='en'?'Node 1 in the workflow definition':'定义中的第 1 个节点')
        await expect(panel).not.toContainText(/source_shape|feature_key|完整属性|route_key|节点业务几何|Traceback|site-packages|\/tmp\//)
        await page.evaluate(()=>navigator.clipboard.writeText('NODES_CLIPBOARD_NOT_WRITTEN'))
        await panel.getByRole('button',{name:expected.copyButton,exact:true}).click()
        await expect.poll(()=>page.evaluate(()=>navigator.clipboard.readText())).toBe(expected.clipboard)
        const clipboard=await page.evaluate(()=>navigator.clipboard.readText())
        const screenshot=`${prefix}-${failure.key}-${owner}-${locale}.png`;await page.screenshot({path:resolve(directory,screenshot)})
        record.observations.push({owner,locale,execution:value,clipboard,screenshot})
      }
    }
    persist('failure_'+failure.key+'_verified')
  }
  expect(writes).toEqual([]);persist('complete','passed')
  writeFileSync(resolve(directory,'topology-browser.json'),JSON.stringify({...evidence,schema_version:schema+'-browser/v1',result:'passed'},null,2)+'\n')
}

test('节点提取独立普通User节点与来源明细双Point及九失败保护',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}');await observeLineageCanvas(page)
  await verifyNodesCase(page,evidence,process.env.ADDP_ONLINE_ARTIFACT_DIR)
})

test('NFS GeoPackage 同一普通用户撤权血缘', async ({ page }) => {
  const binding = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE)
  const directory = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const { source, output } = binding
  const report = { binding, result: 'incomplete', observations: [] }
  const persist = () => writeFileSync(resolve(directory, 'topology-lineage-permissions-browser.json'), JSON.stringify(report, null, 2))
  const waitForGraph = depth => page.waitForResponse(response => {
    const url = new URL(response.url())
    return response.request().method() === 'GET' && url.pathname === '/api/v1/meta/lineage/graph' &&
      url.searchParams.get('item_id') === String(output.item_id) && url.searchParams.get('direction') === 'both' &&
      url.searchParams.get('depth') === String(depth)
  })
  const iframe = page.locator('iframe[data-testid="module-iframe"]')
  const frame = iframe.contentFrame()
  const viewer = frame.locator('.lineage-viewer')
  const canvas = viewer.locator('.lineage-canvas canvas').first()
  const marker = viewer.getByTestId('lineage-permission-filtered')
  const summary = viewer.locator('.lineage-summary')
  const changeDepth = async depth => {
    const response = waitForGraph(depth)
    await viewer.locator('.lineage-depth .el-select__wrapper').click()
    await frame.getByRole('option', { name: `${depth} 层`, exact: true }).click()
    return response
  }
  const capture = async (phase, response) => {
    const observation = { phase, status: response.status(), payload: await response.json() }
    report.observations.push(observation)
    persist()
    return observation.payload
  }
  const issuer = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: {
    Authorization: `Bearer ${process.env.ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN}`
  } })
  const revoke = async grantId => {
    const path = `/api/v1/system/engines/${binding.engine_id}/access_grants/${grantId}/revoke`
    const response = await issuer.post(path, { data: { reason: 'Disposable Online metadata cleanup' } })
    const payload = await json(response, 'withdraw exact metadata Grant')
    report.observations.push({ phase: 'revoke', grant_id: grantId, status: response.status(), payload })
    persist()
  }
  try {
    await observeLineageCanvas(page)
    const beforeRead = waitForGraph(2)
    const redirect = `/manager/data-explorer?locator=${encodeURIComponent(output.locator)}&tab=lineage`
    await login(page, process.env.ADDP_ONLINE_READ_USER_USERNAME, process.env.ADDP_ONLINE_READ_USER_PASSWORD, redirect)
    const before = await beforeRead
    const graph = await capture('authorized', before)
    const api = await browserAPIFromResponse(before, process.env.GATEWAY_URL, {
      principalID: binding.principal_id, tenantID: binding.tenant_id, permissions: binding.permissions_verified
    })
    await api.dispose()
    expect(graph.permission_filtered).toBe(false)
    assertNodesLineage(graph, { ...binding, sources: [source] })
    await expect(marker).toHaveCount(0)
    await expect(summary).toContainText('2 个节点 · 1 条关系')
    await expect(canvas).toBeVisible()
    await expect.poll(async () => {
      const { rows, paths } = await lineageCanvasSnapshot(canvas)
      return paths.length > 0 && [source.name, output.name].every(name => rows.some(row => row.text.includes(name)))
    }).toBe(true)
    await page.screenshot({ path: resolve(directory, 'topology-lineage-authorized.png') })
    const originalDocument = before.request().frame()
    const navigations = []
    page.on('framenavigated', frame => {
      if (frame === originalDocument || frame === page.mainFrame()) navigations.push(frame)
    })
    const sameSessionAndDocument = response => {
      expect(navigations).toHaveLength(0)
      expect(response.request().frame() === originalDocument).toBe(true)
      expect(response.request().headers().authorization === before.request().headers().authorization).toBe(true)
    }

    await revoke(source.grant_id)
    const after = await changeDepth(3)
    const filtered = await capture('source_revoked', after)
    expect(after.status()).toBe(200)
    sameSessionAndDocument(after)
    expect(filtered.permission_filtered).toBe(true)
    expect(filtered.truncated).toBe(false)
    expect(filtered.nodes.map(node => node.item_id)).toEqual([output.item_id])
    expect(filtered.nodes.map(node => [node.hidden_upstream_count, node.hidden_downstream_count])).toEqual([[0, 0]])
    expect(filtered.edges).toEqual([])
    expect(JSON.stringify(filtered)).not.toContain(source.name)
    expect(JSON.stringify(filtered)).not.toContain(binding.execution_id)
    await expect(marker).toHaveText('当前无可显示的上下游关系，部分关系因数据权限限制已隐藏。')
    await expect(summary).toContainText('1 个节点 · 0 条关系')
    await expect.poll(async () => {
      const { rows } = await lineageCanvasSnapshot(canvas)
      return rows.some(row => row.text.includes(output.name)) && rows.every(row => !row.text.includes(source.name))
    }).toBe(true)
    await page.screenshot({ path: resolve(directory, 'topology-lineage-source-revoked.png') })

    await revoke(output.grant_id)
    const denied = await changeDepth(5)
    const error = await capture('root_revoked', denied)
    expect(denied.status()).toBe(404)
    sameSessionAndDocument(denied)
    expect(error.error_code).toBe('lineage_subject_not_found')
    await expect(frame.locator('.lineage-tab-pane .el-alert')).toContainText('加载数据项血缘失败')
    await expect(marker).toHaveCount(0)
    await expect(summary).toContainText('0 个节点 · 0 条关系')
    await expect(canvas).toHaveCount(0)
    await page.screenshot({ path: resolve(directory, 'topology-lineage-root-revoked.png') })
    report.result = 'passed'
    report.same_browser_credential = true
    report.same_manager_frame = true
    persist()
  } finally {
    await issuer.dispose()
  }
})

async function assertOverlayMap(renderer, expected) {
  const fields = Object.keys(expected[0].properties)
  const keys = rows => rows.map(row => JSON.stringify(row)).sort()
  let observed
  await expect.poll(async () => {
    observed = await renderer.evaluate((element, fields) => {
      const value = element.__vueParentComponent?.setupState?.mapInstance, map = value?.value || value
      if (!map || map.getView().getAnimating()) return null
      const geometry = shape => shape.getType() === 'GeometryCollection'
        ? { type: shape.getType(), geometries: shape.getGeometries().map(geometry) }
        : { type: shape.getType(), coordinates: shape.getCoordinates() }
      return { projection: map.getView().getProjection().getCode(), features: map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
        .map(feature => ({ properties: Object.fromEntries(fields.map(field => [field, feature.get(field)])), geometry: geometry(feature.getGeometry()) })) }
    }, fields)
    if (!observed || observed.projection !== 'EPSG:3857') return []
    return keys(observed.features)
  }).toEqual(keys(expected.map(row => ({ properties: row.properties, geometry: webMercatorPreviewGeometry(row.geometry) }))))
  expect(observed.features).toHaveLength(expected.length)
  expect(observed.features.filter(row => row.geometry.type === 'GeometryCollection')).toHaveLength(expected.filter(row=>row.geometry.type==='GeometryCollection').length)
  return observed
}

async function assertOverlayCollectionPixels(renderer) {
  await renderer.evaluate(element => {
    const value = element.__vueParentComponent.setupState.mapInstance, map = value?.value || value
    const collection = map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || []).find(feature => feature.getGeometry().getType() === 'GeometryCollection')
    map.getView().fit(collection.getGeometry().getExtent(), { size: map.getSize(), padding: [32, 32, 32, 32], duration: 0 })
    map.renderSync()
  })
  let pixels
  await expect.poll(async () => {
    pixels = await renderer.evaluate(element => {
      const value = element.__vueParentComponent.setupState.mapInstance, map = value?.value || value
      if (map.getView().getAnimating()) return {}
      const layer = map.getLayers().getArray().find(layer => layer.getSource?.()?.getFeatures?.().some(feature => feature.getGeometry().getType() === 'GeometryCollection'))
      const feature = layer.getSource().getFeatures().find(feature => feature.getGeometry().getType() === 'GeometryCollection')
      const geometries = feature.getGeometry().getGeometries(), context = layer.getRenderer()?.context
      if (!context || !layer.getVisible() || layer.getOpacity() <= 0) return {}
      const rectangle = context.canvas.getBoundingClientRect(), viewport = map.getViewport().getBoundingClientRect()
      const evidence = { members: [] }
      for (const geometry of geometries) {
        const type=geometry.getType(),kind={Point:'point',LineString:'line',Polygon:'polygon'}[type]
        if(!kind)throw new Error('mixed collection pixel probe requires the frozen point/line/polygon members')
        const coordinate=type==='Point'?geometry.getCoordinates():type==='LineString'?geometry.getCoordinateAt(.5):geometry.getInteriorPoint().getCoordinates().slice(0,2)
        const position = map.getPixelFromCoordinate(coordinate)
        const hit = map.forEachFeatureAtPixel(position, candidate => candidate === feature ? true : undefined, { layerFilter: candidate => candidate === layer, hitTolerance: 0 })
        if (hit !== true) { evidence[kind] = 0; evidence.members.push({kind,pixels:0}); continue }
        const x = Math.round((viewport.left + position[0] - rectangle.left) * context.canvas.width / rectangle.width)
        const y = Math.round((viewport.top + position[1] - rectangle.top) * context.canvas.height / rectangle.height)
        const data = context.getImageData(x - 6, y - 6, 13, 13).data
        let count = 0
        for (let i = 3; i < data.length; i += 4) if (data[i] > 0) count++
        evidence[kind] = Math.min(evidence[kind] ?? count,count)
        evidence.members.push({kind,pixels:count})
      }
      return evidence
    })
    return ['point', 'line', 'polygon'].every(kind => Number.isInteger(pixels[kind]) && pixels[kind] > 10) && pixels.members.every(member=>Number.isInteger(member.pixels)&&member.pixels>10)
  }).toBe(true)
  return pixels
}

const mixedOverlayCases={
  'intersection-overlay':{schema:'addp.intersection-overlay',sourceCount:4,resultCount:16,prefix:'intersection',raw:'topology-overlay-browser-raw.json'},
  'geometry-difference':{schema:'addp.geometry-difference',sourceCount:5,resultCount:9,prefix:'difference',raw:'topology-difference-browser-raw.json'},
}

async function verifyOverlayCase(page){return verifyMixedOverlayCase(page,'intersection-overlay')}
async function verifyDifferenceCase(page){return verifyMixedOverlayCase(page,'geometry-difference')}

async function verifyMixedOverlayCase(page,caseName) {
  const config=mixedOverlayCases[caseName]
  const evidence = JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE || '{}'), directory = process.env.ADDP_ONLINE_ARTIFACT_DIR
  await observeLineageCanvas(page)
  expect(evidence.case).toBe(caseName); expect(evidence.sources).toHaveLength(config.sourceCount); expect(evidence.outputs).toHaveLength(1)
  expect(evidence.expected.result).toHaveLength(config.resultCount)
  const identity = { principalID: evidence.principal_id, tenantID: evidence.tenant_id, permissions: evidence.permissions_verified }
  const raw = { schema_version: config.schema+'-browser-raw/v1', case: evidence.case, run_id: evidence.run_id, execution_id: evidence.execution_id, observations: [] }
  const persist = (stage, result = 'incomplete') => writeFileSync(resolve(directory, config.raw), JSON.stringify({ ...raw, stage, result }, null, 2) + '\n')
  const writes = []
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method()) && /^\/api\/v1\/(develop|meta|manager)\//.test(path)) writes.push({ method: request.method(), path })
  })
  const read = page.waitForResponse(r => r.request().method() === 'GET' && new URL(r.url()).pathname === `/api/v1/develop/executions/${evidence.execution_id}`)
  await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, `/develop/executions/${evidence.execution_id}`)
  const response = await read, bound = await browserAPIFromResponse(response, process.env.GATEWAY_URL, identity)
  try { raw.execution = await json(response, 'intersection success'); expect(raw.execution.execution_id).toBe(evidence.execution_id); expect(raw.execution.triggered_by).toBe(Number(evidence.principal_id)); expect(raw.execution.status).toBe('success'); expect(Object.keys(raw.execution.outputs)).toEqual(['save_result']) } finally { await bound.dispose() }
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByText('成功', { exact: true })).toBeVisible()
  await expect(frame.locator('.workflow-result')).toContainText(evidence.outputs[0].name)
  await page.screenshot({ path: resolve(directory, config.prefix+'-execution.png') }); persist('execution_verified')
  const output = evidence.outputs[0], expected = evidence.expected.result, fields = Object.keys(expected[0].properties), locator = `${output.locator}&item_id=${output.item_id}`
  const previewRead = page.waitForResponse(r => { const u = new URL(r.url()); return r.request().method() === 'GET' && u.pathname === '/api/v1/manager/preview' && u.searchParams.get('locator') === locator && u.searchParams.get('child_name') === output.layer_name })
  await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
  const previewResponse = await previewRead, api = await browserAPIFromResponse(previewResponse, process.env.GATEWAY_URL, identity)
  const observation = { port: 'result', output_item_id: output.item_id, sources: [] }; raw.observations.push(observation)
  try {
    observation.preview = await json(previewResponse, 'intersection mixed preview')
    for (const source of evidence.sources) { const item = await json(await api.get(`/api/v1/meta/items/${source.id}`), 'unchanged intersection source'); expect(item).toEqual(source.before); observation.sources.push({ item_id: source.id, item }) }
    observation.output = await json(await api.get(`/api/v1/meta/items/${output.item_id}`), 'intersection output Meta'); expect(observation.output).toEqual(output.before)
    observation.graph = await json(await api.get(`/api/v1/meta/lineage/graph?subject_kind=data_item&item_id=${output.item_id}&direction=upstream`), 'intersection original graph')
    assertProximityLineage(observation.graph, { ...evidence, sources: evidence.sources.slice(0, 2), output })
  } finally { await api.dispose() }
  persist('preview_observed')
  const preview = observation.preview, data = preview.data
  expect(preview.preview_type).toBe('table'); expect([data.page, data.rows.length, data.total, data.source_srid, data.source_crs, data.transform_status, data.preview_hint]).toEqual([1, config.resultCount, config.resultCount, 3857, 'EPSG:3857', 'not_transformed', 'frontend_transform_required'])
  expect(data.geometry_columns).toEqual([output.geometry_column]); expect(output.fid_column).toBeTruthy()
  expect([...data.columns].sort()).toEqual([output.fid_column, output.geometry_column, ...fields].sort())
  expect(data.rows.map(r => r[output.fid_column])).toEqual(Array.from({ length: config.resultCount }, (_, i) => i + 1))
  for (const row of data.rows) expect(Object.keys(row).sort()).toEqual([...data.columns].sort())
  observation.decoded_rows = data.rows.map(r => ({ properties: Object.fromEntries(fields.map(key => [key, r[key]])), geometry: decodePreviewEWKB(r[output.geometry_column]) }))
  expect(observation.decoded_rows).toEqual(expected)
  const table = frame.locator('.table-preview'), renderer = table.locator('.openlayers-map-renderer')
  await expect(table).toBeVisible(); await expect(table.locator('.map-placeholder')).toHaveCount(0)
  observation.rendered = await assertOverlayMap(renderer, expected)
  await page.screenshot({ path: resolve(directory, config.prefix+'-map.png') }); persist('map_verified')
  observation.rendered.collection_pixels = await assertOverlayCollectionPixels(renderer)
  await page.screenshot({ path: resolve(directory, config.prefix+'-collection-map.png') }); persist('collection_map_verified')
  await frame.getByRole('tab', { name: '属性', exact: true }).click(); await expect(frame.locator('.attributes-tab-pane')).toContainText(output.layer_name)
  const graphRead = page.waitForResponse(r => { const u = new URL(r.url()); return r.request().method() === 'GET' && u.pathname === '/api/v1/meta/lineage/graph' && u.searchParams.get('item_id') === String(output.item_id) && u.searchParams.get('direction') === 'both' })
  await frame.getByRole('tab', { name: '血缘', exact: true }).click()
  const graphResponse = await graphRead, graphAPI = await browserAPIFromResponse(graphResponse, process.env.GATEWAY_URL, identity)
  try { observation.lineage_page = await json(graphResponse, 'intersection lineage page'); assertProximityLineage(observation.lineage_page, { ...evidence, sources: evidence.sources.slice(0, 2), output }) } finally { await graphAPI.dispose() }
  const viewer = frame.locator('.lineage-viewer'), canvas = viewer.locator('.lineage-canvas canvas').first()
  await expect(canvas).toBeVisible()
  await expect.poll(async () => {
    observation.lineage_rendered = await viewer.evaluate(element => {
      const ref = element.__vueParentComponent?.setupState?.graphInstance, graph = ref?.value || ref
      return graph ? { nodes: graph.getNodes().map(item => item.getModel()._node), edges: graph.getEdges().map(item => item.getModel()._edge) } : null
    })
    return observation.lineage_rendered?.edges?.length || 0
  }).toBe(2)
  await expect.poll(async () => {
    observation.lineage_canvas = await lineageCanvasSnapshot(canvas)
    return [...evidence.sources.slice(0, 2).map(source => source.name), output.name].every(name => observation.lineage_canvas.rows.some(row => row.text.includes(name)))
  }).toBe(true)
  expect(observation.lineage_canvas.paths.length).toBeGreaterThanOrEqual(2)
  assertProximityLineage({ ...observation.lineage_page, edges: observation.lineage_rendered.edges }, { ...evidence, sources: evidence.sources.slice(0, 2), output })
  await page.screenshot({ path: resolve(directory, config.prefix+'-lineage.png') }); persist('lineage_verified')
  const observations = await verifyInputDiagnosticCase(page, caseName, { ...evidence, executions: evidence.failures.map(f => ({ ...f, execution: { error_details: f.error_details } })) })
  raw.failures = evidence.failures.map(f => ({ key: f.key, execution_id: f.execution_id, observations: observations.filter(v => v.key === f.key) }))
  expect(writes).toEqual([]); raw.publication_requests = writes
  persist('complete', 'passed')
  writeFileSync(resolve(directory, 'topology-browser.json'), JSON.stringify({ ...evidence, schema_version: config.schema+'-browser/v1', result: 'passed' }, null, 2) + '\n')
}

test('相交叠加独立普通User完整混合交集及四失败保护', async ({ page }) => verifyOverlayCase(page))

async function assertDissolveMap(renderer, expected) {
  const fields = Object.keys(expected[0].properties)
  const keys = rows => rows.map(row => JSON.stringify(row)).sort()
  let observed
  await expect.poll(async () => {
    observed = await renderer.evaluate((element, fields) => {
      const value = element.__vueParentComponent?.setupState?.mapInstance, map = value?.value || value
      if (!map || map.getView().getAnimating()) return null
      const geometry = shape => shape.getType() === 'GeometryCollection'
        ? { type: shape.getType(), geometries: shape.getGeometries().map(geometry) }
        : { type: shape.getType(), coordinates: shape.getCoordinates() }
      return { projection: map.getView().getProjection().getCode(), features: map.getLayers().getArray().flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
        .map(feature => ({ properties: Object.fromEntries(fields.map(field => [field, feature.get(field)])), geometry: geometry(feature.getGeometry()) })) }
    }, fields)
    if (!observed || observed.projection !== 'EPSG:3857') return []
    return keys(observed.features)
  }).toEqual(keys(expected.map(row => ({ properties: row.properties, geometry: webMercatorPreviewGeometry(row.geometry) }))))
  expect(observed.features).toHaveLength(expected.length)
  let pixels
  await expect.poll(async () => {
    pixels=await renderer.locator('canvas').evaluateAll(canvases=>canvases.reduce((count,canvas)=>{
      const data=canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data
      for(let i=0;i<data.length;i+=4)if(data[i+3]>0&&data[i+1]>data[i]*1.1&&data[i+1]>data[i+2]*1.1)count++
      return count
    },0))
    return pixels
  }).toBeGreaterThan(30)
  observed.painted_pixels=pixels
  return observed
}

function assertDissolveLineage(graph,evidence,output) {
  expect(graph.truncated).toBe(false)
  const edges=graph.edges.filter(edge=>edge.relation_kind==='derive'&&edge.status==='active'&&edge.evidence?.execution_id===output.execution_id)
  expect(edges).toHaveLength(1)
  expect(edges.map(edge=>[edge.source.item_id,edge.target.item_id])).toEqual([[evidence.sources[0].id,output.item_id]])
  for(const edge of edges){expect(Number.isInteger(edge.source.item_id)).toBe(true);expect(Number.isInteger(edge.target.item_id)).toBe(true)}
}

async function verifyDissolveCase(page) {
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}'),directory=process.env.ADDP_ONLINE_ARTIFACT_DIR
  expect(evidence.case).toBe('polygon-dissolve');expect(evidence.sources).toHaveLength(5);expect(evidence.successes.map(s=>s.variant)).toEqual(['grouped','global']);expect(evidence.outputs).toHaveLength(4)
  await observeLineageCanvas(page)
  const identity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  const raw={schema_version:'addp.polygon-dissolve-browser-raw/v1',case:evidence.case,run_id:evidence.run_id,successes:[],observations:[]}
  const persist=(stage,result='incomplete')=>writeFileSync(resolve(directory,'topology-dissolve-browser-raw.json'),JSON.stringify({...raw,stage,result},null,2)+'\n')
  const writes=[];page.on('request',request=>{const path=new URL(request.url()).pathname;if(!['GET','HEAD','OPTIONS'].includes(request.method())&&/^\/api\/v1\/(develop|meta|manager)\//.test(path))writes.push({method:request.method(),path})})
  const frame=page.frameLocator('iframe[data-testid="module-iframe"]');let loggedIn=false
  for(const success of evidence.successes){
    const read=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname===`/api/v1/develop/executions/${success.execution_id}`)
    if(!loggedIn){await login(page,process.env.ADDP_ONLINE_TEST_USER_USERNAME,process.env.ADDP_ONLINE_TEST_USER_PASSWORD,`/develop/executions/${success.execution_id}`);loggedIn=true}
    else await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${success.execution_id}`)
    const response=await read,api=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
    let execution;try{execution=await json(response,'dissolve success');expect(execution).toEqual(success.execution)}finally{await api.dispose()}
    raw.successes.push({variant:success.variant,execution_id:success.execution_id,execution})
    await expect(frame.getByText('成功',{exact:true})).toBeVisible()
    for(const output of success.outputs)await expect(frame.locator('.workflow-result')).toContainText(output.name)
    await page.screenshot({path:resolve(directory,`dissolve-${success.variant}-execution.png`)});persist('execution_'+success.variant)
  }
  for(const output of evidence.outputs){
    const expected=output.features,fields=Object.keys(expected[0].properties),locator=`${output.locator}&item_id=${output.item_id}`
    const previewRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/manager/preview'&&u.searchParams.get('locator')===locator&&u.searchParams.get('child_name')===output.layer_name})
    await page.goto(`${process.env.CONSOLE_URL}/manager/data-explorer?locator=${encodeURIComponent(locator)}`)
    const response=await previewRead,api=await browserAPIFromResponse(response,process.env.GATEWAY_URL,identity)
    const observation={name:output.name,port:output.port,execution_id:output.execution_id,output_item_id:output.item_id,sources:[]};raw.observations.push(observation)
    try{
      observation.preview=await json(response,'dissolve preview')
      for(const source of evidence.sources){const item=await json(await api.get(`/api/v1/meta/items/${source.id}`),'dissolve unchanged source');expect(item).toEqual(source.before);observation.sources.push({item_id:source.id,item})}
      observation.output=await json(await api.get(`/api/v1/meta/items/${output.item_id}`),'dissolve output');expect(observation.output).toEqual(output.before)
      observation.graph=await json(await api.get(`/api/v1/meta/lineage/graph?subject_kind=data_item&item_id=${output.item_id}&direction=upstream`),'dissolve graph');assertDissolveLineage(observation.graph,evidence,output)
    }finally{await api.dispose()}
    const data=observation.preview.data
    expect(observation.preview.preview_type).toBe('table');expect([data.page,data.total,data.rows.length,data.source_srid,data.source_crs,data.transform_status,data.preview_hint]).toEqual([1,expected.length,expected.length,3857,'EPSG:3857','not_transformed','frontend_transform_required'])
    expect(data.geometry_columns).toEqual([output.geometry_column]);expect([...data.columns].sort()).toEqual([output.fid_column,output.geometry_column,...fields].sort())
    expect(data.rows.map(row=>row[output.fid_column])).toEqual(expected.map((_,i)=>i+1))
    for(const row of data.rows)expect(Object.keys(row).sort()).toEqual([...data.columns].sort())
    observation.decoded_rows=data.rows.map(row=>({properties:Object.fromEntries(fields.map(key=>[key,row[key]])),geometry:decodePreviewEWKB(row[output.geometry_column])}))
    expect(observation.decoded_rows).toEqual(expected)
    const table=frame.locator('.table-preview'),renderer=table.locator('.openlayers-map-renderer');await expect(table).toBeVisible();await expect(table.locator('.map-placeholder')).toHaveCount(0)
    observation.rendered=await assertDissolveMap(renderer,expected)
    for(const field of fields)await expect(table.locator('thead')).toContainText(field)
    // A click probes the actual popup; its property rows must preserve the original business geometry field.
    const popupProperties=await renderer.evaluate((element,fields)=>{
      const value=element.__vueParentComponent.setupState.mapInstance,map=value?.value||value,layer=map.getLayers().getArray().find(l=>l.getSource?.()?.getFeatures?.().length)
      const geometry=layer.getSource().getFeatures()[0].getGeometry(),shape=geometry.getType()==='MultiPolygon'?geometry.getPolygon(0):geometry
      const coordinate=shape.getInteriorPoint().getCoordinates().slice(0,2),pixel=map.getPixelFromCoordinate(coordinate),feature=map.forEachFeatureAtPixel(pixel,(f,l)=>l===layer?f:undefined)
      if(!feature)throw new Error('dissolve popup has no actual feature hit')
      const properties=Object.fromEntries(fields.map(f=>[f,feature.get(f)]));map.dispatchEvent({type:'singleclick',coordinate,pixel});return properties
    },fields)
    expect(expected.some(row=>JSON.stringify(row.properties)===JSON.stringify(popupProperties))).toBe(true)
    const popup=renderer.locator('.map-popup');await expect(popup).toBeVisible()
    for(const field of fields){const row=popup.locator('.attribute-item').filter({has:frame.locator('.attr-key').filter({hasText:new RegExp('^'+field+':$')})});await expect(row).toHaveCount(1);await expect(row.locator('.attr-value')).toHaveText(popupProperties[field]===null?'空值':String(popupProperties[field]))}
    observation.popup={properties:popupProperties,rows:await popup.locator('.attribute-item').evaluateAll(rows=>rows.map(row=>({key:row.querySelector('.attr-key').textContent,value:row.querySelector('.attr-value').textContent}))) }
    await page.screenshot({path:resolve(directory,`dissolve-${output.name}-map.png`)});persist('map_'+output.name)
    await frame.getByRole('tab',{name:'属性',exact:true}).click();await expect(frame.locator('.attributes-tab-pane')).toContainText(output.layer_name)
    const graphRead=page.waitForResponse(r=>{const u=new URL(r.url());return r.request().method()==='GET'&&u.pathname==='/api/v1/meta/lineage/graph'&&u.searchParams.get('item_id')===String(output.item_id)&&u.searchParams.get('direction')==='both'})
    await frame.getByRole('tab',{name:'血缘',exact:true}).click()
    const graphResponse=await graphRead,bound=await browserAPIFromResponse(graphResponse,process.env.GATEWAY_URL,identity)
    try{observation.lineage_page=await json(graphResponse,'dissolve lineage page');assertDissolveLineage(observation.lineage_page,evidence,output)}finally{await bound.dispose()}
    const viewer=frame.locator('.lineage-viewer'),canvas=viewer.locator('.lineage-canvas canvas').first();await expect(canvas).toBeVisible()
    await expect.poll(async()=>{
      observation.lineage_rendered=await viewer.evaluate(element=>{const ref=element.__vueParentComponent?.setupState?.graphInstance,graph=ref?.value||ref;return graph?{nodes:graph.getNodes().map(i=>i.getModel()._node),edges:graph.getEdges().map(i=>i.getModel()._edge)}:null})
      return observation.lineage_rendered?.edges?.filter(e=>e.evidence?.execution_id===output.execution_id&&e.target?.item_id===output.item_id).length||0
    }).toBe(1)
    assertDissolveLineage({...observation.lineage_page,edges:observation.lineage_rendered.edges},evidence,output)
    await expect.poll(async()=>{observation.lineage_canvas=await lineageCanvasSnapshot(canvas);return [evidence.sources[0].name,output.name].every(name=>observation.lineage_canvas.rows.some(row=>row.text.includes(name)))}).toBe(true)
    expect(observation.lineage_canvas.paths.length).toBeGreaterThanOrEqual(1)
    await page.screenshot({path:resolve(directory,`dissolve-${output.name}-lineage.png`)});persist('lineage_'+output.name)
  }
  const observations=await verifyInputDiagnosticCase(page,'polygon-dissolve',{...evidence,executions:evidence.failures.map(f=>({...f,execution:{error_details:f.execution.error_details}}))})
  raw.failures=evidence.failures.map(f=>({key:f.key,execution_id:f.execution_id,observations:observations.filter(v=>v.key===f.key)}));expect(writes).toEqual([]);raw.publication_requests=writes
  persist('complete','passed');writeFileSync(resolve(directory,'topology-browser.json'),JSON.stringify({...evidence,schema_version:'addp.polygon-dissolve-browser/v1',result:'passed'},null,2)+'\n')
}

test('融合几何独立普通User分组全局双成果及八失败保护',async({page})=>verifyDissolveCase(page))

test('几何差集独立普通User双源混合成果及四失败保护',async({page})=>verifyDifferenceCase(page))


test('顶点提取独立普通User三Point成果及七失败保护',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}');await observeLineageCanvas(page)
  await verifyVerticesCase(page,evidence,process.env.ADDP_ONLINE_ARTIFACT_DIR)
})
