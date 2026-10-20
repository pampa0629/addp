import { describe, expect, it } from 'vitest'
import { captureIndependentGrant } from '../src/utils/independentGrant'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

const path = { engine_id: '2', version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' },
  { term: 'table', kind: 'table', name: 'a"b/中文' }
] }
const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), native_target: path }
const requirement = { binding, engine_id: '2', catalog_path: path, mode: 'independent', version: 1 }
const candidates = [{ id: '9007199254740993', name: 'Outdoor reader' }]
const form = { recipientType: 'user', recipientID: candidates[0].id, expiryMode: 'until_revoked', expiresAt: null, reason: ' Explicit read ' }
describe('independent read-grant command capture', () => {
  it('labels production as its own authorization source in both locales', () => {
    expect(zh.system.engine.dataAuthorization.modes.task_production).toBe('任务产出初始授权')
    expect(en.system.engine.dataAuthorization.modes.task_production).toBe('Initial task output grant')
  })
  it('captures immutable lossless identifiers, explicit validity and one exact target', () => {
    const captured = captureIndependentGrant('2', requirement, form, candidates)
    const body = JSON.parse(captured.payload)
    expect(body).toEqual({ item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash, request_id: captured.requestID, requirement_version: '1', initialize_approval: false, recipient_type: 'user', recipient_id: candidates[0].id,
      action: 'read', expiry_mode: 'until_revoked', expires_at: null, reason: 'Explicit read', catalog_path: { ...path, engine_id: 2 } })
    expect(Object.isFrozen(captured)).toBe(true)
    expect(captured.recipientLabel).toBe(candidates[0].name)
    expect(captured.recipientType).toBe('user')
    expect(captured.recipientID).toBe('9007199254740993')
    expect(captured.targetLabel).toBe('outdoor / a"b/中文')
    expect(JSON.parse(captureIndependentGrant('2', { ...requirement, initialize_approval: true }, form, candidates).payload).initialize_approval).toBe(true)
    expect(() => captureIndependentGrant('2', { ...requirement, version: '2', initialize_approval: true }, form, candidates)).toThrow()
    expect(captureIndependentGrant('2', requirement, { ...form, recipientType: 'department' }, candidates).payload).toContain('"recipient_type":"department"')
  })
  it('requires current candidates, exact independent target, valid version and explicit expiry', () => {
    for (const changed of [{ recipientID: '1' }, { recipientType: 'service_principal' }, { expiryMode: '' }, { reason: ' ' }, { reason: '字'.repeat(2001) }, { expiryMode: 'at_time', expiresAt: new Date(1) }]) {
      expect(() => captureIndependentGrant('2', requirement, { ...form, ...changed }, candidates)).toThrow()
    }
    for (const changed of [{ binding: null }, { binding: { ...binding, boundary_hash: '' } }, { mode: 'catalog' }, { engine_id: '3' }, { version: 0 }, { version: 9007199254740993 },
      { catalog_path: { ...path, segments: [...path.segments.slice(0, -1), { term: 'table', kind: 'view', name: 'view' }] } }]) {
      expect(() => captureIndependentGrant('2', { ...requirement, ...changed }, form, candidates)).toThrow()
    }
    expect(() => captureIndependentGrant('2', requirement, form, [])).toThrow()
    const future = captureIndependentGrant('2', requirement, { ...form, expiryMode: 'at_time', expiresAt: new Date(2000) }, candidates, 1000)
    expect(JSON.parse(future.payload).expires_at).toBe('1970-01-01T00:00:02.000Z')
  })
})
