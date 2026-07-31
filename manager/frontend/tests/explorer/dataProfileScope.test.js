import { describe, expect, it } from 'vitest'
import { buildProfileDataScope, operatorsForProfileField } from '../../src/utils/dataProfileScope.js'

describe('data profile scope', () => {
  it('limits operators by canonical field type', () => {
    expect(operatorsForProfileField({ type: 'string' })).toContain('contains')
    expect(operatorsForProfileField({ type: 'timestamp' })).toContain('between')
    expect(operatorsForProfileField({ type: 'geometry' })).toEqual(['is_null', 'is_not_null'])
  })

  it('builds typed single-level conditions', () => {
    expect(buildProfileDataScope(
      [{ name: 'amount', type: 'double' }, { name: 'status', type: 'string' }],
      'and',
      [
        { field: 'amount', operator: 'between', values: ['10', '20'] },
        { field: 'status', operator: 'in', values: ['active', 'pending'] }
      ]
    )).toEqual({
      kind: 'condition',
      logic: 'and',
      conditions: [
        { field: 'amount', operator: 'between', values: [10, 20] },
        { field: 'status', operator: 'in', values: ['active', 'pending'] }
      ]
    })
  })
})
