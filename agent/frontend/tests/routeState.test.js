import { describe, expect, it } from 'vitest'
import {
  routeAfterSessionDeletion,
  resolveAgentSessionRouteState
} from '../src/utils/routeState'

describe('Agent recoverable route state', () => {
  it('redirects the session-less homepage to the first available session', () => {
    expect(resolveAgentSessionRouteState([{ id: 72 }, { id: 70 }], undefined)).toEqual({
      kind: 'redirect',
      location: { name: 'ChatSession', params: { session_id: '72' } }
    })
  })

  it('keeps an empty homepage when no sessions exist', () => {
    expect(resolveAgentSessionRouteState([], undefined)).toEqual({ kind: 'empty' })
  })

  it('loads a valid session and preserves an unavailable session identity', () => {
    const sessions = [{ id: 72 }, { id: 70 }]
    expect(resolveAgentSessionRouteState(sessions, '70')).toEqual({
      kind: 'ready',
      sessionId: '70'
    })
    expect(resolveAgentSessionRouteState(sessions, '999')).toEqual({
      kind: 'unavailable',
      sessionId: '999'
    })
  })

  it('uses one canonical destination after deleting the current session', () => {
    expect(routeAfterSessionDeletion([{ id: 70 }])).toEqual({
      name: 'ChatSession',
      params: { session_id: '70' }
    })
    expect(routeAfterSessionDeletion([])).toEqual({ name: 'Chat' })
  })
})
