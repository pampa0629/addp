import { describe, expect, it } from 'vitest'
import { supportsEngineInstanceOverview, instanceOverviewSnapshot } from '../src/utils/engineInstanceOverview'

const engine = { id: '71', version: 2, engine_type: 'future_native', capabilities: { instance_overview: true } }
const metric = { key: 'database_size_bytes', scope: 'database', scope_name: 'example', unit: 'bytes', semantics: 'gauge', status: 'available', value: '9007199254740993' }
const snapshot = { engine_id: '71', version: 2, queried_at: '2026-11-08T12:00:00Z', metrics: [metric] }
describe('native instance overview', () => {
  it('uses the declared capability independently of engine names or storage classification', () => {
    expect(supportsEngineInstanceOverview(engine)).toBe(true)
    expect(supportsEngineInstanceOverview({ ...engine, engine_type: 'redis', capabilities: {} })).toBe(false)
    expect(supportsEngineInstanceOverview({ ...engine, capabilities: { instance_overview: 'true' } })).toBe(false)
  })
  it('retains exact integers and true zero while permitting unavailable independent metrics', () => {
    expect(instanceOverviewSnapshot(snapshot, engine).metrics[0].value).toBe('9007199254740993')
    const values = [{ ...metric, value: '0' }, { ...metric, key: 'server_connections', value: undefined, scope: 'server', status: 'permission_denied' }]
    expect(instanceOverviewSnapshot({ ...snapshot, metrics: values }, engine).metrics[0].value).toBe('0')
  })
  it('rejects another engine/version, unsafe JSON numbers and missing values disguised as zero', () => {
    for (const changed of [{ engine_id: '72' }, { version: 3 }, { queried_at: 'bad' },
      { metrics: [{ ...metric, value: 9007199254740992 }] }, { metrics: [{ ...metric, value: null }] },
      { metrics: [{ ...metric, value: '0', status: 'permission_denied' }] },
      { metrics: [{ ...metric, value: '-1' }] }, { metrics: [metric, metric] }]) {
      expect(() => instanceOverviewSnapshot({ ...snapshot, ...changed }, engine)).toThrow('invalid_instance_overview')
    }
  })
})
