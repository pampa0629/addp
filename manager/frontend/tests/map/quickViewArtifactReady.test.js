import { describe, expect, it, vi } from 'vitest'
import {
  isQuickViewArtifactReady,
  waitForQuickViewArtifactReady
} from '../../src/utils/quickViewArtifactReady'

describe('quickViewArtifactReady', () => {
  it('matches only the requested ready render source', () => {
    expect(isQuickViewArtifactReady({
      can_use_quick_view: true,
      render_source: 'model_3d_glb'
    }, 'model_3d_glb')).toBe(true)

    expect(isQuickViewArtifactReady({
      can_use_quick_view: true,
      render_source: 'gaussian_splat_ksplat'
    }, 'model_3d_glb')).toBe(false)

    expect(isQuickViewArtifactReady({
      can_use_quick_view: false,
      render_source: 'model_3d_glb'
    }, 'model_3d_glb')).toBe(false)
  })

  it('waits until quick view capability reports the generated artifact', async () => {
    const fetchStatus = vi.fn()
      .mockResolvedValueOnce({
        can_use_quick_view: false,
        unavailable_reason: 'requires_glb_generation'
      })
      .mockResolvedValueOnce({
        can_use_quick_view: true,
        render_source: 'model_3d_glb'
      })

    const result = await waitForQuickViewArtifactReady(fetchStatus, 'model_3d_glb', {
      intervalMs: 0,
      initialDelayMs: 0,
      maxAttempts: 3
    })

    expect(fetchStatus).toHaveBeenCalledTimes(2)
    expect(result).toMatchObject({
      ready: true,
      status: {
        can_use_quick_view: true,
        render_source: 'model_3d_glb'
      }
    })
  })

  it('returns the last status when the artifact is not ready in time', async () => {
    const fetchStatus = vi.fn()
      .mockResolvedValue({
        can_use_quick_view: false,
        unavailable_reason: 'requires_glb_generation'
      })

    const result = await waitForQuickViewArtifactReady(fetchStatus, 'model_3d_glb', {
      intervalMs: 0,
      initialDelayMs: 0,
      maxAttempts: 2
    })

    expect(fetchStatus).toHaveBeenCalledTimes(2)
    expect(result).toMatchObject({
      ready: false,
      status: {
        can_use_quick_view: false,
        unavailable_reason: 'requires_glb_generation'
      }
    })
  })
})
