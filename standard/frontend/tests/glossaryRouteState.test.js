import { describe, expect, it } from 'vitest'
import {
  buildGlossaryFilterQuery,
  createGlossaryForm,
  isGlossaryDeletable,
  parsePositiveInteger,
  resolveGlossaryFilters
} from '../src/utils/glossaryRouteState'

describe('glossary route state', () => {
  it('从 URL 恢复有效筛选条件', () => {
    expect(resolveGlossaryFilters({
      keyword: '客户',
      owner_domain_id: '12',
      status: 'published',
      page: '3',
      page_size: '50'
    })).toEqual({
      keyword: '客户',
      owner_domain_id: 12,
      status: 'published',
      page: 3,
      page_size: 50
    })
  })

  it('清理无效 URL 参数并恢复默认分页', () => {
    expect(resolveGlossaryFilters({
      keyword: ['客户'],
      owner_domain_id: '0',
      status: 'unknown',
      page: '-1',
      page_size: 'invalid'
    })).toEqual({
      keyword: '',
      owner_domain_id: null,
      status: '',
      page: 1,
      page_size: 20
    })
  })

  it('只把非默认筛选条件写回 URL', () => {
    expect(buildGlossaryFilterQuery({
      keyword: '客户',
      owner_domain_id: 12,
      status: 'draft',
      page: 2,
      page_size: 20
    })).toEqual({ keyword: '客户', owner_domain_id: '12', status: 'draft', page: '2' })
  })

  it('新增业务术语继承当前业务域且每次生成独立数组', () => {
    const first = createGlossaryForm(12)
    const second = createGlossaryForm(12)

    expect(first.owner_domain_id).toBe(12)
    expect(first).toEqual({
      scope_type: 'domain',
      owner_domain_id: 12,
      code: '',
      name: '',
      alias: [],
      definition: '',
      example: '',
      note: '',
      related_ids: [],
      tags: [],
      change_summary: '',
      effective_from: null,
      effective_to: null
    })
    expect(first.alias).not.toBe(second.alias)
    expect(first.tags).not.toBe(second.tags)
  })

  it.each([
    ['8', null, 8],
    ['1.5', 20, 20],
    [null, 20, 20]
  ])('正整数解析 %s 使用兜底值 %s 时得到 %s', (value, fallback, expected) => {
    expect(parsePositiveInteger(value, fallback)).toBe(expected)
  })
})

describe('isGlossaryDeletable', () => {
  it('只允许从未发布的术语展示删除动作', () => {
    expect(isGlossaryDeletable({ has_publication_history: false })).toBe(true)
    expect(isGlossaryDeletable({ has_publication_history: true })).toBe(false)
  })
})
