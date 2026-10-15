import { describe, expect, it } from 'vitest'
import { captureSourceDeny } from '../src/utils/sourceDeny'

const path = { engine_id: 2, version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' }, { term: 'table', kind: 'table', name: 'a"b/中文' }
] }
const row = { catalog_path: path, item_fingerprint: 'a'.repeat(64), recipient_type: 'user', recipient_id: '9007199254740993' }
const form = { expiryMode: 'at_time', expiresAt: new Date(2000), reason: ' Test restriction ' }
describe('explicit source Deny command', () => {
  it('captures exact immutable identity and validity without a boundary digest or Grant', () => {
    const command = captureSourceDeny('2', row, form, 1000)
    expect(Object.isFrozen(command)).toBe(true)
    expect(JSON.parse(command.payload)).toEqual({ deny_id: command.denyID, item_fingerprint: row.item_fingerprint,
      catalog_path: path, recipient_type: 'user', recipient_id: row.recipient_id, action: 'read', expiry_mode: 'at_time', expires_at: '1970-01-01T00:00:02.000Z', reason: 'Test restriction' })
    form.reason = 'Changed after capture'
    expect(command.payload).toContain('Test restriction')
    expect(JSON.parse(captureSourceDeny('2', row, { ...form, expiryMode: 'until_revoked' }).payload).expires_at).toBeNull()
  })
  it('rejects another engine, unsafe identifiers, missing identity, unspecified validity and invalid reasons', () => {
    for (const changed of [{ catalog_path: { ...path, engine_id: 3 } }, { recipient_id: 9007199254740992 }, { recipient_id: '9223372036854775808' }, { recipient_type: 'department' }, { item_fingerprint: '' }]) {
      expect(() => captureSourceDeny('2', { ...row, ...changed }, form, 1000)).toThrow()
    }
    for (const changed of [{ expiryMode: '' }, { expiresAt: new Date(500) }, { reason: ' ' }, { reason: '字'.repeat(2001) }]) {
      expect(() => captureSourceDeny('2', row, { ...form, ...changed }, 1000)).toThrow()
    }
  })
})
