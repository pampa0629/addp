import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { calculateTileRangeEstimate, isZoomAboveRecommendation } from '../../src/utils/vectorTileEstimate'
import { hasRequiredVectorTileSpatialFacts, isDeferredExtentDatabaseSource, isVectorTilePreviewTarget, isVectorTileSourceItem, resolveVectorTileZoomRecommendation } from '../../src/utils/vectorTileSetResource'

const editorSource = readFileSync(new URL('../../src/components/tasks/VectorTileSetTaskEditor.vue', import.meta.url), 'utf8')
const derivedTasksSource = readFileSync(new URL('../../src/views/DerivedTasks.vue', import.meta.url), 'utf8')
const routerSource = readFileSync(new URL('../../src/router/index.js', import.meta.url), 'utf8')
const layoutSource = readFileSync(new URL('../../src/components/Layout.vue', import.meta.url), 'utf8')
const apiSource = readFileSync(new URL('../../src/api/quickView.js', import.meta.url), 'utf8')
const previewPanelSource = readFileSync(new URL('../../src/components/explorer/PreviewPanel.vue', import.meta.url), 'utf8')

describe('vectorTileEstimate', () => {
  it('estimates the same farmland range for cache and business tile generation', () => {
    expect(calculateTileRangeEstimate({ extent: [108.55648171959794, 24.52585476646484, 114.3433679860587, 30.244050172136756], extentSRID: 4326, minZoom: 4, maxZoom: 12 })).toEqual({ supported: true, tileCount: 6751 })
  })

  it('distinguishes the fixed source recommendation from the editable max zoom', () => {
    expect(isZoomAboveRecommendation(12, 12)).toBe(false)
    expect(isZoomAboveRecommendation(13, 12)).toBe(true)
    expect(isZoomAboveRecommendation(13, 0)).toBe(false)
  })

  it('keeps source recommendation separate in the typed editor', () => {
    expect(editorSource).toContain('sourceFacts.recommendedMinZoom')
    expect(editorSource).toContain('calculateTileRangeEstimate')
    expect(editorSource).toContain("zoomAboveRecommendation ? 'warning' : 'info'")
  })

  it('uses the single derived-tasks route for vector tile tasks', () => {
    expect(routerSource).toContain("path: 'derived-tasks'")
    expect(layoutSource).toContain('index="/derived-tasks"')
    expect(routerSource).not.toContain('spatial-tasks/vector-tiles')
    expect(layoutSource).not.toContain('spatial-tasks/vector-tiles')
  })

  it('only exposes vector tile generation for spatial table items', () => {
    const spatialTable = { locator: 'addp://engine/1/table/public/roads?item_id=9', metadata: { data_type: 'table', spatial: { geometry_columns: ['shape'], primary_geometry_column: 'shape', srid: 4326 } } }
    expect(isVectorTileSourceItem(spatialTable)).toBe(true)
    expect(isVectorTileSourceItem({ ...spatialTable, metadata: { data_type: 'media', spatial: { srid: 4326 } } })).toBe(false)
    expect(isVectorTileSourceItem({ ...spatialTable, metadata: { data_type: 'table' } })).toBe(false)
  })

  it('uses confirmed preview facts when tree metadata is incomplete', () => {
    expect(isVectorTilePreviewTarget({ locator: 'addp://engine/8/path/public/farmland?type=table&item_id=55', itemID: 55, locatorType: 'table', geometryColumn: 'geometry', geometryColumns: ['geometry'] })).toBe(true)
  })

  it('defers extent only for supported database sources', () => {
    const facts = { geometryColumn: 'SHAPE', sourceSRID: 4326, extent: [], extentSRID: 0 }
    expect(isDeferredExtentDatabaseSource({ display: { engine_type: 'oracle' } })).toBe(true)
    expect(hasRequiredVectorTileSpatialFacts(facts, { display: { engine_type: 'oracle' } })).toBe(true)
    expect(hasRequiredVectorTileSpatialFacts(facts, { display: { engine_type: 'postgresql' } })).toBe(false)
    expect(editorSource).toContain('if (form.extent.length === 4 && form.extentSRID > 0)')
  })

  it('uses a bounded default zoom before extent is known', () => {
    expect(resolveVectorTileZoomRecommendation({ min_zoom: 3, max_zoom: 18 }, {}, false)).toEqual({ minZoom: 3, maxZoom: 12 })
  })

  it('opens the unified spatial task editor from the data preview action', () => {
    expect(previewPanelSource).toContain("name: 'DerivedTasks'")
    expect(previewPanelSource).toContain("task_type: 'vector_tile_set_generation'")
    expect(previewPanelSource).toContain("create: '1'")
    expect(derivedTasksSource).toContain('VectorTileSetTaskEditor')
  })

  it('uses unified task endpoints for typed create and edit', () => {
    expect(apiSource).toContain("request.post('/manager/tasks/vector_tile_set_generation', payload)")
    expect(apiSource).toContain('request.put(`/manager/tasks/vector_tile_set_generation/${id}`, payload)')
  })

  it('uses one full-width target storage picker without a repeated label', () => {
    expect(editorSource).toContain(':engine-label="\'\'"')
    expect(editorSource).toContain('class="picker-wrap"')
  })
})
