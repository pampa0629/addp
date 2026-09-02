import { describe, expect, it } from 'vitest'
import { activeCodeItemLabels, formatPhysicalType, formatRangeConstraint } from '../src/utils/dataDictionaryView'

describe('dataDictionaryView', () => {
  it('formats current physical types without replacing native details', () => {
    expect(formatPhysicalType({ native_type: 'numeric', precision: 18, scale: 2 })).toBe('numeric(18,2)')
    expect(formatPhysicalType({ type: 'string', size: 64 })).toBe('string(64)')
  })

  it('formats range boundaries and only active code items', () => {
    expect(formatRangeConstraint({ min: '0', max: '100', min_inclusive: true, max_inclusive: false })).toBe('[0, 100)')
    expect(activeCodeItemLabels({ code_set_revision: { items: [
      { code: 'A', label: 'Active', status: 'active' },
      { code: 'D', label: 'Deprecated', status: 'deprecated' }
    ] } })).toEqual(['A · Active'])
  })
})
