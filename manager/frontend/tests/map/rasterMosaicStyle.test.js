import { describe, expect, it } from 'vitest'
import {
  DEFAULT_RASTER_MOSAIC_GAMMA,
  normalizeRasterMosaicGamma,
  rasterMosaicTileURLWithStyle
} from '../../src/utils/rasterMosaicStyle'

describe('rasterMosaicStyle', () => {
  it('normalizes gamma', () => {
    expect(normalizeRasterMosaicGamma(0.7)).toBe(0.7)
    expect(normalizeRasterMosaicGamma('bad')).toBe(DEFAULT_RASTER_MOSAIC_GAMMA)
  })

  it('adds style query parameters to raster mosaic tile URL', () => {
    const url = rasterMosaicTileURLWithStyle('/api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?locator=a%2Fb', {
      gamma: 0.65,
      displayMin: 10,
      displayMax: 4200,
      invert: true
    })

    expect(url).toContain('/api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?')
    expect(url).toContain('locator=a%2Fb')
    expect(url).toContain('gamma=0.65')
    expect(url).toContain('display_min=10')
    expect(url).toContain('display_max=4200')
    expect(url).toContain('invert=true')
  })

  it('removes invalid optional range and disabled invert', () => {
    const url = rasterMosaicTileURLWithStyle('/tiles/{z}/{x}/{y}.png?gamma=1&display_min=1&display_max=2&invert=true', {
      gamma: 0.6,
      displayMin: 5,
      displayMax: 3,
      invert: false
    })

    expect(url).toBe('/tiles/{z}/{x}/{y}.png?gamma=0.6')
  })
})
