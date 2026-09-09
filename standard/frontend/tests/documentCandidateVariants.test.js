import { describe, expect, it } from 'vitest'

import { buildCandidateVariantDifferences } from '../src/utils/documentCandidateVariants'

const variant = (semanticFingerprint, candidate) => ({ semantic_fingerprint: semanticFingerprint, candidate })

describe('document candidate variant differences', () => {
  it('returns only fields that differ between semantic variants', () => {
    const differences = buildCandidateVariantDifferences({
      candidate_type: 'element',
      variants: [
        variant('a', { name: '成员状态', definition: '成员参与状态', payload: { data_type: 'string', value_domain_kind: 'enumeration', code_set_code: 'outdoor_member_status' } }),
        variant('b', { name: '成员关系状态', definition: '成员参与状态', payload: { data_type: 'string', value_domain_kind: 'enumeration', code_set_code: 'outdoor_member_status' } })
      ]
    })

    expect(differences.map(item => item.field)).toEqual(['name'])
    expect(differences[0].values.map(item => item.value)).toEqual(['成员状态', '成员关系状态'])
  })

  it('uses semantic normalization for whitespace, dimensions, and code items', () => {
    const metricDifferences = buildCandidateVariantDifferences({
      candidate_type: 'metric',
      variants: [
        variant('a', { name: '参与人数', definition: '活动 参与人数', payload: { dimensions: ['person', ' activity ', 'person'] } }),
        variant('b', { name: '参与人数', definition: '活动\n参与人数', payload: { dimensions: ['activity', 'person'] } })
      ]
    })
    const codeSetDifferences = buildCandidateVariantDifferences({
      candidate_type: 'code_set',
      variants: [
        variant('a', { name: '状态', definition: '状态', payload: { data_type: 'string', items: [{ code: 'closed', name: '关闭' }, { code: 'open', name: '开放' }] } }),
        variant('b', { name: '状态', definition: '状态', payload: { data_type: 'string', items: [{ code: 'open', name: '开放' }, { code: 'closed', name: '关闭' }] } })
      ]
    })

    expect(metricDifferences).toEqual([])
    expect(codeSetDifferences).toEqual([])
  })

  it('does not create a summary for a single variant', () => {
    expect(buildCandidateVariantDifferences({ candidate_type: 'glossary', variants: [variant('a', { name: '领队', definition: '组织活动的人', payload: {} })] })).toEqual([])
  })
})
