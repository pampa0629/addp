import { describe, expect, it } from 'vitest'
import {
  buildGovernanceEntryCandidateQuery,
  buildGovernanceTaskQuery,
  isCanonicalGovernanceTaskQuery,
  parseGovernanceTaskRoute
} from '../src/utils/governanceTaskRouteState.js'

describe('Catalog governance task route state', () => {
  it('normalizes invalid values to the open queue defaults', () => {
    expect(parseGovernanceTaskRoute({ status: 'invalid', entry_id: '01', page: '-1', page_size: '500' })).toEqual({
      status: 'open',
      entry_id: '',
      page: 1,
      page_size: 200
    })
  })

  it('keeps a canonical entry UUID and omits default query values', () => {
    const entryID = '5b753fb8-f8d3-4e9b-bd18-fd2f54c662f8'
    expect(buildGovernanceTaskQuery({ entry_id: entryID, status: 'open', page: 1, page_size: 20 })).toEqual({ entry_id: entryID })
    expect(buildGovernanceTaskQuery({ status: 'resolved', page: 2, page_size: 50 })).toEqual({
      status: 'resolved',
      page: '2',
      page_size: '50'
    })
  })

  it('searches governance task entry candidates across the inventory view', () => {
    expect(buildGovernanceEntryCandidateQuery('  Outdoor  ')).toEqual({
      view: 'inventory',
      search: 'Outdoor',
      page: 1,
      page_size: 20
    })
    expect(buildGovernanceEntryCandidateQuery()).toEqual({
      view: 'inventory',
      page: 1,
      page_size: 20
    })
  })

  it('compares canonical queries independently of key order', () => {
    expect(isCanonicalGovernanceTaskQuery({ page: '2', status: 'resolved' }, { status: 'resolved', page: '2' })).toBe(true)
    expect(isCanonicalGovernanceTaskQuery({ status: 'open' }, {})).toBe(false)
  })
})
