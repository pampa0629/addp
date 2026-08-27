import { describe, expect, it } from 'vitest'
import { buildCollectionQuery, isCanonicalCollectionQuery, parseCollectionRoute } from '../src/utils/collectionRouteState.js'

describe('Catalog collection route state', () => {
  it('normalizes project group and paging values', () => {
    expect(parseCollectionRoute({ project_group_id: '01', page: '-1', page_size: 500 })).toEqual({
      project_group_id: '', page: 1, page_size: 200
    })
  })

  it('builds a compact canonical query', () => {
    expect(buildCollectionQuery({ project_group_id: '9007199254740993', page: 2, page_size: 20 })).toEqual({
      project_group_id: '9007199254740993', page: '2'
    })
  })

  it('compares canonical query values', () => {
    expect(isCanonicalCollectionQuery({ page: '2', project_group_id: '9' }, { project_group_id: '9', page: '2' })).toBe(true)
    expect(isCanonicalCollectionQuery({ page: '1' }, {})).toBe(false)
  })
})
