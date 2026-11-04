import { describe, expect, it } from 'vitest'
import { supportsEngineTableStructure, isStructureTableEntry, tableStructureFields } from '../src/utils/engineTableStructure'

const capabilities = { engine_family: 'tabular', storage: {
  catalog: { supported: true, real_time: true }, facts: { supported: true, field_info: true },
  catalog_model: { path_version: 'catalog.path/v1', root_term: 'server', levels: [
    { term: 'schema', role: 'branch', kinds: ['namespace'] }, { term: 'table', role: 'leaf', kinds: ['table', 'view'] }
  ] }
} }
const engine = { id: '2', capabilities }
const entry = { role: 'leaf', term: 'table', kind: 'table', path: { engine_id: 2, version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' }, { term: 'table', kind: 'table', name: 'persons' }
] } }
const facts = { path: entry.path, kind: 'table', table: { fields: [
  { name: 'id', type: 'int', native_type: 'bigint', nullable: false, primary_key: true },
  { name: 'profile', type: 'json', native_type: 'jsonb', nullable: true, comment: 'PRIVATE', default_expression: 'PRIVATE' }
], row_count: 8 }, samples: ['PRIVATE'], statistics: { secret: 'PRIVATE' } }

describe('System live table structure', () => {
  it('admits declared tabular facts, independent of an engine type name', () => {
    expect(supportsEngineTableStructure({ ...engine, engine_type: 'another-tabular-engine' })).toBe(true)
    expect(supportsEngineTableStructure({ ...engine, capabilities: JSON.stringify(capabilities) })).toBe(true)
    for (const changed of [null, {}, { ...capabilities, engine_family: 'dynamic_schema' },
      { ...capabilities, storage: { ...capabilities.storage, facts: { supported: true } } },
      { ...capabilities, storage: { ...capabilities.storage, catalog: { supported: true, real_time: false } } },
      { ...capabilities, storage: { ...capabilities.storage, catalog_model: { ...capabilities.storage.catalog_model, levels: [null] } } }]) {
      expect(supportsEngineTableStructure({ ...engine, capabilities: changed })).toBe(false)
    }
  })
  it('validates the typed table path and rejects a branch, sampled collection or another engine', () => {
    expect(isStructureTableEntry(engine, entry)).toBe(true)
    for (const change of [{ role: 'branch' }, { term: 'collection', kind: 'collection' },
      { path: { ...entry.path, engine_id: 3 } }, { path: { ...entry.path, segments: entry.path.segments.slice(0, -1) } }]) {
      expect(isStructureTableEntry(engine, { ...entry, ...change })).toBe(false)
    }
  })
  it('preserves every column and explicit true/false nullable while dropping nonstructural fields', () => {
    expect(tableStructureFields(engine, entry, facts)).toEqual([
      { name: 'id', type: 'int', native_type: 'bigint', nullable: false, primary_key: true },
      { name: 'profile', type: 'json', native_type: 'jsonb', nullable: true, primary_key: false }
    ])
    expect(JSON.stringify(tableStructureFields(engine, entry, facts))).not.toContain('PRIVATE')
    expect(tableStructureFields(engine, entry, { ...facts, path: { ...entry.path, segments: entry.path.segments.map(s => ({ name: s.name, kind: s.kind, term: s.term })) } })).toHaveLength(2)
  })
  it('refuses wrong target, missing nullable, duplicate columns and incomplete facts', () => {
    for (const change of [{ path: { ...entry.path, engine_id: 3 } }, { kind: 'collection' },
      { path: { ...entry.path, segments: [...entry.path.segments.slice(0, -1), { term: 'table', kind: 'table', name: 'other' }] } },
      { table: {} }, { table: { fields: [{ name: 'id' }] } },
      { table: { fields: [facts.table.fields[0], facts.table.fields[0]] } }]) {
      expect(() => tableStructureFields(engine, entry, { ...facts, ...change })).toThrow()
    }
  })
})
