import { describe, expect, it } from 'vitest'
import {
  rasterGeoTIFFProjectionFromQuickView,
  rasterGeoTIFFSourceOptions,
  rasterWebMercatorExtent
} from '../../src/utils/rasterGeoTIFFSourceOptions'

describe('rasterGeoTIFFSourceOptions', () => {
  it('fits a declared UTM range around its true longitude rather than the default map center', () => {
    const extent = rasterWebMercatorExtent({ extent: [500000, 3097440, 502560, 3100000], extent_srid: 32650 })
    expect(extent[0]).toBeCloseTo(13024380.422813, 3) // UTM 50N central meridian is 117 degrees.
    expect(extent[1]).toBeGreaterThan(3200000)
    expect(extent[1]).toBeLessThan(3300000)
    expect(extent[2] - extent[0]).toBeGreaterThan(2800)
    expect(extent[2] - extent[0]).toBeLessThan(3000)
    expect(extent[3] - extent[1]).toBeGreaterThan(2800)
    expect(extent[3] - extent[1]).toBeLessThan(3000)
  })

  it('preserves Web Mercator ranges and refuses missing, unknown or invalid extent CRS facts', () => {
    expect(rasterWebMercatorExtent({ extent: [100, 200, 300, 400], extent_srid: 3857 })).toEqual([100, 200, 300, 400])
    for (const facts of [
      { extent: [113, 27, 114, 28] },
      { extent: [113, 27, 114, 28], source_srid: 4326 },
      { extent: [0, 0, 1, 1], extent_srid: 999999 },
      { extent: [0, 0, 0, 1], extent_srid: 4326 }
    ]) expect(rasterWebMercatorExtent(facts)).toBeNull()
  })

  it('requires range reads for managed COG quick view artifacts', () => {
    expect(rasterGeoTIFFSourceOptions('client_cog_render', 'token-1')).toEqual({
      allowFullFile: false,
      blockSize: 262144,
      cacheSize: 128,
      headers: {
        Authorization: 'Bearer token-1'
      }
    })
  })

  it('allows full-file reads for direct TIFF browser preview', () => {
    expect(rasterGeoTIFFSourceOptions('direct_tiff_client', '')).toEqual({
      allowFullFile: true,
      blockSize: 65536,
      cacheSize: 100,
      headers: {}
    })
  })

  it('uses quick view SRID as explicit GeoTIFF projection when supported', () => {
    expect(rasterGeoTIFFProjectionFromQuickView({ extent_srid: 4326 })).toBe('EPSG:4326')
    expect(rasterGeoTIFFProjectionFromQuickView({ source_srid: 3857 })).toBe('EPSG:3857')
    expect(rasterGeoTIFFProjectionFromQuickView({ extent_srid: 4490 })).toBe('')
  })
})
