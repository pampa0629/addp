import { describe, expect, it } from 'vitest'
import {
  rasterGeoTIFFProjectionFromQuickView,
  rasterGeoTIFFSourceOptions
} from '../../src/utils/rasterGeoTIFFSourceOptions'

describe('rasterGeoTIFFSourceOptions', () => {
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
