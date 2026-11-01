import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import GeoJSON from 'ol/format/GeoJSON.js'
import RenderFeature from 'ol/render/Feature.js'
import { readMapFeatures, mapFeatureOriginal, mapFeatureProperties, cloneMapFeature } from '../../../../common-frontend/map/src/utils/openLayersFeatures.js'
import { createThematicFeatureStyle } from '../../../../common-frontend/map/src/utils/mapStyles.js'
import { formatFeatureProperties } from '../../../../common-frontend/map/src/utils/mapFormatters.js'

const geometry = { type: 'LineString', coordinates: [[10, 20], [11, 21]] }
const properties = { id: 0, geometry: '<business>', geom: null, rowData: false, originalFeature: 9,
  __addpGeometry: 'occupied', __addpGeometry_: 'also occupied' }

describe('shared map geometry and business attributes', () => {
  it('isolates arbitrary keys and retains actual geometry, id, values, source and Vue identity', () => {
    const original = { type: 'Feature', id: 0, geometry, properties }
    const before = structuredClone(original)
    const [feature] = readMapFeatures([original])
    expect(feature.getId()).toBe(0)
    expect(feature.getGeometry().getCoordinates()).toEqual(geometry.coordinates)
    expect(feature.getGeometryName()).toBe('__addpGeometry__')
    expect(feature.get('geometry')).toBe('<business>')
    expect(mapFeatureProperties(feature)).toEqual(properties)
    expect(mapFeatureOriginal(reactive(feature))).toBe(original)
    expect(cloneMapFeature(feature).getGeometry().getCoordinates()).toEqual(geometry.coordinates)
    expect(mapFeatureProperties(cloneMapFeature(feature))).toEqual(properties)
    expect(original).toEqual(before)
  })

  it('does not reserve geometry even when its business value looks like geometry', () => {
    for (const value of [null, false, 0, { type: 'Point', coordinates: [99, 88] }]) {
      const [feature] = readMapFeatures([{ type: 'Feature', geometry, properties: { geometry: value } }])
      expect(feature.getGeometry().getType()).toBe('LineString')
      expect(mapFeatureProperties(feature)).toEqual({ geometry: value })
    }
  })

  it('styles business geometry values and formats them without losing the real geometry type', () => {
    const [feature] = readMapFeatures([{ type: 'Feature', geometry, properties }])
    vi.stubGlobal('document', { documentElement: {} })
    const readTheme = vi.fn(() => '#409eff')
    vi.stubGlobal('getComputedStyle', () => ({ getPropertyValue: readTheme }))
    try {
      const context = { valid: true, mode: 'categorical', field: 'geometry', categories: ['other', '<business>', 'third'], entries: [1, 2, 3] }
      expect(createThematicFeatureStyle(feature, context).getStroke().getWidth()).toBe(3)
      expect(readTheme).toHaveBeenCalledWith('--el-color-primary-light-5')
      const html = formatFeatureProperties(mapFeatureProperties(feature), {
        geometryType: feature.getGeometry().getType(), primaryField: 'geometry', fields: ['rowData', 'originalFeature', 'geom'],
      })
      expect(html).toContain('&lt;business&gt;')
      expect(html).toContain('LineString')
      expect(html).toContain('rowData:')
      expect(html).toContain('originalFeature:')
      expect(html).toContain('geom:')
    } finally { vi.unstubAllGlobals() }
  })

  it('extracts native Feature and MVT RenderFeature properties by their actual geometry interface', () => {
    const native = new GeoJSON().readFeature({ type: 'Feature', geometry, properties: { geom: 'business' } })
    expect(mapFeatureProperties(native)).toEqual({ geom: 'business' })
    const tile = new RenderFeature('LineString', [10, 20, 11, 21], [4], 2, properties, 0)
    expect(mapFeatureProperties(tile)).toEqual(properties)
    expect(tile.getGeometry().getType()).toBe('LineString')
  })
})
