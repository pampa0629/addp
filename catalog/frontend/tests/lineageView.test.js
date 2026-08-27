import { describe, expect, it } from 'vitest'
import { lineageFailureState, resolveLineageSubject } from '../src/utils/lineageView'

describe('catalog lineage federated view', () => {
  it('uses only an active Meta DataItem structured item identity', () => {
    expect(resolveLineageSubject({
      entry_status: 'active',
      source: {
        source_module: 'meta',
        source_type: 'data_item',
        source_status: 'active',
        observed_snapshot: { item_id: 42, full_name: 'public.orders' }
      }
    })).toEqual({ subject_kind: 'data_item', item_id: '42', direction: 'both', depth: 3, limit: 100 })
  })

  it('does not infer an item identity from paths or professional sources', () => {
    expect(resolveLineageSubject({
      entry_status: 'active',
      source: {
        source_module: 'meta',
        source_type: 'data_item',
        source_status: 'active',
        observed_snapshot: { full_name: 'public.orders' }
      }
    })).toBeNull()
    expect(resolveLineageSubject({
      entry_status: 'active',
      source: {
        source_module: 'model',
        source_type: 'logical_table',
        source_status: 'active',
        observed_snapshot: { item_id: 42 }
      }
    })).toBeNull()
  })

  it('keeps permission, missing subject, and owner availability failures distinct', () => {
    expect(lineageFailureState({ response: { status: 403 } })).toBe('forbidden')
    expect(lineageFailureState({ response: { status: 404 } })).toBe('subject_missing')
    expect(lineageFailureState({ response: { status: 503 } })).toBe('unavailable')
  })
})
