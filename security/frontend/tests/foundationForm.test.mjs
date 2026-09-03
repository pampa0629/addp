import { describe, expect, it } from 'vitest'

import { buildFoundationPayload, initialFoundationFieldValue, protectionEffectI18nKey, sortFoundationRows } from '../src/utils/foundationForm.mjs'

describe('Security foundation form payload', () => {
  it('keeps an optional parent relation absent instead of serializing ID zero', () => {
    const fields = [
      { key: 'code', type: 'text' },
      { key: 'parent_id', type: 'number', nullable: true },
      { key: 'sort_order', type: 'number' }
    ]

    expect(initialFoundationFieldValue(fields[1])).toBeNull()
    expect(buildFoundationPayload(fields, {
      code: 'personal_information',
      parent_id: null,
      sort_order: 10
    })).toEqual({ code: 'personal_information', sort_order: 10 })
  })

  it('preserves a real parent relation and optimistic version', () => {
    const fields = [{ key: 'parent_id', type: 'number', nullable: true }]

    expect(buildFoundationPayload(fields, { parent_id: 7, version: '3' }))
      .toEqual({ parent_id: 7, version: 3 })
  })

  it('orders classifications and grades by their semantic order instead of creation ID', () => {
    expect(sortFoundationRows('classification', [
      { id: '1', code: 'later', sort_order: 20 },
      { id: '2', code: 'first', sort_order: 10 }
    ]).map(row => row.code)).toEqual(['first', 'later'])

    expect(sortFoundationRows('grade', [
      { id: '1', code: 'l3', risk_order: 3 },
      { id: '2', code: 'l1', risk_order: 1 },
      { id: '3', code: 'l2', risk_order: 2 }
    ]).map(row => row.code)).toEqual(['l1', 'l2', 'l3'])
  })

  it('does not construct an invalid translation key for an incomplete table row', () => {
    expect(protectionEffectI18nKey(undefined)).toBeNull()
    expect(protectionEffectI18nKey('unknown')).toBeNull()
    expect(protectionEffectI18nKey('mask')).toBe('security.options.effects.mask')
  })
})
