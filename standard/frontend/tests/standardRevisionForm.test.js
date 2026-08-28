import { describe, expect, it } from 'vitest'

import {
  buildCodeSetRevisionPayload,
  buildElementRevisionPayload,
  isCodeSetCompatible,
  listReplacementItems,
  resetIncompatibleElementConstraints
} from '../src/utils/standardRevisionForm'

describe('Standard revision form mapping', () => {
  it('matches backend code set value-type compatibility', () => {
    expect(isCodeSetCompatible('text', 'string')).toBe(true)
    expect(isCodeSetCompatible('int', 'bigint')).toBe(true)
    expect(isCodeSetCompatible('bigint', 'int')).toBe(false)
    expect(isCodeSetCompatible('decimal', 'string')).toBe(false)
  })

  it('clears constraints that become invalid after changing data type', () => {
    const revision = {
      length: 32,
      precision_num: 10,
      scale: 2,
      format: '^x$',
      value_domain_kind: 'range',
      range_constraint: { min: 0, max: 10 },
      code_set_revision_id: 7
    }

    resetIncompatibleElementConstraints(revision, 'bool')

    expect(revision).toMatchObject({
      length: null,
      precision_num: null,
      scale: null,
      format: '',
      value_domain_kind: 'unrestricted',
      range_constraint: null,
      code_set_revision_id: null
    })
  })

  it('builds one canonical element revision payload and preserves rule identity', () => {
    const payload = buildElementRevisionPayload({
      name: 'Amount',
      definition: 'Transaction amount',
      data_type: 'decimal',
      precision_num: 12,
      scale: 2,
      nullable: false,
      value_domain_kind: 'range',
      range_constraint: { min: 0, min_inclusive: true },
      example_values: ['12.50'],
      change_summary: 'Define amount',
      effective_from: '2026-09-01T00:00:00+08:00'
    }, 4, '00000000-0000-4000-8000-000000000001', true)

    expect(payload.version).toBe(4)
    expect(payload.code_set_revision_id).toBeNull()
    expect(payload.extra_quality_rules.rules[0].rule_key).toBe('00000000-0000-4000-8000-000000000001')
    expect(payload.effective_to).toBeNull()
  })

  it('maps code set revision effective dates without identity fields', () => {
    expect(buildCodeSetRevisionPayload({
      name: 'Gender',
      description: 'Gender code list',
      value_type: 'string',
      change_summary: 'Initial definition',
      effective_from: null,
      effective_to: '2027-01-01T00:00:00+08:00'
    }, 2)).toEqual({
      version: 2,
      name: 'Gender',
      description: 'Gender code list',
      value_type: 'string',
      change_summary: 'Initial definition',
      effective_from: null,
      effective_to: '2027-01-01T00:00:00+08:00'
    })
  })

  it('only offers other active items as replacements', () => {
    expect(listReplacementItems([
      { id: 1, status: 'deprecated' },
      { id: 2, status: 'active' },
      { id: 3, status: 'active' }
    ], 2)).toEqual([{ id: 3, status: 'active' }])
  })
})
