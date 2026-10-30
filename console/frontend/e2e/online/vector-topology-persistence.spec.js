import { expect, test } from '@playwright/test'
import { observeLineageCanvas, lineageCanvasSnapshot } from '../../../../common-frontend/basic/tests/fixtures/lineageCanvas.js'
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
  return features.map(row => JSON.stringify({ properties:row.properties,
    coordinates:row.geometry.type==='Polygon' ? row.geometry.coordinates.map(ring=>ring.map(webMercatorPreviewCoordinate)) : row.geometry.coordinates.map(webMercatorPreviewCoordinate) })).sort()
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
  await expect(panel).not.toContainText(/PRIVATE_DIAGNOSTIC|Traceback|site-packages|\/tmp\/|99\.0/)
  return expected
}

test('算子参数诊断独立普通User双语失败页',async({page})=>{
  const evidence=JSON.parse(process.env.ADDP_ONLINE_TOPOLOGY_EVIDENCE||'{}')
  expect(evidence.case).toBe('operator-input-diagnostics')
  expect(evidence.executions.map(run=>run.key)).toEqual(Object.keys(inputDiagnosticText))
  expect(new Set(evidence.executions.map(run=>run.execution_id)).size).toBe(4)
  const expectedIdentity={principalID:evidence.principal_id,tenantID:evidence.tenant_id,permissions:evidence.permissions_verified}
  await page.context().grantPermissions(['clipboard-read','clipboard-write'],{origin:process.env.CONSOLE_URL})
  let loggedIn=false
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
  expect(observations).toHaveLength(16)
  expect(publicationRequests).toEqual([])
  writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,'topology-browser.json'),JSON.stringify({
    ...evidence,schema_version:'addp.operator-input-diagnostics-browser/v1',result:'passed',observations},null,2)+'\n')
})

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
  expect(observed.features).toHaveLength(7)
  expect(new Set(observed.features.map(feature=>JSON.stringify(feature.coordinates))).size).toBe(3)
  let greenPixels=0
  await expect.poll(async()=>greenPixels=await renderer.locator('canvas').evaluateAll(canvases=>canvases.reduce((n,canvas)=>{
    const pixels=canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data
    for(let i=0;i<pixels.length;i+=4)if(pixels[i+3]>0&&pixels[i+1]>pixels[i]*1.1&&pixels[i+1]>pixels[i+2]*1.1)n++
    return n
  },0))).toBeGreaterThan(30)
  return {...observed,green_pixels:greenPixels}
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
