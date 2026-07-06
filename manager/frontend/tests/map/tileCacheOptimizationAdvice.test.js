import { describe, expect, it } from 'vitest'
import { tileCacheOptimizationAdvice } from '../../src/utils/tileCacheOptimizationAdvice'

describe('tileCacheOptimizationAdvice', () => {
  it('shows Manager ready target as reusable without optimization action', () => {
    expect(tileCacheOptimizationAdvice({
      source_kind: 'table',
      optimization_available: true,
      optimization_target_kind: 'source_schema_materialized_view'
    })).toMatchObject({
      visible: true,
      type: 'success',
      actionVisible: false,
      titleKey: 'manager.tileCache.optimizationTargetReadyTitle'
    })
  })

  it('shows external 3857 target as readonly reusable without optimization action', () => {
    expect(tileCacheOptimizationAdvice({
      source_kind: 'table',
      optimization_available: true,
      optimization_target_kind: 'external_3857_materialized_view'
    })).toMatchObject({
      visible: true,
      type: 'success',
      actionVisible: false,
      titleKey: 'manager.tileCache.externalOptimizationTargetReadyTitle'
    })
  })

  it('routes stale Manager target to vector materialized view action', () => {
    expect(tileCacheOptimizationAdvice({
      source_kind: 'table',
      optimization_available: false,
      optimization_status: 'stale',
      source_srid: 2360
    })).toMatchObject({
      visible: true,
      type: 'warning',
      actionVisible: true,
      titleKey: 'manager.tileCache.optimizationTargetStaleTitle'
    })
  })

  it('routes non-3857 source-transform path to vector materialized view action', () => {
    expect(tileCacheOptimizationAdvice({
      source_kind: 'table',
      optimization_recommended: true,
      source_srid: 2360
    })).toMatchObject({
      visible: true,
      type: 'warning',
      actionVisible: true,
      titleKey: 'manager.tileCache.optimizationRecommendedTitle'
    })
  })

  it('does not recommend vector materialized view for source 3857 without stale target', () => {
    expect(tileCacheOptimizationAdvice({
      source_kind: 'table',
      optimization_recommended: true,
      source_srid: 3857
    })).toMatchObject({
      visible: false,
      actionVisible: true
    })
  })

  it('does not recommend vector materialized view for file sources', () => {
    expect(tileCacheOptimizationAdvice({
      source_kind: 'file',
      optimization_recommended: true,
      optimization_status: 'stale',
      source_srid: 4549
    })).toMatchObject({
      visible: false,
      actionVisible: true
    })
  })
})
