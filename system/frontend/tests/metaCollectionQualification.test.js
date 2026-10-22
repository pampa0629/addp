import { describe, expect, it } from 'vitest'
import { captureMetaCollectionApproval, captureMetaCollectionFacts, captureMetaCollectionRevocation, metaCollectionRecordState } from '../src/utils/metaCollectionQualification'

const facts = { schema_version: 'meta.scan_task_facts/v1', engine_id: '2', task_id: '9007199254740993',
  task_definition_digest: 'a'.repeat(64), scope_kind: 'engine', scan_depth: 'basic', force: false, read_options: {} }
describe('Meta collection qualification UI protocol', () => {
  it('captures only the inspected exact task, purpose and expiry without data Grant fields', () => {
    const payload = captureMetaCollectionApproval(facts, '2', facts.task_id, ' Governance ', new Date('2099-01-01T00:00:00Z'))
    expect(payload).toEqual({ command_id: expect.any(String), task_id: facts.task_id, expected_task_definition_digest: facts.task_definition_digest, purpose: 'Governance', expires_at: '2099-01-01T00:00:00.000Z' })
    expect(Object.isFrozen(payload)).toBe(true)
    const copy = captureMetaCollectionFacts(facts, '2', facts.task_id)
    copy.read_options.encoding = 'utf8'
    expect(facts.read_options).toEqual({})
  })
  it.each([
    { engine_id: '3' }, { task_id: '4' }, { task_id: 42 }, { schema_version: 'other' },
    { scan_depth: 'deep' }, { scope_kind: 'path' }, { force: 'false' },
    { task_definition_digest: '' }, { read_options: null }, { read_options: { encoding: 'utf8' } },
    { read_options: { has_header: false } }, { read_options: { unknown: true } }
  ])('rejects mismatched, unsupported and unconfirmed facts %j', change => {
    expect(() => captureMetaCollectionFacts({ ...facts, ...change }, '2', facts.task_id)).toThrow()
  })
  it('rejects invalid decimal IDs, missing purpose, expired or missing expiry', () => {
    expect(() => captureMetaCollectionFacts(facts, '02', facts.task_id)).toThrow()
    expect(() => captureMetaCollectionFacts(facts, '2', '9223372036854775808')).toThrow()
    for (const [purpose, expiry] of [[' ', '2099-01-01'], ['x'.repeat(2001), '2099-01-01'], ['ok', null], ['ok', '2000-01-01']]) {
      expect(() => captureMetaCollectionApproval(facts, '2', facts.task_id, purpose, expiry)).toThrow()
    }
  })
  it('treats persisted status as a record state, not current execution eligibility', () => {
    expect(metaCollectionRecordState({ status: 'active', expires_at: '2099-01-01' })).toBe('unexpired')
    expect(metaCollectionRecordState({ status: 'active', expires_at: '2000-01-01' })).toBe('expired')
    expect(metaCollectionRecordState({ status: 'revoked', expires_at: '2099-01-01' })).toBe('revoked')
    expect(metaCollectionRecordState({ status: 'active', expires_at: 'invalid' })).toBe('unknown')
  })
  it('preserves the exact decimal version and reason for an explicit revocation', () => {
    const row = { status: 'active', expires_at: '2099-01-01', version: '9007199254740993' }
    expect(captureMetaCollectionRevocation(row, ' Stop collection ')).toEqual({ version: row.version, reason: 'Stop collection' })
    for (const change of [{ status: 'revoked' }, { version: 1 }, { version: '0' }, { expires_at: '2000-01-01' }]) {
      expect(() => captureMetaCollectionRevocation({ ...row, ...change }, 'Stop')).toThrow()
    }
  })
})
