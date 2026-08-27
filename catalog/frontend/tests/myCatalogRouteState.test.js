import { describe, expect, it } from 'vitest'
import { buildMyCatalogQuery, isCanonicalMyCatalogQuery, parseMyCatalogRoute } from '../src/utils/myCatalogRouteState.js'

describe('Catalog personal route state', () => {
  it('normalizes unknown relations and paging', () => {
    expect(parseMyCatalogRoute({ relation: 'recent', page: 0, page_size: 500 })).toEqual({
      relation: 'responsible', page: 1, page_size: 200
    })
  })

  it('omits the default responsible relation', () => {
    expect(buildMyCatalogQuery({ relation: 'responsible', page: 1, page_size: 20 })).toEqual({})
    expect(buildMyCatalogQuery({ relation: 'favorite', page: 2 })).toEqual({ relation: 'favorite', page: '2' })
  })

  it('compares canonical query values', () => {
    expect(isCanonicalMyCatalogQuery({ page: '2', relation: 'following' }, { relation: 'following', page: '2' })).toBe(true)
    expect(isCanonicalMyCatalogQuery({ relation: 'responsible' }, {})).toBe(false)
  })
})
