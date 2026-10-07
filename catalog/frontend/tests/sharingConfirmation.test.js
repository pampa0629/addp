import { describe, expect, it } from 'vitest'
import { canonicalSharingUUID, captureSharingConfirmation, captureSharingRequest, sharingEligibility, sharingProgress, hasInvalidSharingRouteIdentity } from '../src/utils/sharingConfirmation'

const id = 'cc0a8000-6000-4000-8000-800000000001'
const entry = { version: 4, entry_type: 'data_item', entry_status: 'active', governance_status: 'curated',
  source: { source_module: 'meta', source_status: 'active' },
  responsibilities: [{ role: 'business_owner', subject_type: 'user', subject_id: '50', status: 'active' }] }
const form = { recipientType: 'user', recipientID: '9007199254740993', expiryMode: 'until_revoked', expiresAt: null, reason: ' research ' }
const options = [{ id: form.recipientID, recipient_type: 'user', status: 'active' }]
const auth = permissions => ({ authContext: { principal: { type: 'user', id: '50' } }, hasPermission: permission => permissions.includes(permission) })

describe('explicit human sharing confirmation', () => {
  it('explains the next responsible action without granting either role data access', () => {
    expect(sharingProgress({ canCreate: true })).toMatchObject({ status: 'awaitingConfirmation', step: 0, next: 'confirm' })
    expect(sharingProgress({ canHandle: true })).toMatchObject({ status: 'chooseConfirmation' })
    const selected = { decisionID: id, confirmation: { id } }
    expect(sharingProgress(selected)).toMatchObject({ status: 'awaitingHandling', next: 'askHandler' })
    expect(sharingProgress({ ...selected, canHandle: true })).toMatchObject({ next: 'handle' })
  })
  it('keeps accepted, pending and issued historical facts distinct', () => {
    const current = { requestID: id, request: { request_id: id, state: 'accepted' } }
    expect(sharingProgress(current)).toMatchObject({ status: 'issuing', step: 2 })
    expect(sharingProgress({ ...current, request: { ...current.request, granted_at: '2026-10-07T00:00:00Z' } })).toEqual({ status: 'issued', step: 3, next: 'issued' })
    expect(sharingProgress({ ...current, request: { ...current.request, state: 'pending' } })).toMatchObject({ status: 'processing', step: 1 })
    expect(sharingProgress({ ...current, request: { ...current.request, state: 'closed' } })).toMatchObject({ status: 'closed' })
  })
  it('never uses another confirmation or a later history page as current progress', () => {
    const selected = { decisionID: id, confirmation: { id }, results: [{ decision_id: id, state: 'accepted', granted_at: '2026-10-07T00:00:00Z' }] }
    expect(sharingProgress(selected)).toMatchObject({ status: 'issued' })
    expect(sharingProgress({ ...selected, resultsPage: 2 })).toMatchObject({ status: 'history', step: -1 })
    expect(sharingProgress({ ...selected, results: [{ ...selected.results[0], decision_id: 'another' }] })).toMatchObject({ status: 'mismatch' })
    expect(sharingProgress({ ...selected, requestID: 'request', request: { request_id: 'request', decision_id: 'another', state: 'accepted', granted_at: '2026-10-07T00:00:00Z' } })).toMatchObject({ status: 'mismatch' })
  })
  it('does not convert a failed query or loading state into unhandled sharing', () => {
    const selected = { decisionID: id, confirmation: { id } }
    expect(sharingProgress({ ...selected, error: true })).toMatchObject({ status: 'unavailable', step: -1 })
    expect(sharingProgress({ ...selected, loading: true })).toMatchObject({ status: 'checking', step: -1 })
    expect(sharingProgress({ decisionID: id })).toMatchObject({ status: 'unavailable' })
    expect(sharingProgress({ requestID: id })).toMatchObject({ status: 'unavailable' })
  })
  it('uses the exact authorized confirmation result when the viewer cannot read handler-only request detail', () => {
    const result = { request_id: id, decision_id: 'decision', state: 'accepted', granted_at: '2026-10-07T00:00:00Z' }
    expect(sharingProgress({ decisionID: 'decision', requestID: id, results: [result] })).toMatchObject({ status: 'issued' })
    expect(sharingProgress({ decisionID: 'decision', requestID: 'another', results: [result] })).toMatchObject({ status: 'unavailable' })
  })
  it('does not reinterpret an invalid routed original identity as a new command', () => {
    expect(hasInvalidSharingRouteIdentity({})).toBe(false)
    expect(hasInvalidSharingRouteIdentity({ sharing_decision_id: id, sharing_request_id: id })).toBe(false)
    for (const invalid of [null, '', 'invalid', id.toUpperCase(), [id], ` ${id}`]) {
      expect(hasInvalidSharingRouteIdentity({ sharing_decision_id: invalid })).toBe(true)
      expect(hasInvalidSharingRouteIdentity({ sharing_request_id: invalid })).toBe(true)
    }
  })
  it('freezes the original handling version without defaults, numeric rounding or target overrides', () => {
    const candidate = { id, target: { engine_id: '9007199254740993', version: 'v1', segments: [] } }
    const requirement = { mode: 'catalog', requirement_version: '9007199254740993' }
    const captured = captureSharingRequest(candidate, requirement, id)
    expect(captured).toEqual({ request_id: id, decision_id: id, requirement_version: '9007199254740993' })
    expect(Object.isFrozen(captured)).toBe(true)
    for (const invalid of [{}, { mode: 'independent', requirement_version: '1' }, { ...requirement, requirement_version: 9007199254740993 },
      { ...requirement, requirement_version: '01' }, { ...requirement, requirement_version: '9223372036854775808' }]) {
      expect(() => captureSharingRequest(candidate, invalid, id)).toThrow('invalidHandlingRequirement')
    }
    expect(() => captureSharingRequest(candidate, requirement, '')).toThrow()
    expect(() => captureSharingRequest({ ...candidate, target: { ...candidate.target, engine_id: 9007199254740993 } }, requirement, id)).toThrow()
  })
  it('keeps read, confirmation and fulfillment permissions independent of curation and responsibility', () => {
    expect(sharingEligibility(entry, auth(['catalog.entry.read', 'catalog.entry.update']))).toEqual({ owner: true, confirm: false, create: false, history: false })
    expect(sharingEligibility(entry, auth(['catalog.entry.read', 'catalog.sharing_decision.create']))).toEqual({ owner: true, confirm: true, create: true, history: false })
    expect(sharingEligibility(entry, auth(['catalog.entry.read', 'system.engine_access_fulfillment.create']))).toEqual({ owner: true, confirm: false, create: false, history: true })
    expect(sharingEligibility({ ...entry, governance_status: 'deprecated' }, auth(['catalog.entry.read', 'catalog.sharing_decision.create', 'system.engine_access_fulfillment.create']))).toEqual({ owner: true, confirm: true, create: false, history: true })
    const another = auth(['catalog.entry.read', 'catalog.sharing_decision.create']); another.authContext.principal.id = '51'
    expect(sharingEligibility(entry, another)).toMatchObject({ confirm: true, create: false })
  })
  it('captures immutable original parameters and preserves decimal ID precision', () => {
    const captured = captureSharingConfirmation(entry, form, options, id)
    expect(captured).toEqual({ decision_id: id, version: '4', recipient_type: 'user', recipient_id: '9007199254740993', expiry_mode: 'until_revoked', expires_at: null, reason: 'research' })
    expect(Object.isFrozen(captured)).toBe(true)
    expect(captureSharingConfirmation({ ...entry, version: '9007199254740993' }, form, options, id).version).toBe('9007199254740993')
    expect(() => captureSharingConfirmation({ ...entry, version: 9007199254740993 }, form, options, id)).toThrow()
  })
  it('rejects a manually entered ID, inactive or wrong-type recipient and missing purpose', () => {
    for (const candidates of [[], [{ ...options[0], status: 'disabled' }], [{ ...options[0], recipient_type: 'project_group' }]]) {
      expect(() => captureSharingConfirmation(entry, form, candidates, id)).toThrow()
    }
    expect(() => captureSharingConfirmation(entry, { ...form, reason: ' ' }, options, id)).toThrow()
  })
  it('requires an explicit mode and never defaults a missing date to indefinite', () => {
    const now = Date.parse('2026-10-03T00:00:00Z')
    for (const invalid of [{ expiryMode: '' }, { expiryMode: 'at_time' }, { expiryMode: 'at_time', expiresAt: new Date(now) }, { expiresAt: new Date(now + 1000) }]) {
      expect(() => captureSharingConfirmation(entry, { ...form, ...invalid }, options, id, now)).toThrow()
    }
    expect(captureSharingConfirmation(entry, { ...form, expiryMode: 'at_time', expiresAt: new Date(now + 1000) }, options, id, now).expires_at).toBe('2026-10-03T00:00:01.000Z')
  })
  it('accepts only a nonzero canonical opaque confirmation ID from the route', () => {
    expect(canonicalSharingUUID(id)).toBe(id)
    for (const invalid of [id.toUpperCase(), [id], '00000000-0000-0000-0000-000000000000', 'untrusted/path']) expect(canonicalSharingUUID(invalid)).toBe('')
  })
})
