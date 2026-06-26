import { describe, expect, it, vi } from 'vitest'
import {
  buildRasterCOGTaskPayload,
  shouldShowRasterCOGGenerationAction,
  waitForRasterCOGExecution
} from '../../src/utils/rasterCOGTask'

describe('rasterCOGTask', () => {
  it('shows managed COG generation action for large raster quick-view gaps', () => {
    expect(shouldShowRasterCOGGenerationAction({
      can_use_quick_view: false,
      unavailable_reason: 'requires_managed_cog'
    })).toBe(true)

    expect(shouldShowRasterCOGGenerationAction({
      can_use_quick_view: false,
      raster: {
        recommended_action: 'create_managed_cog'
      }
    })).toBe(true)
  })

  it('does not show managed COG action when raster quick view is already usable', () => {
    expect(shouldShowRasterCOGGenerationAction({
      can_use_quick_view: true,
      unavailable_reason: 'requires_managed_cog'
    })).toBe(false)
  })

  it('builds tiff_to_cog task payload from locator capability facts', () => {
    const payload = buildRasterCOGTaskPayload(
      {
        engineId: 9,
        locator: 'addp://engine/9/path/addp/image/srtm_40_01.tif?type=object&item_id=254',
        itemID: 254,
        sourceSRID: 4326,
        extent: [110, 20, 120, 30]
      },
      {
        item_fingerprint: 'fp-raster',
        raster: {
          profile: 'geotiff',
          size_bytes: 900 * 1024 * 1024,
          width: 120000,
          height: 80000,
          band_count: 1,
          extent_srid: 4326
        }
      },
      '生成 COG - srtm_40_01'
    )

    expect(payload).toMatchObject({
      name: '生成 COG - srtm_40_01',
      enabled: true,
      config: {
        target: {
          source_engine_id: 9,
          locator: 'addp://engine/9/path/addp/image/srtm_40_01.tif?type=object&item_id=254',
          item_id: 254,
          item_fingerprint: 'fp-raster'
        },
        raster: {
          source_profile: 'geotiff',
          source_size_bytes: 900 * 1024 * 1024,
          width: 120000,
          height: 80000,
          band_count: 1,
          source_srid: 4326,
          extent: [110, 20, 120, 30],
          extent_srid: 4326
        },
        cog: {
          compression: 'DEFLATE',
          blocksize: 512,
          overview_resampling: 'NEAREST'
        }
      }
    })
  })

  it('builds payload from quick view capability when route target only provides locator', () => {
    const payload = buildRasterCOGTaskPayload(
      {
        locator: 'addp://engine/26/path/rasters/large.tif?type=file'
      },
      {
        source_engine_id: 26,
        locator: 'addp://engine/26/path/rasters/large.tif?type=file',
        item_fingerprint: 'fp-large',
        raster: {
          profile: 'geotiff',
          width: 8192,
          height: 8192,
          band_count: 3,
          source_srid: 4326
        },
        quick_view: {
          extent: [110, 20, 120, 30],
          extent_srid: 4326
        }
      },
      'Generate COG'
    )

    expect(payload.config.target).toMatchObject({
      source_engine_id: 26,
      locator: 'addp://engine/26/path/rasters/large.tif?type=file',
      item_fingerprint: 'fp-large'
    })
    expect(payload.config.raster).toMatchObject({
      source_profile: 'geotiff',
      width: 8192,
      height: 8192,
      band_count: 3,
      source_srid: 4326,
      extent: [110, 20, 120, 30],
      extent_srid: 4326
    })
  })

  it('waits until the raster COG execution reaches a terminal status', async () => {
    const fetchExecutionStatus = vi.fn()
      .mockResolvedValueOnce({ execution_id: 'exec-1', status: 'running' })
      .mockResolvedValueOnce({ execution_id: 'exec-1', status: 'success' })

    const result = await waitForRasterCOGExecution('exec-1', fetchExecutionStatus, {
      intervalMs: 0,
      initialDelayMs: 0,
      maxAttempts: 3
    })

    expect(fetchExecutionStatus).toHaveBeenCalledTimes(2)
    expect(result).toMatchObject({
      completed: true,
      success: true,
      status: 'success'
    })
  })

  it('marks failed raster COG execution as completed but unsuccessful', async () => {
    const fetchExecutionStatus = vi.fn()
      .mockResolvedValueOnce({ execution_id: 'exec-2', status: 'failed' })

    const result = await waitForRasterCOGExecution('exec-2', fetchExecutionStatus, {
      intervalMs: 0,
      initialDelayMs: 0,
      maxAttempts: 3
    })

    expect(fetchExecutionStatus).toHaveBeenCalledTimes(1)
    expect(result).toMatchObject({
      completed: true,
      success: false,
      failed: true,
      status: 'failed'
    })
  })

  it('returns incomplete when raster COG execution polling reaches the attempt limit', async () => {
    const fetchExecutionStatus = vi.fn()
      .mockResolvedValue({ execution_id: 'exec-3', status: 'running' })

    const result = await waitForRasterCOGExecution('exec-3', fetchExecutionStatus, {
      intervalMs: 0,
      initialDelayMs: 0,
      maxAttempts: 2
    })

    expect(fetchExecutionStatus).toHaveBeenCalledTimes(2)
    expect(result).toMatchObject({
      completed: false,
      success: false,
      failed: false,
      status: 'running'
    })
  })
})
