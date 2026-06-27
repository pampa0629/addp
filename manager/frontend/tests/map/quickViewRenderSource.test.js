import { describe, expect, it } from 'vitest'
import {
  isRasterQuickViewRenderSource,
  isTileQuickViewRenderSource,
  normalizeQuickViewRenderSource,
  shouldLoadBasicPreview
} from '../../src/utils/quickViewRenderSource.js'

describe('quickViewRenderSource', () => {
  it('classifies raster mosaic as raster quick view', () => {
    expect(isRasterQuickViewRenderSource('raster_mosaic_tile')).toBe(true)
    expect(isRasterQuickViewRenderSource(' client_cog_render ')).toBe(true)
    expect(isRasterQuickViewRenderSource('direct_tiff_client')).toBe(true)
    expect(isRasterQuickViewRenderSource('cached_tile')).toBe(false)
  })

  it('classifies vector tile render sources', () => {
    expect(isTileQuickViewRenderSource('cached_tile')).toBe(true)
    expect(isTileQuickViewRenderSource(' realtime_tile ')).toBe(true)
    expect(isTileQuickViewRenderSource('raster_mosaic_tile')).toBe(false)
  })

  it('loads basic preview only when map quick view is not already usable', () => {
    expect(shouldLoadBasicPreview('map_quick_view', { can_use_quick_view: true })).toBe(false)
    expect(shouldLoadBasicPreview('basic_preview', { can_use_quick_view: true })).toBe(true)
    expect(shouldLoadBasicPreview('map_quick_view', { can_use_quick_view: false })).toBe(true)
    expect(shouldLoadBasicPreview('map_quick_view', null)).toBe(true)
  })

  it('normalizes empty render source values', () => {
    expect(normalizeQuickViewRenderSource(null)).toBe('')
    expect(normalizeQuickViewRenderSource(' raster_mosaic_tile ')).toBe('raster_mosaic_tile')
  })
})
