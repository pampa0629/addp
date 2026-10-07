import { describe, expect, it } from 'vitest'
import { captureDelegation } from '../src/utils/engineAccessDelegation'

const now = Date.parse('2026-10-06T00:00:00Z')
const member = { id: '9007199254740993', principal_type: 'user', principal_status: 'active', status: 'active', expires_at: '2026-12-01T00:00:00Z' }
const form = () => ({ membershipID: member.id, expiresAt: new Date('2026-11-01T00:00:00Z'), reason: ' Hand over ' })
describe('engine management delegation command', () => {
  it('uses a selected membership without rounding and sends only the explicit expiry and reason', () => {
    expect(captureDelegation(form(), [member], now)).toEqual({ tenant_membership_id: member.id, expires_at: '2026-11-01T00:00:00.000Z', reason: 'Hand over' })
  })
  it.each([
    { membershipID: 'manual' }, { expiresAt: null }, { expiresAt: new Date(now) }, { reason: ' ' },
    { expiresAt: new Date('2027-01-01T00:00:00Z') }
  ])('rejects an incomplete or expired command %j', change => {
    expect(() => captureDelegation({ ...form(), ...change }, [member], now)).toThrow()
  })
  it.each([{ principal_type: 'service_principal' }, { principal_status: 'disabled' }, { status: 'suspended' }, { ended_at: '2026-10-01' }])('rejects an ineligible account %j', change => {
    expect(() => captureDelegation(form(), [{ ...member, ...change }], now)).toThrow()
  })
})
