import { describe, expect, it } from 'vitest'
import { conversationReferences, mergeMessageWindow, referenceEntries } from '../src/utils/history'

describe('conversation history windows', () => {
  it('keeps authoritative persisted content and merges repeated protocol identities once', () => {
    const local = { id: 'local:2', protocol_message_id: 'p2', content: 'partial' }
    const committed = { id: 2, protocol_message_id: 'p2', content: 'complete' }
    const old = { id: 1, content: 'older' }
    const current = mergeMessageWindow([local], [old, committed], true)
    expect(current).toEqual([old, committed])
    expect(mergeMessageWindow(current, [committed])).toEqual(current)
  })

  it('deduplicates the index across loaded pages using the newest source', () => {
    const entry = { key: 'surface:1', kind: 'TransferDraftSnapshot', label: 'new draft' }
    const rows = [
      { message_id: 30, created_at: '2026-11-07T10:00:00Z', entries: [entry] },
      { message_id: 10, created_at: '2026-11-07T09:00:00Z', entries: [{ ...entry, label: 'older draft' }] }
    ]
    expect(referenceEntries(rows)).toEqual([{ ...entry, source: '30' }])
  })

  it('keeps newer loaded resource references when older index pages arrive', () => {
    const locator = 'addp://engine/11/path/source?type=collection'
    const message = { id: 40, created_at: '2026-11-07T11:00:00Z', parts: [{ type: 'presentation_ref', protocol: 'a2ui', content: {
      operations: [{ updateComponents: { components: [{ component: 'TransferDraftSnapshot', parameters: JSON.stringify({ config: { source: { locator } } }) }] } }]
    } }] }
    const old = { message_id: 10, created_at: '2026-11-07T09:00:00Z', entries: [{ key: locator, locator, kind: 'resource' }] }
    expect(conversationReferences([old], [message], 'resources')[0].source).toBe('40')
    const newer = { ...old, message_id: 50, created_at: '2026-11-07T12:00:00Z' }
    expect(conversationReferences([old, newer], [message], 'resources')[0].source).toBe('50')
  })

  it('preserves PostgreSQL microsecond ordering before using the id tie breaker', () => {
    const entry = { key: 'same-resource', kind: 'resource' }
    expect(referenceEntries([
      { message_id: 20, created_at: '2026-11-07T11:00:00.000001+00:00', entries: [entry] },
      { message_id: 10, created_at: '2026-11-07T11:00:00.000002+00:00', entries: [entry] }
    ])[0].source).toBe('10')
  })
})
