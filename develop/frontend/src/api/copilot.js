import client from './client'

export function generateQueryFromNL(data) {
  return client.post('/copilot/query/generate', data)
}

export function generateWorkflowFromNL(data) {
  return client.post('/copilot/workflow/generate', data)
}

export function getConversationHistory(conversationId, type = 'workflow') {
  return client.get(`/copilot/${type}/conversations/${conversationId}`)
}
