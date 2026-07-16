import { HttpAgent } from '@ag-ui/client'
import { createAuthenticatedFetch } from '@common-ui'

export function createAgentClient({ sessionId, endpoint = '/api/v1/agent/chat', getAuthStore, fetch: fetchImpl }) {
  const authenticatedFetch = createAuthenticatedFetch(getAuthStore, {
    moduleName: 'Agent',
    systemBaseURL: '',
    ...(fetchImpl ? { fetch: fetchImpl } : {})
  })

  return new HttpAgent({
    url: endpoint,
    threadId: String(sessionId),
    fetch: authenticatedFetch
  })
}

export async function replayAgentRunEvents({ agentRunId, after = 0, getAuthStore, fetch: fetchImpl, onEvent }) {
  const authenticatedFetch = createAuthenticatedFetch(getAuthStore, {
    moduleName: 'Agent',
    systemBaseURL: '',
    ...(fetchImpl ? { fetch: fetchImpl } : {})
  })
  const response = await authenticatedFetch(
    `/api/v1/agent/runs/${encodeURIComponent(agentRunId)}/events?after=${after}`,
    { headers: { Accept: 'text/event-stream' } }
  )
  if (!response.ok || !response.body) {
    throw new Error(`事件回放失败: ${response.status}`)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const emitBlock = (block) => {
    const data = block
      .split('\n')
      .filter(line => line.startsWith('data:'))
      .map(line => line.slice(5).trimStart())
      .join('\n')
    if (!data) return
    const sequence = Number(block.split('\n').find(line => line.startsWith('id:'))?.slice(3).trim())
    onEvent?.({ sequence: Number.isInteger(sequence) ? sequence : null, event: JSON.parse(data) })
  }
  while (true) {
    const { done, value } = await reader.read()
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done })
    let boundary
    while ((boundary = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, boundary)
      buffer = buffer.slice(boundary + 2)
      emitBlock(block)
    }
    if (done) {
      if (buffer.trim()) emitBlock(buffer)
      break
    }
  }
}
