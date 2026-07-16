import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

export { authAPI } from './auth'

const agentClient = createAPIClient(() => useAuthStore(), {
  moduleName: 'Agent',
  baseURL: '/api/v1',
})

export const sessionAPI = {
  list() {
    return agentClient.get('/agent/sessions')
  },
  create(data) {
    return agentClient.post('/agent/sessions', data)
  },
  delete(id) {
    return agentClient.delete(`/agent/sessions/${id}`)
  },
  getMessages(sessionId) {
    return agentClient.get(`/agent/sessions/${sessionId}/messages`)
  },
}

export const runAPI = {
  cancel(id) {
    return agentClient.post(`/agent/runs/${id}/cancel`)
  },
}

export const resultRefAPI = {
  getDevelopExecution(executionId) {
    return agentClient.get(`/develop/executions/${encodeURIComponent(executionId)}`)
  }
}

export default agentClient
