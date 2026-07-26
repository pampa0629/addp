import { describe, expect, it } from 'vitest'
import { optionalCount, pickNestedCount } from '../../src/utils/metadataRowCount'

describe('metadata row counts', () => {
  it('keeps an exact zero while treating absent values as unknown', () => {
    expect(optionalCount(0)).toBe(0)
    expect(optionalCount('0')).toBe(0)
    expect(optionalCount(null)).toBeNull()
    expect(optionalCount(undefined)).toBeNull()
    expect(optionalCount('')).toBeNull()
  })

  it('reads exact and estimated values only from their requested paths', () => {
    const attributes = {
      type_info: {
        table: {
          row_count: 0,
          estimated_row_count: 12
        }
      }
    }

    expect(pickNestedCount(attributes, [['type_info', 'table', 'row_count']])).toBe(0)
    expect(pickNestedCount(attributes, [['type_info', 'table', 'estimated_row_count']])).toBe(12)
  })
})
