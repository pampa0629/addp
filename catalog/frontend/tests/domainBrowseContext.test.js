import { describe, expect, it } from 'vitest'
import { domainBrowseOptions } from '../src/utils/domainBrowseContext'

describe('Catalog business-domain browsing context', () => {
  const tree = [{ id: '1', name: '户外域', code: 'outdoor', children: [
    { id: '2', name: '活动', code: 'outdoor_activity', description: '户外活动' }
  ] }, { id: '3', name: '空域', code: 'empty' }]

  it('uses every Standard domain while counts come only from visible Catalog facets', () => {
    const options = domainBrowseOptions(tree, {
      status: 'current', options: [{ id: '1', name: '旧名称', count: 12 }]
    }, '3', '引用不可用')
    expect(options.map(({ id, name, count }) => [id, name, count])).toEqual([
      ['1', '户外域', 12], ['2', '活动', 0], ['3', '空域', 0]
    ])
    expect(options[1].path).toEqual(['户外域', '活动'])
    expect(options[1].depth).toBe(1)
  })

  it('keeps already-used domain navigation when Standard is unavailable', () => {
    const options = domainBrowseOptions(null, {
      status: 'current', options: [{ id: '1', name: '户外域', domain_path: ['户外域'], count: 12 }]
    }, '3', '引用不可用')
    expect(options.map(({ id, name }) => [id, name])).toEqual([['3', '引用不可用'], ['1', '户外域']])
    expect(options[1].path).toEqual(['户外域'])
  })

  it('does not display unknown facet counts as zero', () => {
    const options = domainBrowseOptions(tree, { status: 'unavailable', options: [] }, '1', '引用不可用')
    expect(options[0].count).toBeUndefined()
  })
})
