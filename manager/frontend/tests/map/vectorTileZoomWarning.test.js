import { describe, expect, it } from 'vitest'
import {
  shouldWarnVectorTileMaxZoom,
  vectorTileMaxZoomWarningKey,
  vectorTileSourceMaxZoom
} from '../../src/utils/vectorTileZoomWarning'

describe('vectorTileZoomWarning', () => {
  it('warns above max zoom only for cached tile rendering', () => {
    expect(shouldWarnVectorTileMaxZoom('cached_tile')).toBe(true)
    expect(shouldWarnVectorTileMaxZoom('realtime_tile')).toBe(false)
    expect(shouldWarnVectorTileMaxZoom('')).toBe(false)
    expect(vectorTileMaxZoomWarningKey('cached_tile')).toBe('manager.vectorTile.zoomTooHighCached')
    expect(vectorTileMaxZoomWarningKey('realtime_tile')).toBe('')
  })

  it('keeps realtime MVT source zoom open beyond recommended max zoom', () => {
    expect(vectorTileSourceMaxZoom('cached_tile', 12)).toBe(12)
    expect(vectorTileSourceMaxZoom('realtime_tile', 12)).toBe(22)
    expect(vectorTileSourceMaxZoom('', 12)).toBe(22)
  })
})
