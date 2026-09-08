import { describe, expect, it } from 'vitest'

import { hasAccessGrantActivated } from '../../src/utils/plaintextAccess.js'

describe('plaintext access state transitions', () => {
  it('does not treat an already active grant as a new approval on initial load', () => {
    expect(hasAccessGrantActivated([], [
      { assessment_id: 'email', active_exemption_id: 'grant-1', access_request: { state: 'approved' } }
    ])).toBe(false)
  })

  it('detects a pending request becoming an active grant for the same assessment', () => {
    expect(hasAccessGrantActivated(
      [{ assessment_id: 'email', access_request: { state: 'pending' } }],
      [{ assessment_id: 'email', active_exemption_id: 'grant-1', access_request: { state: 'approved' } }]
    )).toBe(true)
  })

  it('does not confuse an active grant on another field with the pending request', () => {
    expect(hasAccessGrantActivated(
      [{ assessment_id: 'phone', access_request: { state: 'pending' } }],
      [{ assessment_id: 'email', active_exemption_id: 'grant-1', access_request: { state: 'approved' } }]
    )).toBe(false)
  })
})
