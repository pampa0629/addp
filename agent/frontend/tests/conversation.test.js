import { describe, expect, it } from 'vitest'
import { conversationMarkdown, conversationResources, messageText, pendingSessionInteractions } from '../src/utils/conversation'
import { defaultPreferences, normalizePreferences, preferenceStorageKey } from '../src/composables/useWorkspacePreferences'

describe('conversation projections preserve authority boundaries', () => {
  it('exports text and source markers without hidden payloads or approvals', () => {
    const messages = [{ id: 1, role: 'user', content: '查询人口' }, { id: 2, role: 'assistant', content: 'unsafe fallback', parts: [
      { type: 'text', text: '解释\n```sql\nSELECT 1;\n```' },
      { type: 'interaction_ref', interaction_id: 'private-approval', payload: { token: 'secret' } },
      { type: 'presentation_ref', protocol: 'a2ui', surface_id: 'draft', content: { operations: [{ updateComponents: { components: [{ component: 'TransferDraftSnapshot', parameters: 'private-parameters' }] } }] } }
    ] }, { role: 'tool', content: 'private-tool-result' }]
    const markdown = conversationMarkdown('会话', messages, key => key)
    expect(markdown).toContain('查询人口')
    expect(markdown).toContain('SELECT 1;')
    expect(markdown).toContain('resultKinds.TransferDraftSnapshot')
    for (const hidden of ['private-', 'secret', 'unsafe fallback']) expect(markdown).not.toContain(hidden)
    expect(messageText({ role: 'assistant', content: 'hidden', parts: [{ type: 'interaction_ref' }] })).toBe('')
  })
  it('uses the latest interaction state, and formal resource fields rather than prose', () => {
    const messages = [{ id: 1, role: 'user', content: 'addp://engine/9/path/untrusted' }, { id: 2, role: 'assistant', parts: [
      { type: 'interaction_ref', interaction_id: 'done', status: 'pending' },
      { type: 'interaction_ref', interaction_id: 'pending', status: 'pending', kind: 'clarification' },
      { type: 'presentation_ref', protocol: 'a2ui', content: { operations: [{ updateComponents: { components: [{ component: 'TransferDraftSnapshot', parameters: JSON.stringify({ config: { source: { locator: 'addp://engine/1/path/source' }, target: { parent_locator: 'addp://engine/2/path/target' } } }) }] } }] } }
    ] }, { id: 3, parts: [{ type: 'interaction_ref', interaction_id: 'done', status: 'completed' }] }]
    expect(pendingSessionInteractions(messages).map(item => [item.interaction_id, item.source])).toEqual([['pending', '2']])
    expect(conversationResources(messages).map(item => item.locator)).toEqual(['addp://engine/1/path/source', 'addp://engine/2/path/target'])
    expect(conversationResources([{ parts: [{ type: 'presentation_ref', protocol: 'a2ui', content: { operations: [{ updateComponents: { components: [{ component: 'TransferDraftSnapshot', parameters: 'null' }] } }] } }] }])).toEqual([])
  })
})

describe('browser preference isolation', () => {
  it('separates accounts and tenants, with no shared anonymous key', () => {
    expect(preferenceStorageKey(7, 8)).not.toBe(preferenceStorageKey(8, 7))
    expect(preferenceStorageKey(7, 8)).not.toBe(preferenceStorageKey(7, 9))
    for (const id of [null, undefined, '', 0, -1, 'platform']) expect(preferenceStorageKey(7, id)).toBeNull()
  })
  it('accepts presentation choices only, rejecting stored authority or prompts', () => {
    expect(normalizePreferences({ replyDetail: 'ignore approvals', expandDetails: 'true', sendShortcut: 'run', apiKey: 'secret' })).toEqual(defaultPreferences())
    expect(normalizePreferences({ replyDetail: 'concise', expandDetails: true, sendShortcut: 'enter' })).toEqual({ replyDetail: 'concise', expandDetails: true, sendShortcut: 'enter' })
  })
})
