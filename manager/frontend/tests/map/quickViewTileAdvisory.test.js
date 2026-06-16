import { describe, expect, it } from 'vitest'
import {
  quickViewTileAdvisoryAction,
  quickViewTileAdvisoryMessage,
  quickViewTileAdvisoryTimeoutBudgetMS,
  shouldShowQuickViewTileAdvisoryNotice
} from '../../src/utils/quickViewTileAdvisory'

const t = (key, params = {}) => {
  const messages = {
    'manager.spatialPreview.tileTimeoutCacheRecommendedWithBudget': '超过 {timeout} 秒，生成瓦片缓存',
    'manager.spatialPreview.tileTimeoutCacheRecommended': '生成瓦片缓存',
    'manager.spatialPreview.tileTimeoutOptimizationRecommendedWithBudget': '超过 {timeout} 秒，执行快显优化',
    'manager.spatialPreview.tileTimeoutOptimizationRecommended': '执行快显优化'
  }
  let message = messages[key] || key
  for (const [name, value] of Object.entries(params)) {
    message = message.replace(`{${name}}`, value)
  }
  return message
}

describe('quickViewTileAdvisory', () => {
  it('routes source-transform suppression to quick view optimization when no 3857 target is available', () => {
    const action = quickViewTileAdvisoryAction(
      { retryPolicy: 'suppress_tile', timeoutBudgetMS: '5000' },
      {
        render_facts: { source_srid: 2360 },
        optimization: { available: false }
      }
    )

    expect(action).toBe('quick_view_optimization')
    expect(quickViewTileAdvisoryMessage(t, action, { timeoutBudgetMS: '5000' })).toBe('超过 5 秒，执行快显优化')
  })

  it('routes timeout on available 3857 target to tile cache generation', () => {
    const action = quickViewTileAdvisoryAction(
      { retryPolicy: 'suppress_tile', timeoutBudgetMS: '5000' },
      {
        render_facts: { source_srid: 2360 },
        optimization: {
          available: true,
          target_kind: 'external_3857_materialized_view'
        }
      }
    )

    expect(action).toBe('tile_cache_generation')
    expect(quickViewTileAdvisoryMessage(t, action, { timeoutBudgetMS: '5000' })).toBe('超过 5 秒，生成瓦片缓存')
  })

  it('uses realtime tile timeout budget as message fallback', () => {
    expect(quickViewTileAdvisoryTimeoutBudgetMS({}, { realtime_tile: { timeout_budget_ms: 6000 } })).toBe(6000)
  })

  it('throttles repeated advisory notices by action and scenario', () => {
    const advisory = { recommendation: 'tile_cache_generation', retryPolicy: 'ttl', performanceMode: 'ready_3857_target' }
    const quickViewStatus = { optimization: { target_kind: 'external_3857_materialized_view' } }
    const first = shouldShowQuickViewTileAdvisoryNotice({}, 'tile_cache_generation', advisory, quickViewStatus, 1000, 60000)
    const repeated = shouldShowQuickViewTileAdvisoryNotice(first.lastNotice, 'tile_cache_generation', advisory, quickViewStatus, 30000, 60000)
    const cooledDown = shouldShowQuickViewTileAdvisoryNotice(first.lastNotice, 'tile_cache_generation', advisory, quickViewStatus, 62000, 60000)

    expect(first.show).toBe(true)
    expect(repeated.show).toBe(false)
    expect(cooledDown.show).toBe(true)
  })
})
