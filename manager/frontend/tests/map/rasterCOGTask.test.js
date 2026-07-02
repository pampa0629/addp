import { describe, expect, it, vi } from 'vitest'
import { waitForRasterCOGExecution } from '../../src/utils/rasterCOGTask'

describe('rasterCOGTask', () => {
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
