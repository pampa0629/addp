import { describe, expect, it } from 'vitest'
import {
  RASTER_QUICK_VIEW_GAODE_BASE_MAP,
  RASTER_QUICK_VIEW_TDT_IMAGE_BASE_MAP,
  RASTER_QUICK_VIEW_TDT_VECTOR_BASE_MAP,
  defaultRasterQuickViewBaseMap,
  isGaodeRasterQuickViewBaseMap,
  isTiandituRasterQuickViewBaseMap,
  rasterQuickViewBaseMapOptions
} from '../../src/utils/rasterQuickViewBaseMap'

describe('rasterQuickViewBaseMap', () => {
  it('prioritizes Tianditu vector/image base maps and keeps Gaode as last fallback', () => {
    const options = rasterQuickViewBaseMapOptions([
      { value: 'amapVector', label: '高德矢量' },
      { value: 'tiandituImage', label: '天地图影像' },
      { value: 'tiandituVector', label: '天地图矢量' }
    ])

    expect(options).toEqual([
      { value: 'tiandituVector', label: '天地图矢量' },
      { value: 'tiandituImage', label: '天地图影像' },
      { value: 'amapVector', label: '高德矢量' },
    ])
    expect(defaultRasterQuickViewBaseMap(options)).toBe(RASTER_QUICK_VIEW_TDT_VECTOR_BASE_MAP)
  })

  it('uses Gaode as fallback when no supported base map is configured', () => {
    expect(rasterQuickViewBaseMapOptions([{ value: 'unsupported', label: '外部底图' }])).toEqual([
      { value: RASTER_QUICK_VIEW_GAODE_BASE_MAP, label: '高德地图 矢量（GCJ-02）' }
    ])
    expect(defaultRasterQuickViewBaseMap([{ value: 'unsupported', label: '外部底图' }])).toBe('amapVector')
  })

  it('identifies Tianditu base maps used by raster quick view', () => {
    expect(isTiandituRasterQuickViewBaseMap(RASTER_QUICK_VIEW_TDT_VECTOR_BASE_MAP)).toBe(true)
    expect(isTiandituRasterQuickViewBaseMap(RASTER_QUICK_VIEW_TDT_IMAGE_BASE_MAP)).toBe(true)
    expect(isTiandituRasterQuickViewBaseMap('amapVector')).toBe(false)
    expect(isGaodeRasterQuickViewBaseMap('amapVector')).toBe(true)
    expect(isGaodeRasterQuickViewBaseMap('tiandituVector')).toBe(false)
  })
})
