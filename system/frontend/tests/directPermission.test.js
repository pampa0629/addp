import { createHash } from 'node:crypto'
import { describe, expect, it, vi } from 'vitest'
import { captureDirectPermission, readPermissionTarget, validateDirectPermission } from '../src/utils/directPermission'
const fingerprint = 'a'.repeat(64), hash = 'b'.repeat(64)
const path = { engine_id: '9007199254740993', version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' }, { term: 'table', kind: 'table', name: 'a"b/中文' }] }
const row = { engine_id: path.engine_id, item_fingerprint: fingerprint, state: 'denied', source: 'explicit', version: '9007199254740995' }
const target = { path, binding: { item_fingerprint: fingerprint, boundary_hash: hash, native_target: path } }
describe('effective direct permission observation and command', () => {
  it('keeps source, version and exact target lossless without caller authority or a Grant', () => {
    const encoded = captureDirectPermission(row, target, false, ' Review ')
    expect(encoded).toContain('"engine_id":9007199254740993')
    expect(encoded).toContain('"expected_version":"9007199254740995"')
    expect(JSON.parse(encoded)).toMatchObject({ allowed: false, expected_source: 'explicit', expected_boundary_hash: hash, reason: 'Review' })
    expect(encoded).not.toContain('mode')
    expect(encoded).not.toContain('principal')
    expect(encoded).not.toContain('request_id')
  })
  it('rejects unknown state masquerading as allow, ambiguous versions and wrong identity', () => {
    for (const changed of [{ version: 1 }, { version: '0' }, { version: '9223372036854775808' }, { source: 'deployment_unknown', state: 'allowed', version: '0' }, { source: 'independent' }, { state: 'unknown' }, { engine_id: '2' }, { item_fingerprint: 'other' }]) {
      expect(() => validateDirectPermission({ ...row, ...changed }, path.engine_id, fingerprint)).toThrow()
    }
    expect(validateDirectPermission({ ...row, source: 'deployment_unknown', state: 'unknown', version: '0' }, path.engine_id, fingerprint).state).toBe('unknown')
    expect(() => captureDirectPermission(row, target, 'false', 'Review')).toThrow()
    expect(() => captureDirectPermission(row, target, false, '')).toThrow()
  })
  it('requires a current complete Meta boundary and never treats identity-list fields as facts', async () => {
    // This fixture uses an exact supported numeric engine for facts; the command test above covers raw identifier preservation.
    const native = { ...path, engine_id: 2 }, fp = createHash('sha256').update('2:outdoor.a"b/中文').digest('hex')
    const complete = { schema_version: 'meta.data_item_authorization_facts/v1', item_fingerprint: fp, engine_id: 2, basis_state: 'complete', boundary: { layout: 'single', native_target: native }, boundary_hash: hash }
    const observe = vi.fn().mockResolvedValue(complete)
    expect(await readPermissionTarget('2', fp, observe)).toEqual({ path: native, binding: { item_fingerprint: fp, boundary_hash: hash, native_target: native } })
    expect(observe).toHaveBeenCalledTimes(1)
    const read = vi.fn().mockResolvedValue({ item_fingerprint: fp, boundary: null, basis_state: 'incomplete' })
    await expect(readPermissionTarget('2', fp, read)).rejects.toThrow()
    expect(read).toHaveBeenCalledTimes(1)
    read.mockResolvedValue({ engine_id: 3, item_fingerprint: fp, basis_state: 'complete', boundary: { layout: 'single', native_target: { ...native, engine_id: 3 } }, boundary_hash: hash })
    await expect(readPermissionTarget('2', fp, read)).rejects.toThrow()
  })
})
