import { describe, expect, it, vi } from 'vitest'
// Exercise Giro3D's real readiness/event contract without its browser worker
// imports; actual COPC initialization and Range reads are covered by the E2E.
vi.mock('@giro3d/giro3d/sources/COPCSource.js', async () => {
  const { PointCloudSourceBase } = await import('@giro3d/giro3d/sources/PointCloudSource.js')
  return { default: class extends PointCloudSourceBase {} }
})
import { ColorDisplayCOPCSource } from '../../src/utils/pointCloudColor.js'

function sourceWithPendingRead() {
  let resolve, reject
  const read = new Promise((yes, no) => { resolve = yes; reject = no })
  const source = new ColorDisplayCOPCSource({}, () => 'rgb16')
  source.initializeOnce = vi.fn(() => read)
  const initialized = vi.fn()
  source.addEventListener('initialized', initialized)
  return { source, resolve, reject, initialized }
}

describe('COPC initialization rejection ownership', () => {
  it('shares one initialization and marks ready before notifying exactly once', async () => {
    const { source, resolve, initialized } = sourceWithPendingRead()
    initialized.mockImplementation(() => expect(source.ready).toBe(true))
    const pending = source.initialize()
    expect(source.initialize()).toBe(pending)
    expect(source.ready).toBe(false)
    expect(initialized).not.toHaveBeenCalled()
    resolve(source)
    await expect(pending).resolves.toBe(source)
    expect(source.initialize()).toBe(pending)
    expect(source.initializeOnce).toHaveBeenCalledTimes(1)
    expect(initialized).toHaveBeenCalledTimes(1)
  })

  for (const message of ['aborted', 'COPC range request failed: HTTP 403', 'invalid COPC header']) {
    it(`returns the original ${message} rejection without a detached promise`, async () => {
      const { source, reject, initialized } = sourceWithPendingRead()
      const failure = new Error(message)
      const pending = source.initialize()
      const assertion = expect(pending).rejects.toBe(failure)
      reject(failure)
      await assertion
      expect(source.ready).toBe(false)
      expect(initialized).not.toHaveBeenCalled()
      expect(source.initialize()).toBe(pending)
      expect(source.initializeOnce).toHaveBeenCalledTimes(1)
    })
  }
})
