import { computed } from 'vue'
import { formatBytes } from '../utils/formatters'

const requiredTileStatKeys = ['tiles_processed', 'generated_tiles', 'empty_tiles', 'skipped_tiles', 'failed_tiles']

const formatInteger = (value) => {
  const number = Number(value)
  return Number.isFinite(number) ? Math.trunc(number).toLocaleString() : '-'
}

const integerValue = (value) => {
  const number = Number(value)
  return Number.isFinite(number) ? Math.trunc(number) : null
}

const formatByteValue = (value) => {
  const number = Number(value)
  return Number.isFinite(number) ? formatBytes(number) : '-'
}

const formatSizeKB = (value) => {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? formatBytes(number * 1024) : '-'
}

export function useTileCacheExecutionStats({ t, metadata }) {
  const executionStatsAvailable = computed(() => {
    const value = metadata.value
    return [
      'tiles_total_estimate',
      'tiles_processed',
      'generated_tiles',
      'empty_tiles',
      'failed_tiles',
      'total_size_bytes',
      'zoom_levels'
    ].some((key) => value[key] !== undefined && value[key] !== null)
  })

  const executionStatsCheck = computed(() => {
    const value = metadata.value
    const missingKeys = requiredTileStatKeys.filter((key) => value[key] === undefined || value[key] === null)
    if (missingKeys.length > 0) {
      return {
        visible: executionStatsAvailable.value,
        type: 'info',
        message: t('manager.tileCache.statsCheckIncomplete')
      }
    }

    const processed = integerValue(value.tiles_processed)
    const generated = integerValue(value.generated_tiles)
    const empty = integerValue(value.empty_tiles)
    const skipped = integerValue(value.skipped_tiles)
    const failed = integerValue(value.failed_tiles)
    if ([processed, generated, empty, skipped, failed].some((item) => item === null)) {
      return {
        visible: true,
        type: 'warning',
        message: t('manager.tileCache.statsCheckInvalid')
      }
    }

    const classified = generated + empty + skipped + failed
    if (processed !== classified) {
      return {
        visible: true,
        type: 'warning',
        message: t('manager.tileCache.statsCheckMismatch', {
          processed: formatInteger(processed),
          classified: formatInteger(classified)
        })
      }
    }

    return {
      visible: true,
      type: 'success',
      message: t('manager.tileCache.statsCheckMatched', {
        processed: formatInteger(processed),
        classified: formatInteger(classified)
      })
    }
  })

  const executionStatItems = computed(() => {
    const value = metadata.value
    return [
      {
        key: 'tiles_total_estimate',
        label: t('manager.tileCache.tilesTotalEstimate'),
        value: formatInteger(value.tiles_total_estimate)
      },
      {
        key: 'tiles_processed',
        label: t('manager.tileCache.tilesProcessed'),
        value: formatInteger(value.tiles_processed)
      },
      {
        key: 'generated_tiles',
        label: t('manager.tileCache.generatedTiles'),
        value: formatInteger(value.generated_tiles)
      },
      {
        key: 'empty_tiles',
        label: t('manager.tileCache.emptyTiles'),
        value: formatInteger(value.empty_tiles)
      },
      {
        key: 'skipped_tiles',
        label: t('manager.tileCache.skippedTiles'),
        value: formatInteger(value.skipped_tiles)
      },
      {
        key: 'oversized_skipped_tiles',
        label: t('manager.tileCache.oversizedSkippedTiles'),
        value: formatInteger(value.oversized_skipped_tiles)
      },
      {
        key: 'failed_tiles',
        label: t('manager.tileCache.failedTiles'),
        value: formatInteger(value.failed_tiles)
      },
      {
        key: 'actual_max_zoom',
        label: t('manager.tileCache.actualMaxZoom'),
        value: formatInteger(value.actual_max_zoom)
      },
      {
        key: 'total_size_bytes',
        label: t('manager.tileCache.totalTileSize'),
        value: formatByteValue(value.total_size_bytes)
      },
      {
        key: 'max_tile_size_bytes',
        label: t('manager.tileCache.maxTileSize'),
        value: formatByteValue(value.max_tile_size_bytes)
      }
    ]
  })

  const zoomLevelRows = computed(() => {
    const zoomLevels = metadata.value.zoom_levels
    if (!zoomLevels || typeof zoomLevels !== 'object') return []
    return Object.entries(zoomLevels)
      .map(([zoom, item]) => {
        const row = item && typeof item === 'object' ? item : {}
        const zoomValue = Number(row.zoom ?? zoom)
        return {
          zoom: Number.isFinite(zoomValue) ? zoomValue : zoom,
          totalTilesText: formatInteger(row.total_tiles),
          generatedTilesText: formatInteger(row.generated_tiles),
          emptyTilesText: formatInteger(row.empty_tiles),
          skippedTilesText: formatInteger(row.skipped_tiles),
          oversizedTilesText: formatInteger(row.oversized_tiles),
          failedTilesText: formatInteger(row.failed_tiles),
          avgSizeText: formatSizeKB(row.avg_size_kb),
          maxSizeText: formatByteValue(row.max_size_bytes)
        }
      })
      .sort((a, b) => Number(a.zoom) - Number(b.zoom))
  })

  return {
    executionStatsAvailable,
    executionStatsCheck,
    executionStatItems,
    zoomLevelRows
  }
}
