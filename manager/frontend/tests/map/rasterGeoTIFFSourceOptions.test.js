import { describe, expect, it } from 'vitest'
import { rasterGeoTIFFSourceOptions } from '../../src/utils/rasterGeoTIFFSourceOptions'

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
})
