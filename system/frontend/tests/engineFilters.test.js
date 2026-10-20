import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { createEngineFilters, filterEngineList, paginateEngines } from '../src/utils/engineList'

const engine = (id, capabilities, engine_origin = 'general', is_builtin = false, lifecycle_state = 'active') => ({
  id, capabilities: { schema_version: 'engine.capabilities/v1', ...capabilities }, engine_origin, is_builtin, lifecycle_state
})
const snapshot = [
  engine(1, { storage: {} }),
  engine(2, { compute: {} }),
  engine(3, { storage: {}, compute: {} }),
  engine(4, { storage: {} }, 'extension'),
  engine(5, { compute: {} }, 'extension', true),
  engine(6, { storage: {} }, 'general', true),
  engine(7, { storage: {} }, 'general', false, 'disabled'),
  engine(8, { storage: {} }, 'general', false, 'deleting'),
  engine(9, { storage: {} }, 'general', false, 'deleted')
]
const ids = filters => filterEngineList(snapshot, { ...createEngineFilters(), ...filters }).map(row => row.id)

describe('engine management filters', () => {
  it('owns filter controls in one component without retaining the mixed inline filter path', () => {
    const view = readFileSync(new URL('../src/views/Engines.vue', import.meta.url), 'utf8')
    expect(view.match(/<EngineFilterBar\b/g)).toHaveLength(1)
    expect(view).not.toContain('selectedCategories')
    expect(view).not.toContain('<el-checkbox-group')
  })

  it('defaults to both capabilities, both origins and both registration modes, excluding only deleted engines', () => {
    expect(ids({})).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
    const first = createEngineFilters()
    first.capabilityGroups.pop()
    expect(createEngineFilters().capabilityGroups).toEqual(['storage', 'compute'])
  })

  it('unions capabilities and matches a dual-capability engine once, independently of its type name', () => {
    expect(ids({ capabilityGroups: ['storage'] })).toEqual([1, 3, 4, 6, 7, 8])
    expect(ids({ capabilityGroups: ['compute'] })).toEqual([2, 3, 5])
    expect(ids({ capabilityGroups: ['storage', 'compute'] })).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
    expect(filterEngineList([engine(10, { compute: {} })], { ...createEngineFilters(), capabilityGroups: ['storage'] })).toEqual([])
    expect(filterEngineList([{ ...snapshot[0], capabilities: null }], createEngineFilters())).toEqual([])
  })

  it.each(['capabilityGroups', 'engineOrigins', 'lifecycleStates'])('returns no engines when %s is empty', key => {
    expect(ids({ [key]: [] })).toEqual([])
  })

  it('intersects origin, registration and capability dimensions', () => {
    expect(ids({ engineOrigins: ['extension'], registration: 'registered' })).toEqual([4])
    expect(ids({ capabilityGroups: ['storage'], registration: 'builtin' })).toEqual([6])
    expect(ids({ capabilityGroups: ['storage'], registration: 'builtin', engineOrigins: ['extension'] })).toEqual([])
    expect(ids({ registration: 'builtin' })).toEqual([5, 6])
    expect(ids({ registration: 'registered' })).toEqual([1, 2, 3, 4, 7, 8])
  })

  it('unions only the selected lifecycle states, without changing the original snapshot', () => {
    expect(ids({ lifecycleStates: ['deleted'] })).toEqual([9])
    expect(ids({ lifecycleStates: ['disabled', 'deleted'] })).toEqual([7, 9])
    expect(snapshot).toHaveLength(9)
    expect(ids({})).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
  })

  it('filters the complete snapshot before pagination', () => {
    const filtered = filterEngineList(snapshot, { ...createEngineFilters(), capabilityGroups: ['storage'] })
    expect(filtered).toHaveLength(6)
    expect(paginateEngines(filtered, 2, 2).map(row => row.id)).toEqual([4, 6])
  })
})
