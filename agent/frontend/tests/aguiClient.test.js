import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createAgentClient, replayAgentRunEvents } from '../src/agent/createAgentClient'
import { clearRuntimeAccessToken, setRuntimeAccessToken } from '@common-ui'

function sse(event) {
  return `data: ${JSON.stringify(event)}\n\n`
}

describe('AG-UI client', () => {
  beforeEach(() => clearRuntimeAccessToken())

  it('replays sequenced AgentRun events with ADDP authentication', async () => {
    setRuntimeAccessToken('test-token')
    const events = []
    const fetchMock = vi.fn(async (_url, init) => {
      expect(init.headers.get('Authorization')).toBe('Bearer test-token')
      return new Response([
        'id: 7',
        'data: {"type":"STATE_SNAPSHOT","snapshot":{"status":"waiting"}}',
        '',
        'id: 8',
        'data: {"type":"TOOL_CALL_RESULT","toolCallId":"call-1","content":"restricted"}',
        '',
      ].join('\n'), { headers: { 'Content-Type': 'text/event-stream' } })
    })

    await replayAgentRunEvents({
      agentRunId: 'run-1',
      after: 6,
      getAuthStore: () => ({ token: 'test-token', clearLocalSession: vi.fn(), refreshAccessToken: vi.fn() }),
      fetch: fetchMock,
      onEvent: event => events.push(event)
    })

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/agent/runs/run-1/events?after=6')
    expect(events).toEqual([
      { sequence: 7, event: { type: 'STATE_SNAPSHOT', snapshot: { status: 'waiting' } } },
      { sequence: 8, event: { type: 'TOOL_CALL_RESULT', toolCallId: 'call-1', content: 'restricted' } }
    ])
  })

  it('sends the standard RunAgentInput contract with ADDP authentication', async () => {
    setRuntimeAccessToken('test-token')
    let requestBody
    const fetchMock = vi.fn(async (_url, init) => {
      requestBody = JSON.parse(init.body)
      const stream = [
        { type: 'RUN_STARTED', threadId: requestBody.threadId, runId: requestBody.runId },
        { type: 'TEXT_MESSAGE_START', messageId: 'assistant-1', role: 'assistant' },
        { type: 'TEXT_MESSAGE_CONTENT', messageId: 'assistant-1', delta: 'ok' },
        { type: 'TEXT_MESSAGE_END', messageId: 'assistant-1' },
        {
          type: 'RUN_FINISHED',
          threadId: requestBody.threadId,
          runId: requestBody.runId,
          outcome: { type: 'success' }
        }
      ].map(sse).join('')
      return new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } })
    })

    const authStore = {
      token: 'test-token',
      clearLocalSession: vi.fn(),
      refreshAccessToken: vi.fn()
    }
    const agent = createAgentClient({
      sessionId: 42,
      getAuthStore: () => authStore,
      fetch: fetchMock
    })
    agent.addMessage({ id: 'user-1', role: 'user', content: 'hello' })
    const result = await agent.runAgent()

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(requestBody.threadId).toBe('42')
    expect(requestBody.messages.at(-1)).toMatchObject({ id: 'user-1', role: 'user', content: 'hello' })
    expect(fetchMock.mock.calls[0][1].headers.get('Authorization')).toBe('Bearer test-token')
    expect(result.newMessages.at(-1)).toMatchObject({ role: 'assistant', content: 'ok' })
  })

  it('refreshes an expired token once and retries the AG-UI request', async () => {
    setRuntimeAccessToken('expired-token')
    const authStore = {
      token: 'expired-token',
      refreshAccessToken: vi.fn(async () => {
        authStore.token = 'fresh-token'
        setRuntimeAccessToken('fresh-token')
        return 'fresh-token'
      }),
      clearLocalSession: vi.fn()
    }
    let chatAttempts = 0
    const fetchMock = vi.fn(async (url, init) => {
      chatAttempts += 1
      if (chatAttempts === 1) {
        expect(init.headers.get('Authorization')).toBe('Bearer expired-token')
        return new Response('', { status: 401 })
      }

      expect(init.headers.get('Authorization')).toBe('Bearer fresh-token')
      const requestBody = JSON.parse(init.body)
      return new Response([
        { type: 'RUN_STARTED', threadId: requestBody.threadId, runId: requestBody.runId },
        {
          type: 'RUN_FINISHED',
          threadId: requestBody.threadId,
          runId: requestBody.runId,
          outcome: { type: 'success' }
        }
      ].map(sse).join(''), { headers: { 'Content-Type': 'text/event-stream' } })
    })

    const agent = createAgentClient({
      sessionId: 42,
      getAuthStore: () => authStore,
      fetch: fetchMock
    })
    agent.addMessage({ id: 'user-1', role: 'user', content: 'hello' })
    await agent.runAgent()

    expect(chatAttempts).toBe(2)
    expect(authStore.refreshAccessToken).toHaveBeenCalledWith({ force: true })
    expect(authStore.clearLocalSession).not.toHaveBeenCalled()
  })
})
