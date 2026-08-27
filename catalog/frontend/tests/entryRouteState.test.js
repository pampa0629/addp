import { describe, expect, it } from 'vitest'
import { buildEntryListQuery, isCanonicalEntryListQuery, parseEntryListRoute } from '../src/utils/entryRouteState.js'

describe('Catalog entry list route state', () => {
  it('normalizes invalid values to the canonical defaults', () => {
    expect(parseEntryListRoute({
      search: ' orders ',
      entry_type: 'unknown',
      source_status: 'unknown',
      governance_status: 'curated',
      visibility: 'tenant',
      primary_domain_id: '01',
      accountable_department_id: '9007199254740993',
      source_engine_id: '9223372036854775808',
      page: '-2',
      page_size: '500'
    })).toEqual({
      view: 'governance',
      search: 'orders',
      entry_type: '',
      source_status: '',
      governance_status: 'curated',
      visibility: 'tenant',
      primary_domain_id: '',
      accountable_department_id: '9007199254740993',
      source_engine_id: '',
      page: 1,
      page_size: 200
    })
  })

  it('omits default values from the canonical query', () => {
    expect(buildEntryListQuery({ page: 1, page_size: 20 })).toEqual({})
    expect(buildEntryListQuery({ search: 'orders', entry_type: 'logical_model', primary_domain_id: '9007199254740993', page: 2, page_size: 50 })).toEqual({
      search: 'orders',
      entry_type: 'logical_model',
      primary_domain_id: '9007199254740993',
      page: '2',
      page_size: '50'
    })
  })

  it('uses governance as the omitted default and preserves explicit inventory view', () => {
    expect(parseEntryListRoute({}).view).toBe('governance')
    expect(buildEntryListQuery({ view: 'inventory', page: 1, page_size: 20 })).toEqual({ view: 'inventory' })
    expect(parseEntryListRoute({ view: 'unknown' }).view).toBe('governance')
  })

  it('accepts Metric as a first-class entry type', () => {
    expect(parseEntryListRoute({ entry_type: 'metric' }).entry_type).toBe('metric')
  })

  it('accepts service and development artifact entry types', () => {
    expect(parseEntryListRoute({ entry_type: 'data_service' }).entry_type).toBe('data_service')
    expect(parseEntryListRoute({ entry_type: 'development_artifact' }).entry_type).toBe('development_artifact')
  })

  it('compares canonical queries independently of key order', () => {
    expect(isCanonicalEntryListQuery(
      { page: '2', search: 'orders' },
      { search: 'orders', page: '2' }
    )).toBe(true)
    expect(isCanonicalEntryListQuery({ page: '1' }, {})).toBe(false)
  })
})
