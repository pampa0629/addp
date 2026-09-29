import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { lineageFailureState, lineageNodesToSourceReferences, resolveLineageSubject } from '../src/utils/lineageView'

describe('catalog lineage federated view', () => {
  it('gives the shared viewer a bounded, responsive host instead of an ignored height prop', () => {
    const source = readFileSync(new URL('../src/views/EntryDetail.vue', import.meta.url), 'utf8')
    expect(source).toContain('<div class="lineage-viewer-frame">')
    expect(source).toContain('.lineage-viewer-frame { height: clamp(420px, 70vh, 720px); }')
    expect(source).not.toContain('<LineageViewer :graph="lineageGraph" :height=')
  })

  it('binds the selected depth and reloads the Meta graph when it changes', () => {
    const source = readFileSync(new URL('../src/views/EntryDetail.vue', import.meta.url), 'utf8')
    expect(source).toContain('v-model:depth="lineageDepth"')
    expect(source).toContain('const lineageDepth = ref(2)')
    expect(source).toContain('resolveLineageSubject(entry.value, lineageDepth.value)')
    expect(source).toContain('canReadLineage.value, lineageDepth.value], loadLineage')
  })

  it('uses only an active Meta DataItem structured item identity', () => {
    const entry = {
      entry_status: 'active',
      source: {
        source_module: 'meta',
        source_type: 'data_item',
        source_status: 'active',
        observed_snapshot: { item_id: 42, full_name: 'public.orders' }
      }
    }
    expect(resolveLineageSubject(entry)).toEqual({ subject_kind: 'data_item', item_id: '42', direction: 'both', depth: 2, limit: 100 })
    expect(resolveLineageSubject(entry, 1)).toEqual({ subject_kind: 'data_item', item_id: '42', direction: 'both', depth: 1, limit: 100 })
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

  it('maps only DataItem fingerprints to exact Catalog source references', () => {
    expect(lineageNodesToSourceReferences([
      { kind: 'data_item', item_fingerprint: 'fp-1' },
      { kind: 'data_item', item_fingerprint: 'fp-1' },
      { kind: 'published_service', item_fingerprint: 'fp-2' },
      { kind: 'data_item', item_fingerprint: ' fp-3 ' }
    ])).toEqual([
      { source_module: 'meta', source_type: 'data_item', source_identity: 'fp-1' },
      { source_module: 'meta', source_type: 'data_item', source_identity: 'fp-3' }
    ])
  })
})
