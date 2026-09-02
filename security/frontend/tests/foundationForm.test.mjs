import { describe, expect, it } from 'vitest'

import { buildFoundationPayload, initialFoundationFieldValue } from '../src/utils/foundationForm.mjs'

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
})
