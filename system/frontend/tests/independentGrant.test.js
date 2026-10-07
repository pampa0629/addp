import { describe, expect, it } from 'vitest'
import { captureIndependentGrant } from '../src/utils/independentGrant'

const path = { engine_id: '2', version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' },
  { term: 'table', kind: 'table', name: 'a"b/中文' }
] }
const requirement = { engine_id: '2', catalog_path: path, mode: 'independent', version: 1 }
const candidates = [{ id: '9007199254740993', name: 'Outdoor reader' }]
const form = { recipientType: 'user', recipientID: candidates[0].id, expiryMode: 'until_revoked', expiresAt: null, reason: ' Explicit read ' }
describe('independent read-grant command capture', () => {
  it('captures immutable lossless identifiers, explicit validity and one exact target', () => {
    const captured = captureIndependentGrant('2', requirement, form, candidates)
    const body = JSON.parse(captured.payload)
    expect(body).toEqual({ request_id: captured.requestID, requirement_version: '1', recipient_type: 'user', recipient_id: candidates[0].id,
      action: 'read', expiry_mode: 'until_revoked', expires_at: null, reason: 'Explicit read', catalog_path: { ...path, engine_id: 2 } })
    expect(Object.isFrozen(captured)).toBe(true)
    expect(captured.recipientLabel).toBe(candidates[0].name)
    expect(captureIndependentGrant('2', requirement, { ...form, recipientType: 'department' }, candidates).payload).toContain('"recipient_type":"department"')
  })
  it('requires current candidates, exact independent target, valid version and explicit expiry', () => {
    for (const changed of [{ recipientID: '1' }, { recipientType: 'service_principal' }, { expiryMode: '' }, { reason: ' ' }, { reason: '字'.repeat(2001) }, { expiryMode: 'at_time', expiresAt: new Date(1) }]) {
      expect(() => captureIndependentGrant('2', requirement, { ...form, ...changed }, candidates)).toThrow()
    }
    for (const changed of [{ mode: 'catalog' }, { engine_id: '3' }, { version: 0 }, { version: 9007199254740993 },
      { catalog_path: { ...path, segments: [...path.segments.slice(0, -1), { term: 'table', kind: 'view', name: 'view' }] } }]) {
      expect(() => captureIndependentGrant('2', { ...requirement, ...changed }, form, candidates)).toThrow()
    }
    expect(() => captureIndependentGrant('2', requirement, form, [])).toThrow()
    const future = captureIndependentGrant('2', requirement, { ...form, expiryMode: 'at_time', expiresAt: new Date(2000) }, candidates, 1000)
    expect(JSON.parse(future.payload).expires_at).toBe('1970-01-01T00:00:02.000Z')
  })
})
