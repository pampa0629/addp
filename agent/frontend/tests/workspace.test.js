import { describe, expect, it } from 'vitest'
import { groupSessions } from '../src/utils/workspace'

describe('session grouping within a server page', () => {
  it('keeps pins and page order and uses local calendar boundaries', () => {
    const groups = groupSessions([
      { id: 1, pinned: true, updated_at: '2025-01-01' },
      { id: 2, updated_at: new Date(2026, 9, 30, 3).toISOString() },
      { id: 3, updated_at: new Date(2026, 9, 25, 12).toISOString() },
      { id: 4, updated_at: '2025-01-01' }
    ], new Date(2026, 9, 30, 12))
    expect(groups.map(group => [group.key, group.items.map(item => item.id)])).toEqual([
      ['pinned', [1]], ['today', [2]], ['week', [3]], ['older', [4]]
    ])
  })
})

import { sessionResults, stepDuration } from '../src/utils/workspace'
describe('conversation result index', () => {
  it('reuses registered result identities and excludes interactions and plans', () => {
    const presentation = (name, interaction_id) => ({ type: 'presentation_ref', protocol: 'a2ui', surface_id: name, interaction_id,
      content: { operations: [{ updateComponents: { components: [{ component: name }] } }] } })
    expect(sessionResults([{ id: 1, parts: [presentation('WorkflowDag'), presentation('ClarificationChoice', 'i'),
      presentation('TransferDraftSnapshot'), { type: 'presentation_ref', activity_type: 'addp-plan' }] },
      { id: 2, parts: [{ type: 'result_ref', owner_module: 'develop', kind: 'execution', ref: 'execution:1' }] }
    ]).map(item => [item.kind, item.source])).toEqual([['WorkflowDag', '1'], ['TransferDraftSnapshot', '1'], ['execution', '2']])
  })
  it('does not invent elapsed time when timestamps are unavailable', () => {
    expect(stepDuration({ started_at: 'invalid' })).toBeNull()
    expect(stepDuration({ started_at: '2026-10-30T00:00:00Z', completed_at: '2026-10-30T00:00:02Z' })).toBe('2.0')
  })
})
