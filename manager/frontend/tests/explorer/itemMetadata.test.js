import { describe, expect, it } from 'vitest'

import { normalizeMetaItemMetadata } from '../../src/utils/itemMetadata.js'

describe('Meta item metadata normalization', () => {
  it('adapts Meta item facts for the attributes view', () => {
    const result = normalizeMetaItemMetadata({
      fingerprint: 'item-fingerprint',
      item_type: 'table',
      full_name: 'outdoor.ods_outdoor_persons',
      row_count: 2188,
      scanned_at: '2026-09-01T09:00:18Z',
      scanned_depth: 'deep',
      attributes: {
        type_info: { table: { fields: [{ name: 'person_id', type: 'string' }] } },
        item: { data_type: 'table', layout: 'single' }
      }
    })

    expect(result).toEqual({
      fingerprint: 'item-fingerprint',
      item_type: 'table',
      item_type_i18n_key: 'engine.term.table',
      full_name: 'outdoor.ods_outdoor_persons',
      row_count: 2188,
      attributes: [
        { key: 'item', value: { data_type: 'table', layout: 'single' } },
        { key: 'type_info', value: { table: { fields: [{ name: 'person_id', type: 'string' }] } } }
      ],
      scanned_at: '2026-09-01T09:00:18Z',
      scanned_depth: 'deep'
    })
  })

  it('rejects missing facts without inventing metadata', () => {
    expect(normalizeMetaItemMetadata(null)).toBeNull()
  })
})
