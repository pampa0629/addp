import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { useTileCacheExecutionStats } from '../../src/composables/useTileCacheExecutionStats'

const translations = {
  'manager.tileCache.generationTarget': '生成目标',
  'manager.tileCache.generationTargetSourceTable': '源表',
  'manager.tileCache.generationTargetVectorMaterializedView': '矢量物化视图目标',
  'manager.tileCache.generationTargetExternal3857': '外部 3857 优化目标',
  'manager.tileCache.tilesTotalEstimate': '预计瓦片',
  'manager.tileCache.tilesProcessed': '已处理瓦片',
  'manager.tileCache.generatedTiles': '生成瓦片',
  'manager.tileCache.emptyTiles': '空瓦片',
  'manager.tileCache.skippedTiles': '跳过瓦片',
  'manager.tileCache.oversizedSkippedTiles': '超大跳过',
  'manager.tileCache.failedTiles': '失败瓦片',
  'manager.tileCache.actualMaxZoom': '实际最大层级',
  'manager.tileCache.totalTileSize': '瓦片总大小',
  'manager.tileCache.maxTileSize': '最大瓦片',
  'manager.tileCache.statsCheckIncomplete': '统计不完整',
  'manager.tileCache.statsCheckInvalid': '统计异常',
  'manager.tileCache.statsCheckMatched': '统计一致：已处理 {processed} = 分类 {classified}',
  'manager.tileCache.statsCheckMismatch': '统计不一致：已处理 {processed}，分类 {classified}'
}

const t = (key, params = {}) => {
  let message = translations[key] || key
  for (const [name, value] of Object.entries(params)) {
    message = message.replace(`{${name}}`, value)
  }
  return message
}

describe('useTileCacheExecutionStats', () => {
  it('shows external 3857 generation target without tile stat warning', () => {
    const metadata = ref({
      tile_generation_target: {
        schema: 'public',
        table: 'dltb_mv3857',
        geom_column: 'geom_3857',
        srid: 3857,
        target_kind: 'external_3857_materialized_view'
      }
    })

    const { executionStatsAvailable, executionStatsCheck, executionStatItems } = useTileCacheExecutionStats({ t, metadata })

    expect(executionStatsAvailable.value).toBe(true)
    expect(executionStatsCheck.value.visible).toBe(false)
    expect(executionStatItems.value).toEqual([
      {
        key: 'tile_generation_target',
        label: '生成目标',
        value: '外部 3857 优化目标: public.dltb_mv3857.geom_3857; SRID 3857',
        visible: true
      }
    ])
  })

  it('keeps tile stat consistency check for completed generation stats', () => {
    const metadata = ref({
      tiles_processed: 10,
      generated_tiles: 6,
      empty_tiles: 2,
      skipped_tiles: 1,
      failed_tiles: 1
    })

    const { executionStatsAvailable, executionStatsCheck, executionStatItems } = useTileCacheExecutionStats({ t, metadata })

    expect(executionStatsAvailable.value).toBe(true)
    expect(executionStatsCheck.value).toMatchObject({
      visible: true,
      type: 'success',
      message: '统计一致：已处理 10 = 分类 10'
    })
    expect(executionStatItems.value.map((item) => item.key)).toEqual([
      'tiles_processed',
      'generated_tiles',
      'empty_tiles',
      'skipped_tiles',
      'failed_tiles'
    ])
  })
})
