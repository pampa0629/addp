import { describe, expect, it } from 'vitest'
import {
  hasQuickViewAction,
  isRasterQuickViewRenderSource,
  isTileQuickViewRenderSource,
  normalizeQuickViewRenderSource,
  shouldShowQuickViewUnavailableNotice,
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

  it('shows unavailable notices only for source kinds that should surface them', () => {
    expect(shouldShowQuickViewUnavailableNotice({
      source_kind: 'model_3d',
      can_use_quick_view: false,
      unavailable_reason: 'quick view geometry metadata is unavailable'
    })).toBe(false)

    expect(shouldShowQuickViewUnavailableNotice({
      source_kind: 'vector',
      can_use_quick_view: false,
      unavailable_reason: 'quick view geometry metadata is unavailable'
    })).toBe(true)

    expect(shouldShowQuickViewUnavailableNotice({
      source_kind: 'raster',
      can_use_quick_view: false,
      unavailable_reason: 'missing_crs'
    })).toBe(true)
  })

  it('reads backend-provided quick view actions', () => {
    expect(hasQuickViewAction({
      available_actions: ['switch_quick_view', 'generate_model_3d_glb']
    }, 'generate_model_3d_glb')).toBe(true)

    expect(hasQuickViewAction({
      available_actions: ['switch_quick_view']
    }, 'generate_model_3d_glb')).toBe(false)
  })
})
