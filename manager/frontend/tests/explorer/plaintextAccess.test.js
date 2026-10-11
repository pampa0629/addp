import { describe, expect, it } from 'vitest'

import { hasAccessGrantSynchronized, isAccessGrantSynchronizing } from '../../src/utils/plaintextAccess.js'

describe('plaintext access state transitions', () => {
  it('does not treat an already active grant as a new approval on initial load', () => {
    expect(hasAccessGrantSynchronized([], [
      { assessment_id: 'email', active_exemption_id: 'grant-1', projection_acknowledged: true, access_request: { state: 'approved' } }
    ])).toBe(false)
  })

  it('detects a pending request becoming an active grant for the same assessment', () => {
    expect(hasAccessGrantSynchronized(
      [{ assessment_id: 'email', access_request: { state: 'pending' } }],
      [{ assessment_id: 'email', active_exemption_id: 'grant-1', projection_acknowledged: true, access_request: { state: 'approved' } }]
    )).toBe(true)
  })

  it('does not confuse an active grant on another field with the pending request', () => {
    expect(hasAccessGrantSynchronized(
      [{ assessment_id: 'phone', access_request: { state: 'pending' } }],
      [{ assessment_id: 'email', active_exemption_id: 'grant-1', projection_acknowledged: true, access_request: { state: 'approved' } }]
    )).toBe(false)
  })

  it('keeps an approved grant waiting until its Owner checkpoint is confirmed', () => {
    const pending = { assessment_id: 'phone', access_request: { state: 'pending' } }
    const waiting = { assessment_id: 'phone', active_exemption_id: 'grant-1', projection_acknowledged: false, access_request: { state: 'approved' } }
    const synchronized = { ...waiting, projection_acknowledged: true }
    expect(isAccessGrantSynchronizing(waiting)).toBe(true)
    expect(hasAccessGrantSynchronized([pending], [waiting])).toBe(false)
    expect(hasAccessGrantSynchronized([waiting], [synchronized])).toBe(true)
    expect(isAccessGrantSynchronizing(synchronized)).toBe(false)
    expect(hasAccessGrantSynchronized([synchronized], [synchronized])).toBe(false)
  })

  it('does not wait for an expired or revoked grant or infer a missing checkpoint', () => {
    expect(isAccessGrantSynchronizing({ projection_acknowledged: false, access_request: { state: 'approved' } })).toBe(false)
    expect(hasAccessGrantSynchronized(
      [{ assessment_id: 'phone', access_request: { state: 'pending' } }],
      [{ assessment_id: 'phone', active_exemption_id: 'grant-1' }]
    )).toBe(false)
  })
})
