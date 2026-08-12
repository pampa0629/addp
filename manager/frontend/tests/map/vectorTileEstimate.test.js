import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import {
  calculateTileRangeEstimate,
  isZoomAboveRecommendation
} from '../../src/utils/vectorTileEstimate'
import {
  hasRequiredVectorTileSpatialFacts,
  isDeferredExtentDatabaseSource,
  isVectorTilePreviewTarget,
  isVectorTileSourceItem,
  resolveVectorTileZoomRecommendation
} from '../../src/utils/vectorTileSetResource'

const vectorTileSetView = readFileSync(
  new URL('../../src/views/VectorTileSet.vue', import.meta.url),
  'utf8'
)
const routerSource = readFileSync(
  new URL('../../src/router/index.js', import.meta.url),
  'utf8'
)
const layoutSource = readFileSync(
  new URL('../../src/components/Layout.vue', import.meta.url),
  'utf8'
)
const quickViewAPISource = readFileSync(
  new URL('../../src/api/quickView.js', import.meta.url),
  'utf8'
)
const explorerTreeSource = readFileSync(
  new URL('../../src/components/explorer/ExplorerTree.vue', import.meta.url),
  'utf8'
)
const previewPanelSource = readFileSync(
  new URL('../../src/components/explorer/PreviewPanel.vue', import.meta.url),
  'utf8'
)

describe('vectorTileEstimate', () => {
  it('estimates the same farmland range for cache and business tile generation', () => {
    expect(calculateTileRangeEstimate({
      extent: [108.55648171959794, 24.52585476646484, 114.3433679860587, 30.244050172136756],
      extentSRID: 4326,
      minZoom: 4,
      maxZoom: 12
    })).toEqual({ supported: true, tileCount: 6751 })
  })

  it('distinguishes the fixed source recommendation from the editable max zoom', () => {
    expect(isZoomAboveRecommendation(12, 12)).toBe(false)
    expect(isZoomAboveRecommendation(13, 12)).toBe(true)
    expect(isZoomAboveRecommendation(13, 0)).toBe(false)
  })

  it('keeps the source recommendation separate from the editable business zoom range', () => {
    expect(vectorTileSetView).toContain('{{ sourceFacts.recommendedMinZoom }}-{{ sourceFacts.recommendedMaxZoom }}')
    expect(vectorTileSetView).toContain("calculateTileRangeEstimate")
    expect(vectorTileSetView).toContain(":type=\"zoomAboveRecommendation ? 'warning' : 'info'\"")
    expect(vectorTileSetView).not.toContain("t('manager.vectorTileSet.recommendedZoom')\">{{ form.minZoom }}-{{ form.maxZoom }}")
  })

  it('uses the single spatial-tasks route for vector tile tasks', () => {
    expect(routerSource).toContain("path: 'spatial-tasks/vector-tiles'")
    expect(layoutSource).toContain('index="/spatial-tasks/vector-tiles"')
    expect(routerSource).not.toContain('spatial-derivation')
    expect(layoutSource).not.toContain('spatial-derivation')
  })

  it('only exposes vector tile generation for spatial table items', () => {
    const spatialTable = {
      locator: 'addp://engine/1/table/public/roads?item_id=9',
      metadata: { data_type: 'table', spatial: { geometry_columns: ['shape'], primary_geometry_column: 'shape', srid: 4326 } }
    }
    expect(isVectorTileSourceItem(spatialTable)).toBe(true)
    expect(isVectorTileSourceItem({ ...spatialTable, metadata: { data_type: 'media', spatial: { srid: 4326 } } })).toBe(false)
    expect(isVectorTileSourceItem({ ...spatialTable, metadata: { data_type: 'table' } })).toBe(false)
    expect(isVectorTileSourceItem({ ...spatialTable, locator: 'addp://engine/1/table/public/roads' })).toBe(false)
  })

  it('uses confirmed preview facts when the tree item has no spatial metadata', () => {
    expect(isVectorTilePreviewTarget({
      locator: 'addp://engine/8/path/public/farmland?type=table&item_id=55',
      itemID: 55,
      locatorType: 'table',
      geometryColumn: 'geometry',
      geometryColumns: ['geometry']
    })).toBe(true)
    expect(isVectorTilePreviewTarget({
      locator: 'addp://engine/8/path/public/farmland?type=table&item_id=55',
      itemID: 55,
      locatorType: 'table',
      geometryColumns: []
    })).toBe(false)
  })

  it('defers extent only for database sources materialized through FlatGeobuf', () => {
    const oracleSelection = { display: { engine_type: 'oracle' } }
    const postgisSelection = { display: { engine_type: 'postgresql' } }
    const factsWithoutExtent = { geometryColumn: 'SHAPE', sourceSRID: 4326, extent: [], extentSRID: 0 }
    expect(isDeferredExtentDatabaseSource(oracleSelection)).toBe(true)
    expect(hasRequiredVectorTileSpatialFacts(factsWithoutExtent, oracleSelection)).toBe(true)
    expect(hasRequiredVectorTileSpatialFacts(factsWithoutExtent, postgisSelection)).toBe(false)
    expect(vectorTileSetView).toContain("if (form.extent.length === 4 && form.extentSRID > 0)")
  })

  it('uses a bounded default zoom when execution-time extent cannot be estimated yet', () => {
    expect(resolveVectorTileZoomRecommendation({ min_zoom: 3, max_zoom: 18 }, {}, false)).toEqual({ minZoom: 3, maxZoom: 12 })
    expect(resolveVectorTileZoomRecommendation({ min_zoom: 3, max_zoom: 18 }, {}, true)).toEqual({ minZoom: 3, maxZoom: 18 })
  })

  it('opens the same create form from the data preview action with its locator', () => {
    expect(previewPanelSource).toContain('v-if="showVectorTileSetAction"')
    expect(previewPanelSource).toContain("name: 'VectorTileSet'")
    expect(previewPanelSource).toContain("query: { create: '1', locator: target.locator }")
    expect(explorerTreeSource).not.toContain("id: 'vector-tiles'")
    expect(vectorTileSetView).toContain('openCreate(routeState.query.locator)')
    expect(vectorTileSetView).toContain('watch(() => route.query, restoreWorkspaceFromRoute)')
  })

  it('opens TaskProvider create and edit deep links in the business tile set form', () => {
    expect(vectorTileSetView).toContain("const taskID = Number(routeState.query.task_id || 0)")
    expect(vectorTileSetView).toContain('const task = await quickViewAPI.getVectorTileSetTask(taskID)')
    expect(vectorTileSetView).toContain('openEdit(task?.data?.data || task?.data || task)')
    expect(vectorTileSetView).toContain("routeState.query.create === '1'")
    expect(vectorTileSetView).toContain('@click="requestEditTask(row)"')
    expect(quickViewAPISource).toContain('getVectorTileSetTask(id)')
    expect(quickViewAPISource).toContain('request.get(`/manager/vector_tile_set_tasks/${id}`)')
  })

  it('uses one full-width target storage engine picker without a repeated inner label', () => {
    expect(vectorTileSetView).toContain("t('manager.vectorTileSet.target')")
    expect(vectorTileSetView).toContain(':engine-label="\'\'"')
    expect(vectorTileSetView).toContain('class="picker-wrap target-picker"')
  })
})
